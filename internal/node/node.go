// Package node ties together discovery, file watching, indexing, and transfer
// into a single running peer node.
package node

import (
	"crypto/rand"
	"encoding/hex"
	"fmt"
	"log"
	"net"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/fsnotify/fsnotify"

	"retrosync/internal/config"
	"retrosync/internal/discovery"
	"retrosync/internal/index"
	"retrosync/internal/transfer"
)

// syncThrottle fires a sync immediately on the first trigger, then suppresses
// further triggers until the cooldown period has elapsed since the last sync
// completed.
type syncThrottle struct {
	mu       sync.Mutex
	lastSync time.Time
	cooldown time.Duration
	runSync  func()
}

func newSyncThrottle(cooldown time.Duration, runSync func()) *syncThrottle {
	return &syncThrottle{
		cooldown: cooldown,
		runSync:  runSync,
	}
}

// trigger fires a sync immediately if the cooldown has elapsed since the last
// sync completed. Otherwise the call is a no-op.
func (t *syncThrottle) trigger() {
	t.mu.Lock()
	if time.Since(t.lastSync) < t.cooldown {
		t.mu.Unlock()
		log.Printf("sync: trigger suppressed (cooldown active)")
		return
	}
	// Mark as running now to block concurrent triggers during the sync.
	t.lastSync = time.Now()
	t.mu.Unlock()

	go func() {
		t.runSync()
		t.mu.Lock()
		t.lastSync = time.Now()
		t.mu.Unlock()
	}()
}

// Node is a single participant in the sync network.
type Node struct {
	id            string
	name          string // human-readable name (hostname or config override)
	version       string
	httpPort      int
	discoveryPort int

	isServer   bool
	serverAddr string // host only (client mode)
	serverPort int    // port only (client mode)
	serverName string // display name of the discovered/configured server (client mode)

	cfgPath string         // empty when launched with -dir (legacy)
	cfg     *config.Config // live copy for mutation

	groups  map[string][]config.PathSpec // group name → specs
	entries []index.SyncEntry            // flattened, for watcher + Build

	mu     sync.RWMutex
	syncMu sync.Mutex // serializes periodic sync and ForceSync
	peers  map[string]discovery.Peer
	fileIdx index.Index

	events  *transfer.EventBuffer

	disc     *discovery.Discovery
	server   *transfer.Server
	client   *transfer.Client
	watcher  *fsnotify.Watcher
	throttle *syncThrottle // nil on server/P2P nodes

	startTime   time.Time
	totalSynced atomic.Int64

	done chan struct{}
}

// New creates a Node from a Config. cfgPath is the path to the TOML file on
// disk; pass "" when launched with the legacy -dir flag (disables config
// mutation).
func New(cfg *config.Config, cfgPath string, version string) (*Node, error) {
	groups, err := config.ParseAllSpecs(cfg.Syncs)
	if err != nil {
		return nil, err
	}

	// Flatten groups into SyncEntries and ensure each dir exists.
	var entries []index.SyncEntry
	for _, sg := range cfg.Syncs {
		specs := groups[sg.Name]
		for _, ps := range specs {
			if err := os.MkdirAll(ps.Dir, 0755); err != nil {
				return nil, fmt.Errorf("mkdir %s: %w", ps.Dir, err)
			}
			entries = append(entries, index.SyncEntry{
				GroupName: sg.Name,
				Dir:       ps.Dir,
				Patterns:  ps.Patterns,
			})
		}
	}

	id := mustGenerateID()
	log.Printf("node ID: %s", id)

	name := cfg.Node.Name
	if name == "" {
		if h, err := os.Hostname(); err == nil {
			name = h
		} else {
			name = id
		}
	}

	var svrAddr string
	var svrPort int
	if cfg.Node.ServerAddr != "" {
		host, portStr, err := net.SplitHostPort(cfg.Node.ServerAddr)
		if err != nil {
			return nil, fmt.Errorf("invalid server_addr %q: %w", cfg.Node.ServerAddr, err)
		}
		p, err := strconv.Atoi(portStr)
		if err != nil {
			return nil, fmt.Errorf("invalid port in server_addr %q: %w", cfg.Node.ServerAddr, err)
		}
		svrAddr = host
		svrPort = p
	}

	n := &Node{
		id:            id,
		name:          name,
		version:       version,
		httpPort:      cfg.Node.Port,
		discoveryPort: cfg.Node.DiscoveryPort,
		isServer:      cfg.Node.Role == "server",
		serverAddr:    svrAddr,
		serverPort:    svrPort,
		cfgPath:       cfgPath,
		cfg:           cfg,
		groups:        groups,
		entries:       entries,
		peers:         make(map[string]discovery.Peer),
		fileIdx:       make(index.Index),
		client:        transfer.NewClient(id, name, cfg.Node.Port),
		done:          make(chan struct{}),
	}
	return n, nil
}

