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
	"time"

	"github.com/fsnotify/fsnotify"

	"retrosync/internal/config"
	"retrosync/internal/discovery"
	"retrosync/internal/index"
	"retrosync/internal/transfer"
)

// Node is a single participant in the sync network.
type Node struct {
	id            string
	httpPort      int
	discoveryPort int

	isServer   bool
	serverAddr string // host only (client mode)
	serverPort int    // port only (client mode)

	cfgPath string         // empty when launched with -dir (legacy)
	cfg     *config.Config // live copy for mutation

	groups  map[string][]config.PathSpec // group name → specs
	entries []index.SyncEntry            // flattened, for watcher + Build

	mu      sync.RWMutex
	peers   map[string]discovery.Peer
	fileIdx index.Index

	disc    *discovery.Discovery
	server  *transfer.Server
	client  *transfer.Client
	watcher *fsnotify.Watcher

	done chan struct{}
}

// New creates a Node from a Config. cfgPath is the path to the TOML file on
// disk; pass "" when launched with the legacy -dir flag (disables config
// mutation).
func New(cfg *config.Config, cfgPath string) (*Node, error) {
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
		client:        transfer.NewClient(),
		done:          make(chan struct{}),
	}
	return n, nil
}

// Start indexes existing files, launches the HTTP server, and starts the
// appropriate sync mode (server, client, or legacy P2P).
func (n *Node) Start() error {
	var err error
	n.fileIdx, err = index.BuildFromGroups(n.entries)
	if err != nil {
		return err
	}
	log.Printf("indexed %d existing file(s)", len(n.fileIdx))

	n.server = transfer.NewServer(n.httpPort, n.snapshot, n.resolveLocal, n.routeIncoming,
		n.Status, n.SyncGroups, n.AddGroup, n.RemoveGroup)
	if err := n.server.Start(); err != nil {
		return err
	}
	log.Printf("web UI: http://localhost:%d/ui", n.httpPort)

	if n.isServer {
		if err := n.startWatcher(); err != nil {
			return err
		}
		log.Printf("running as authoritative server")
		return nil
	}

	if n.cfg.Node.Role == "client" {
		if err := n.startWatcher(); err != nil {
			return err
		}
		go n.periodicSyncWithServer()
		log.Printf("running as client, server %s:%d", n.serverAddr, n.serverPort)
		return nil
	}

	// Legacy P2P mode
	n.disc = discovery.New(n.id, n.httpPort, n.discoveryPort, n.onPeerDiscovered)
	if err := n.disc.Start(); err != nil {
		return err
	}

	if err := n.startWatcher(); err != nil {
		return err
	}

	go n.periodicSync()
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

// onPeerDiscovered is called by discovery when a new peer is first seen.
func (n *Node) onPeerDiscovered(peer discovery.Peer) {
	n.mu.Lock()
	n.peers[peer.ID] = peer
	n.mu.Unlock()
	n.syncWithPeer(peer)
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

	log.Printf("indexed: %s (sha256:%s…)", virtualPath, fi.Hash[:8])
}

// onFileRemoved drops a deleted file from the local index.
func (n *Node) onFileRemoved(absPath string) {
	virtualPath, ok := n.findVirtualPath(absPath)
	if !ok {
		return
	}

	n.mu.Lock()
	delete(n.fileIdx, virtualPath)
	n.mu.Unlock()
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
	serverIdx, err := n.client.FetchIndex(n.serverAddr, n.serverPort)
	if err != nil {
		log.Printf("sync: fetch index from server failed: %v", err)
		return
	}

	local := n.snapshot()

	// PULL PASS — fetch files that are newer on the server.
	for virtualPath, remoteFile := range serverIdx {
		localFile, exists := local[virtualPath]
		if exists && localFile.Hash == remoteFile.Hash {
			continue
		}
		if exists && !remoteFile.ModTime.After(localFile.ModTime) {
			continue // local is same age or newer
		}

		log.Printf("sync: pulling %s from server", virtualPath)
		if err := n.client.FetchFile(n.serverAddr, n.serverPort, virtualPath, n.routeIncoming); err != nil {
			log.Printf("sync: pull %s failed: %v", virtualPath, err)
			continue
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
		remoteFile, exists := serverIdx[virtualPath]
		if exists && localFile.Hash == remoteFile.Hash {
			continue
		}
		if exists && !localFile.ModTime.After(remoteFile.ModTime) {
			continue // server is same age or newer
		}

		log.Printf("sync: pushing %s to server", virtualPath)
		if err := n.client.PushFile(n.serverAddr, n.serverPort, virtualPath, localFile.LocalPath); err != nil {
			log.Printf("sync: push %s failed: %v", virtualPath, err)
		}
	}
}

// periodicSyncWithServer syncs with the server immediately on startup, then
// every 30 seconds.
func (n *Node) periodicSyncWithServer() {
	n.syncWithServer()
	ticker := time.NewTicker(30 * time.Second)
	defer ticker.Stop()
	for {
		select {
		case <-ticker.C:
			n.syncWithServer()
		case <-n.done:
			return
		}
	}
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
		peers = append(peers, transfer.PeerInfo{ID: p.ID, Addr: p.Addr, Port: p.Port})
	}
	return transfer.StatusInfo{
		ID:            n.id,
		HTTPPort:      n.httpPort,
		DiscoveryPort: n.discoveryPort,
		FileCount:     len(n.fileIdx),
		Peers:         peers,
	}
}

// SyncGroups returns a copy of the currently configured sync groups.
func (n *Node) SyncGroups() []config.SyncGroup {
	n.mu.RLock()
	defer n.mu.RUnlock()
	result := make([]config.SyncGroup, len(n.cfg.Syncs))
	copy(result, n.cfg.Syncs)
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