// Start indexes existing files, launches the HTTP server, and starts the
// appropriate sync mode (server, client, or legacy P2P).
func (n *Node) Start() error {
	n.startTime = time.Now()
	var err error
	n.fileIdx, err = index.BuildFromGroups(n.entries)
	if err != nil {
		return err
	}
	log.Printf("indexed %d existing file(s)", len(n.fileIdx))

	var getServerSyncs func() ([]config.SyncGroup, error)
	var forceSync func(string) error
	var triggerSync func() error
	if n.cfg.Node.Role == "client" {
		getServerSyncs = func() ([]config.SyncGroup, error) {
			n.mu.RLock()
			addr, port := n.serverAddr, n.serverPort
			n.mu.RUnlock()
			if addr == "" {
				return nil, fmt.Errorf("server not yet discovered")
			}
			return n.client.FetchSyncs(addr, port)
		}
		forceSync = func(group string) error {
			if group == "" {
				return n.ForceSyncAll()
			}
			return n.ForceSyncGroup(group)
		}
		n.throttle = newSyncThrottle(
			n.cooldownDuration(),
			func() {
				n.syncMu.Lock()
				n.syncWithServer()
				n.syncMu.Unlock()
			},
		)
		triggerSync = n.TriggerSync
	}
	var registerPeer func(id, name string, port int, addr string)
	if n.cfg.Node.Role != "client" {
		registerPeer = n.registerPeer
	}
	evtBuf := transfer.NewEventBuffer()
	n.events = evtBuf
	n.server = transfer.NewServer(transfer.ServerOpts{
		Port:           n.httpPort,
		GetIndex:       n.snapshot,
		ResolveLocal:   n.resolveLocal,
		RouteIncoming:  n.routeIncoming,
		GetStatus:      n.Status,
		GetSyncs:       n.SyncGroupsWithCounts,
		AddGroup:       n.AddGroup,
		RemoveGroup:    n.RemoveGroup,
		RegisterPeer:   registerPeer,
		PauseGroup:     n.PauseGroup,
		PauseAll:       n.PauseAllGroups,
		ForceSync:      forceSync,
		TriggerSync:    triggerSync,
		GetServerSyncs: getServerSyncs,
		OnSyncEvent:    func() { n.totalSynced.Add(1) },
		Events:         evtBuf,
	})
	if err := n.server.Start(); err != nil {
		return err
	}
	log.Printf("web UI: http://localhost:%d/ui", n.httpPort)

	if n.isServer {
		// Broadcast with IsServer=true so clients can discover us automatically.
		n.disc = discovery.New(n.id, n.httpPort, n.discoveryPort, true, n.name, n.onClientDiscovered)
		if err := n.disc.Start(); err != nil {
			return err
		}
		if err := n.startWatcher(); err != nil {
			return err
		}
		go n.pruneInactivePeers()
		log.Printf("running as authoritative server")
		return nil
	}

	if n.cfg.Node.Role == "client" {
		if n.serverAddr == "" {
			// Server address not known — start discovery to find one via UDP broadcast.
			n.disc = discovery.New(n.id, n.httpPort, n.discoveryPort, false, n.name, n.onServerDiscovered)
			if err := n.disc.Start(); err != nil {
				return err
			}
			log.Printf("running as client, discovering server via UDP broadcast on port %d", n.discoveryPort)
		} else {
			log.Printf("running as client, server %s:%d (discovery skipped)", n.serverAddr, n.serverPort)
		}
		if err := n.startWatcher(); err != nil {
			return err
		}
		go n.periodicSyncWithServer()
		return nil
	}

	// Legacy P2P mode
	n.disc = discovery.New(n.id, n.httpPort, n.discoveryPort, false, n.name, n.onPeerDiscovered)
	if err := n.disc.Start(); err != nil {
		return err
	}

	if err := n.startWatcher(); err != nil {
		return err
	}

	go n.periodicSync()
	go n.pruneInactivePeers()
	return nil
}

// Stop shuts all subsystems down cleanly.
func (n *Node) Stop() {
	close(n.done)
	if n.disc != nil {
		n.disc.Stop()
	}
	if n.server != nil {
		n.server.Stop()
	}
	if n.watcher != nil {
		n.watcher.Close()
	}
}

// snapshot returns a copy of the current file index (safe for concurrent use).
func (n *Node) snapshot() index.Index {
	n.mu.RLock()
	defer n.mu.RUnlock()
	out := make(index.Index, len(n.fileIdx))
	for k, v := range n.fileIdx {
		out[k] = v
	}
	return out
}

// resolveLocal maps a virtual path to the absolute OS path via the index.
func (n *Node) resolveLocal(virtualPath string) (string, bool) {
	n.mu.RLock()
	defer n.mu.RUnlock()
	fi, ok := n.fileIdx[virtualPath]
	if !ok {
		return "", false
	}
	return fi.LocalPath, true
}

// routeIncoming maps a virtual path ("group/filename") to the absolute local
// destination path using this node's own config.
func (n *Node) routeIncoming(virtualPath string) (string, error) {
	slash := strings.Index(virtualPath, "/")
	if slash < 0 {
		return "", fmt.Errorf("virtual path has no group prefix: %q", virtualPath)
	}
	group := virtualPath[:slash]
	filename := virtualPath[slash+1:]

	specs, ok := n.groups[group]
	if !ok {
		return "", fmt.Errorf("unknown group %q", group)
	}

	for _, ps := range specs {
		for _, pat := range ps.Patterns {
			if pat == "*" {
				return filepath.Join(ps.Dir, filename), nil
			}
			matched, err := filepath.Match(pat, filename)
			if err == nil && matched {
				return filepath.Join(ps.Dir, filename), nil
			}
		}
	}
	return "", fmt.Errorf("no pattern in group %q matches %q", group, filename)
}

// registerPeer records a peer identified via HTTP request headers. This is called
// by the transfer server when it receives a request carrying RetroSync identity
// headers, allowing the server to track clients that connect directly via
// server_addr without relying on UDP discovery.
func (n *Node) registerPeer(id, name string, port int, addr string) {
	n.mu.Lock()
	defer n.mu.Unlock()
	n.peers[id] = discovery.Peer{ID: id, Name: name, Addr: addr, Port: port, LastSeen: time.Now()}
}

// pruneInactivePeers removes peers that have not sent a beacon within 15 seconds
// (3× the 5s beacon interval). Runs as a background goroutine on server and
// legacy P2P nodes.
func (n *Node) pruneInactivePeers() {
	ticker := time.NewTicker(15 * time.Second)
	defer ticker.Stop()
	for {
		select {
		case <-ticker.C:
			threshold := time.Now().Add(-15 * time.Second)
			n.mu.Lock()
			for id, p := range n.peers {
				if p.LastSeen.Before(threshold) {
					log.Printf("discovery: client %s timed out", p.Name)
					delete(n.peers, id)
				}
			}
			n.mu.Unlock()
		case <-n.done:
			return
		}
	}
}

// onPeerDiscovered is called by discovery for every beacon in legacy P2P mode.
// It refreshes LastSeen on known peers and triggers a sync only for new ones.
func (n *Node) onPeerDiscovered(peer discovery.Peer) {
	n.mu.Lock()
	_, exists := n.peers[peer.ID]
	n.peers[peer.ID] = peer
	n.mu.Unlock()
	if !exists {
		log.Printf("discovery: found peer %s at %s:%d", peer.Name, peer.Addr, peer.Port)
		n.syncWithPeer(peer)
	}
}

// onClientDiscovered is called by discovery for every beacon in server mode.
// It refreshes LastSeen on known clients and logs newly seen ones.
func (n *Node) onClientDiscovered(peer discovery.Peer) {
	if peer.IsServer {
		return
	}
	n.mu.Lock()
	_, exists := n.peers[peer.ID]
	n.peers[peer.ID] = peer
	n.mu.Unlock()
	if !exists {
		log.Printf("discovery: client connected %s at %s:%d", peer.Name, peer.Addr, peer.Port)
	}
}

// onServerDiscovered is called by discovery in client mode. It captures the
// server's address and name the first time a server beacon is heard.
func (n *Node) onServerDiscovered(peer discovery.Peer) {
	if !peer.IsServer {
		return
	}
	n.mu.Lock()
	defer n.mu.Unlock()
	if n.serverAddr == "" {
		n.serverAddr = peer.Addr
		n.serverPort = peer.Port
		n.serverName = peer.Name
		log.Printf("discovered authoritative server at %s:%d", peer.Addr, peer.Port)
	}
}

// startWatcher sets up fsnotify on every entry dir (deduplicated).
func (n *Node) startWatcher() error {
	watcher, err := fsnotify.NewWatcher()
	if err != nil {
		return err
	}
	n.watcher = watcher

	seen := make(map[string]bool)
	for _, entry := range n.entries {
		err = filepath.Walk(entry.Dir, func(path string, info os.FileInfo, err error) error {
			if err != nil || !info.IsDir() {
				return nil
			}
			if !seen[path] {
				seen[path] = true
				return watcher.Add(path)
			}
			return nil
		})
		if err != nil {
			return err
		}
	}

	go n.watchLoop()
	return nil
}

// watchLoop processes fsnotify events with 500 ms debouncing per file.
func (n *Node) watchLoop() {
	debounce := make(map[string]*time.Timer)
	var mu sync.Mutex

	schedule := func(name string) {
		mu.Lock()
		defer mu.Unlock()
		if t, ok := debounce[name]; ok {
			t.Stop()
		}
		debounce[name] = time.AfterFunc(500*time.Millisecond, func() {
			n.onFileChanged(name)
			mu.Lock()
			delete(debounce, name)
			mu.Unlock()
		})
	}

	for {
		select {
		case event, ok := <-n.watcher.Events:
			if !ok {
				return
			}
			// Skip temp files written during our own atomic downloads.
			if isTemp(event.Name) {
				continue
			}
			if event.Has(fsnotify.Create) {
				// Watch newly created subdirectories recursively.
				if info, err := os.Stat(event.Name); err == nil && info.IsDir() {
					n.watcher.Add(event.Name)
					continue
				}
			}
			if event.Has(fsnotify.Write) || event.Has(fsnotify.Create) {
				schedule(event.Name)
			}
			if event.Has(fsnotify.Remove) || event.Has(fsnotify.Rename) {
				n.onFileRemoved(event.Name)
			}

		case err, ok := <-n.watcher.Errors:
			if !ok {
				return
			}
			log.Printf("watcher: %v", err)

		case <-n.done:
			return
		}
	}
}

func isTemp(name string) bool {
	base := filepath.Base(name)
	return len(base) > 0 && base[0] == '.' && filepath.Ext(base) == ".tmp"
}

// onFileChanged re-indexes a single file after it has been written.
func (n *Node) onFileChanged(absPath string) {
	info, err := os.Stat(absPath)
	if err != nil || info.IsDir() {
		return
	}

	virtualPath, ok := n.findVirtualPath(absPath)
	if !ok {
		return
	}

	fi, err := index.BuildFileInfo(absPath, virtualPath, info)
	if err != nil {
		log.Printf("index error %s: %v", virtualPath, err)
		return
	}

	n.mu.Lock()
	n.fileIdx[virtualPath] = fi
	n.mu.Unlock()

	log.Printf("indexed: %s (md5:%s…)", virtualPath, fi.Hash[:8])
}

// onFileRemoved drops a deleted file from the local index.
func (n *Node) onFileRemoved(absPath string) {
	virtualPath, ok := n.findVirtualPath(absPath)
	if !ok {
		return
	}

	n.mu.Lock()
	existing := n.fileIdx[virtualPath]
	delete(n.fileIdx, virtualPath)
	n.mu.Unlock()

	if n.events != nil {
		grp, fname := splitVirtualPath(virtualPath)
		n.events.Append("del", grp, fname, "", existing.Size)
		n.totalSynced.Add(1)
	}
}

// findVirtualPath finds the virtual path for an absolute file path by matching
// it against all entries.
func (n *Node) findVirtualPath(absPath string) (string, bool) {
	name := filepath.Base(absPath)
	for _, entry := range n.entries {
		// Check if the file is directly inside entry.Dir (not subdirs).
		dir := filepath.Clean(entry.Dir)
		fileDir := filepath.Dir(filepath.Clean(absPath))
		if fileDir != dir {
			continue
		}
		for _, pat := range entry.Patterns {
			if pat == "*" {
				return entry.GroupName + "/" + name, true
			}
			matched, err := filepath.Match(pat, name)
			if err == nil && matched {
				return entry.GroupName + "/" + name, true
			}
		}
	}
	return "", false
}

// periodicSync syncs with all known peers every 30 seconds.
func (n *Node) periodicSync() {
	ticker := time.NewTicker(30 * time.Second)
	defer ticker.Stop()
	for {
		select {
		case <-ticker.C:
			n.mu.RLock()
			peers := make([]discovery.Peer, 0, len(n.peers))
			for _, p := range n.peers {
				peers = append(peers, p)
			}
			n.mu.RUnlock()
			for _, p := range peers {
				n.syncWithPeer(p)
			}
		case <-n.done:
			return
		}
	}
}

// syncWithPeer fetches the remote index and pulls any files that are missing
// locally or where the remote version is newer (by ModTime).
func (n *Node) syncWithPeer(peer discovery.Peer) {
	remote, err := n.client.FetchIndex(peer.Addr, peer.Port)
	if err != nil {
		log.Printf("sync: fetch index from %s failed: %v", peer.ID, err)
		return
	}

	n.mu.RLock()
	local := n.fileIdx
	n.mu.RUnlock()

	for path, remoteFile := range remote {
		group := path[:strings.Index(path, "/")]
		if n.isPaused(group) {
			continue
		}
		localFile, exists := local[path]
		if exists && localFile.Hash == remoteFile.Hash {
			continue // already up to date
		}
		if exists && !remoteFile.ModTime.After(localFile.ModTime) {
			continue // local copy is the same age or newer
		}

		log.Printf("sync: pulling %s from %s", path, peer.ID)
		if err := n.client.FetchFile(peer.Addr, peer.Port, path, n.routeIncoming); err != nil {
			log.Printf("sync: pull %s failed: %v", path, err)
			continue
		}
		if n.events != nil {
			grp, fname := splitVirtualPath(path)
			n.events.Append("in", grp, fname, peer.Name, remoteFile.Size)
			n.totalSynced.Add(1)
		}

		// Re-index the freshly downloaded file.
		destPath, err := n.routeIncoming(path)
		if err != nil {
			continue
		}
		info, err := os.Stat(destPath)
		if err != nil {
			continue
		}
		fi, err := index.BuildFileInfo(destPath, path, info)
		if err != nil {
			continue
		}
		n.mu.Lock()
		n.fileIdx[path] = fi
		n.mu.Unlock()
	}
}

// syncWithServer performs a bidirectional sync with the authoritative server:
// pull server-newer files down, push client-newer files up.
func (n *Node) syncWithServer() {
	n.mu.RLock()
	addr, port := n.serverAddr, n.serverPort
	n.mu.RUnlock()

	if addr == "" {
		log.Printf("sync: server not yet discovered, waiting for beacon...")
		return
	}

	if n.serverName == "" {
		if name, err := n.client.FetchStatus(addr, port); err == nil && name != "" {
			n.mu.Lock()
			n.serverName = name
			n.mu.Unlock()
		}
	}

	serverPaused := map[string]bool{}
	if serverSyncs, err := n.client.FetchSyncs(addr, port); err == nil {
		for _, g := range serverSyncs {
			serverPaused[g.Name] = g.Paused
		}
	} else {
		log.Printf("sync: could not fetch server syncs: %v", err)
	}

	serverIdx, err := n.client.FetchIndex(addr, port)
	if err != nil {
		log.Printf("sync: fetch index from server failed: %v", err)
		return
	}

	local := n.snapshot()

	// PULL PASS — fetch files that are newer on the server.
	for virtualPath, remoteFile := range serverIdx {
		group := virtualPath[:strings.Index(virtualPath, "/")]
		if serverPaused[group] || n.isPaused(group) {
			continue
		}
		localFile, exists := local[virtualPath]
		if exists && localFile.Hash == remoteFile.Hash {
			continue
		}
		if exists && !remoteFile.ModTime.After(localFile.ModTime) {
			continue // local is same age or newer
		}

		log.Printf("sync: pulling %s from server", virtualPath)
		if err := n.client.FetchFile(addr, port, virtualPath, n.routeIncoming); err != nil {
			log.Printf("sync: pull %s failed: %v", virtualPath, err)
			continue
		}
		if n.events != nil {
			grp, fname := splitVirtualPath(virtualPath)
			n.events.Append("in", grp, fname, n.serverName, remoteFile.Size)
			n.totalSynced.Add(1)
		}

		// Re-index the freshly downloaded file.
		destPath, err := n.routeIncoming(virtualPath)
		if err != nil {
			continue
		}
		info, err := os.Stat(destPath)
		if err != nil {
			continue
		}
		fi, err := index.BuildFileInfo(destPath, virtualPath, info)
		if err != nil {
			continue
		}
		n.mu.Lock()
		n.fileIdx[virtualPath] = fi
		n.mu.Unlock()
	}

	// Refresh local snapshot after pulls.
	local = n.snapshot()

	// PUSH PASS — upload files that are newer locally.
	for virtualPath, localFile := range local {
		group := virtualPath[:strings.Index(virtualPath, "/")]
		if serverPaused[group] || n.isPaused(group) {
			continue
		}
		remoteFile, exists := serverIdx[virtualPath]
		if exists && localFile.Hash == remoteFile.Hash {
			continue
		}
		if exists && !localFile.ModTime.After(remoteFile.ModTime) {
			continue // server is same age or newer
		}

		log.Printf("sync: pushing %s to server", virtualPath)
		if err := n.client.PushFile(addr, port, virtualPath, localFile.LocalPath); err != nil {
			log.Printf("sync: push %s failed: %v", virtualPath, err)
			continue
		}
		if n.events != nil {
			grp, fname := splitVirtualPath(virtualPath)
			n.events.Append("out", grp, fname, n.serverName, localFile.Size)
			n.totalSynced.Add(1)
		}
	}
}

// TriggerSync fires a sync immediately if the cooldown has elapsed since the
// last triggered sync completed, otherwise the call is suppressed. Returns
// immediately; the sync runs in the background. Not available on server/P2P
// nodes.
func (n *Node) TriggerSync() error {
	if n.throttle == nil {
		return fmt.Errorf("not available")
	}
	n.throttle.trigger()
	return nil
}

// syncIntervalDuration returns the configured periodic sync interval, falling
// back to 30 seconds if unset.
func (n *Node) syncIntervalDuration() time.Duration {
	if s := n.cfg.Node.SyncInterval; s > 0 {
		return time.Duration(s) * time.Second
	}
	return 30 * time.Second
}

// cooldownDuration returns the configured cooldown, falling back to 2 minutes.
func (n *Node) cooldownDuration() time.Duration {
	if s := n.cfg.Node.SyncCooldown; s > 0 {
		return time.Duration(s) * time.Second
	}
	return 120 * time.Second
}

// periodicSyncWithServer syncs with the server immediately on startup, then
// on the configured sync_interval (default 30s).
func (n *Node) periodicSyncWithServer() {
	n.syncMu.Lock()
	n.syncWithServer()
	n.syncMu.Unlock()
	ticker := time.NewTicker(n.syncIntervalDuration())
	defer ticker.Stop()
	for {
		select {
		case <-ticker.C:
			n.syncMu.Lock()
			n.syncWithServer()
			n.syncMu.Unlock()
		case <-n.done:
			return
		}
	}
}

func splitVirtualPath(vp string) (group, filename string) {
	if i := strings.Index(vp, "/"); i >= 0 {
		return vp[:i], vp[i+1:]
	}
	return "", vp
}

func mustGenerateID() string {
	b := make([]byte, 8)
	if _, err := rand.Read(b); err != nil {
		panic(err)
	}
	return hex.EncodeToString(b)
}

// Status returns a snapshot of this node's runtime state.
func (n *Node) Status() transfer.StatusInfo {
	n.mu.RLock()
	defer n.mu.RUnlock()
	peers := make([]transfer.PeerInfo, 0, len(n.peers))
	for _, p := range n.peers {
		peers = append(peers, transfer.PeerInfo{ID: p.ID, Name: p.Name, Addr: p.Addr, Port: p.Port})
	}
	return transfer.StatusInfo{
		ID:            n.id,
		Name:          n.name,
		Version:       n.version,
		HTTPPort:      n.httpPort,
		DiscoveryPort: n.discoveryPort,
		FileCount:     len(n.fileIdx),
		Role:          n.cfg.Node.Role,
		ServerAddr:    n.serverAddr,
		ServerPort:    n.serverPort,
		ServerName:    n.serverName,
		Peers:         peers,
		Uptime:        formatUptime(time.Since(n.startTime)),
		TotalSynced:   n.totalSynced.Load(),
	}
}

func formatUptime(d time.Duration) string {
	d = d.Round(time.Second)
	h := int(d.Hours())
	m := int(d.Minutes()) % 60
	s := int(d.Seconds()) % 60
	if h > 0 {
		return fmt.Sprintf("%dh %dm %ds", h, m, s)
	}
	if m > 0 {
		return fmt.Sprintf("%dm %ds", m, s)
	}
	return fmt.Sprintf("%ds", s)
}

// SyncGroups returns a copy of the currently configured sync groups.
func (n *Node) SyncGroups() []config.SyncGroup {
	n.mu.RLock()
	defer n.mu.RUnlock()
	result := make([]config.SyncGroup, len(n.cfg.Syncs))
	copy(result, n.cfg.Syncs)
	return result
}

// SyncGroupsWithCounts returns the current sync groups annotated with
// per-group indexed file counts.
func (n *Node) SyncGroupsWithCounts() []transfer.SyncGroupInfo {
	n.mu.RLock()
	defer n.mu.RUnlock()

	counts := make(map[string]int, len(n.cfg.Syncs))
	for virtualPath := range n.fileIdx {
		if i := strings.Index(virtualPath, "/"); i > 0 {
			counts[virtualPath[:i]]++
		}
	}

	result := make([]transfer.SyncGroupInfo, len(n.cfg.Syncs))
	for i, sg := range n.cfg.Syncs {
		result[i] = transfer.SyncGroupInfo{
			Name:      sg.Name,
			Paths:     sg.Paths,
			Paused:    sg.Paused,
			FileCount: counts[sg.Name],
		}
	}
	return result
}

// AddGroup adds a new sync group at runtime, re-indexes its files, and
// persists the change to disk (when a config file path is set).
func (n *Node) AddGroup(name string, paths []string) error {
	n.mu.Lock()

	for _, g := range n.cfg.Syncs {
		if g.Name == name {
			n.mu.Unlock()
			return fmt.Errorf("group %q already exists", name)
		}
	}

	specs := make([]config.PathSpec, 0, len(paths))
	for _, raw := range paths {
		ps, err := config.ParsePathSpec(raw)
		if err != nil {
			n.mu.Unlock()
			return fmt.Errorf("group %q: %w", name, err)
		}
		specs = append(specs, ps)
	}

	for _, ps := range specs {
		if err := os.MkdirAll(ps.Dir, 0755); err != nil {
			n.mu.Unlock()
			return fmt.Errorf("mkdir %s: %w", ps.Dir, err)
		}
	}

	n.cfg.Syncs = append(n.cfg.Syncs, config.SyncGroup{Name: name, Paths: paths})
	n.groups[name] = specs

	var newEntries []index.SyncEntry
	for _, ps := range specs {
		entry := index.SyncEntry{GroupName: name, Dir: ps.Dir, Patterns: ps.Patterns}
		newEntries = append(newEntries, entry)
		n.entries = append(n.entries, entry)
	}

	newIdx, err := index.BuildFromGroups(newEntries)
	if err != nil {
		n.mu.Unlock()
		return fmt.Errorf("index group %q: %w", name, err)
	}
	for k, v := range newIdx {
		n.fileIdx[k] = v
	}

	if n.watcher != nil {
		for _, ps := range specs {
			_ = filepath.Walk(ps.Dir, func(path string, info os.FileInfo, walkErr error) error {
				if walkErr == nil && info.IsDir() {
					n.watcher.Add(path)
				}
				return nil
			})
		}
	}

	n.mu.Unlock()

	if n.cfgPath != "" {
		return config.Save(n.cfgPath, n.cfg)
	}
	return nil
}

// isPaused reports whether the named group is currently paused.
func (n *Node) isPaused(groupName string) bool {
	n.mu.RLock()
	defer n.mu.RUnlock()
	for _, g := range n.cfg.Syncs {
		if g.Name == groupName {
			return g.Paused
		}
	}
	return false
}

// PauseGroup sets the paused state of a sync group and persists the change.
func (n *Node) PauseGroup(name string, paused bool) error {
	n.mu.Lock()
	found := false
	for i, g := range n.cfg.Syncs {
		if g.Name == name {
			n.cfg.Syncs[i].Paused = paused
			found = true
			break
		}
	}
	n.mu.Unlock()
	if !found {
		return fmt.Errorf("group %q not found", name)
	}
	if n.cfgPath != "" {
		return config.Save(n.cfgPath, n.cfg)
	}
	return nil
}

// PauseAllGroups sets the paused state on every sync group and persists the change.
func (n *Node) PauseAllGroups(paused bool) error {
	n.mu.Lock()
	for i := range n.cfg.Syncs {
		n.cfg.Syncs[i].Paused = paused
	}
	n.mu.Unlock()
	if n.cfgPath != "" {
		return config.Save(n.cfgPath, n.cfg)
	}
	return nil
}

// ForceSyncGroup performs an authoritative pull from the server for a single
// group: downloads every server file unconditionally (skipping only hash-equal
// files) and deletes any local files in the group that are absent on the server.
// It holds syncMu for the duration so it cannot overlap with a periodic sync.
func (n *Node) ForceSyncGroup(name string) error {
	n.mu.RLock()
	addr, port := n.serverAddr, n.serverPort
	n.mu.RUnlock()
	if addr == "" {
		return fmt.Errorf("server not yet discovered")
	}

	// Record pause state, then pause to block the periodic sync from touching
	// this group while we wait for syncMu.
	waspaused := n.isPaused(name)
	if err := n.PauseGroup(name, true); err != nil {
		return err
	}

	// Wait for any in-progress sync to finish, then hold the lock for our duration.
	n.syncMu.Lock()
	defer n.syncMu.Unlock()

	serverIdx, err := n.client.FetchIndex(addr, port)
	if err != nil {
		n.PauseGroup(name, waspaused) //nolint:errcheck
		return fmt.Errorf("force-sync: fetch index: %w", err)
	}

	local := n.snapshot()

	// PULL PASS — download every server file for this group unconditionally.
	prefix := name + "/"
	for virtualPath, remoteFile := range serverIdx {
		if !strings.HasPrefix(virtualPath, prefix) {
			continue
		}
		localFile, exists := local[virtualPath]
		if exists && localFile.Hash == remoteFile.Hash {
			continue // already identical
		}
		log.Printf("force-sync: pulling %s", virtualPath)
		if err := n.client.FetchFile(addr, port, virtualPath, n.routeIncoming); err != nil {
			log.Printf("force-sync: pull %s failed: %v", virtualPath, err)
			continue
		}
		if n.events != nil {
			grp, fname := splitVirtualPath(virtualPath)
			n.events.Append("in", grp, fname, n.serverName, remoteFile.Size)
			n.totalSynced.Add(1)
		}
		destPath, err := n.routeIncoming(virtualPath)
		if err != nil {
			continue
		}
		info, err := os.Stat(destPath)
		if err != nil {
			continue
		}
		fi, err := index.BuildFileInfo(destPath, virtualPath, info)
		if err != nil {
			continue
		}
		n.mu.Lock()
		n.fileIdx[virtualPath] = fi
		n.mu.Unlock()
	}

	// DELETE PASS — remove local files in this group not present on server.
	local = n.snapshot()
	for virtualPath, localFile := range local {
		if !strings.HasPrefix(virtualPath, prefix) {
			continue
		}
		if _, onServer := serverIdx[virtualPath]; onServer {
			continue
		}
		log.Printf("force-sync: deleting local-only %s", virtualPath)
		os.Remove(localFile.LocalPath)
		n.mu.Lock()
		delete(n.fileIdx, virtualPath)
		n.mu.Unlock()
		if n.events != nil {
			grp, fname := splitVirtualPath(virtualPath)
			n.events.Append("del", grp, fname, "", localFile.Size)
			n.totalSynced.Add(1)
		}
	}

	return n.PauseGroup(name, waspaused)
}

// ForceSyncAll calls ForceSyncGroup for every configured sync group.
func (n *Node) ForceSyncAll() error {
	groups := n.SyncGroups()
	var firstErr error
	for _, g := range groups {
		if err := n.ForceSyncGroup(g.Name); err != nil && firstErr == nil {
			firstErr = err
		}
	}
	return firstErr
}

// RemoveGroup removes a sync group at runtime and persists the change to disk
// (when a config file path is set). Watched directories are not unwatched;
// orphaned events are silently ignored by findVirtualPath.
func (n *Node) RemoveGroup(name string) error {
	n.mu.Lock()

	found := false
	syncs := n.cfg.Syncs[:0]
	for _, g := range n.cfg.Syncs {
		if g.Name == name {
			found = true
		} else {
			syncs = append(syncs, g)
		}
	}
	if !found {
		n.mu.Unlock()
		return fmt.Errorf("group %q not found", name)
	}
	n.cfg.Syncs = syncs

	prefix := name + "/"
	for k := range n.fileIdx {
		if strings.HasPrefix(k, prefix) {
			delete(n.fileIdx, k)
		}
	}

	entries := n.entries[:0]
	for _, e := range n.entries {
		if e.GroupName != name {
			entries = append(entries, e)
		}
	}
	n.entries = entries

	delete(n.groups, name)

	n.mu.Unlock()

	if n.cfgPath != "" {
		return config.Save(n.cfgPath, n.cfg)
	}
	return nil
}
