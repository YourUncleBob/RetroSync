// Package transfer provides the HTTP server and client used to exchange
// file index metadata and raw file content between peers.
package transfer

import (
	_ "embed"

	"context"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"net"
	"net/http"
	"os"
	"path/filepath"
	"strconv"
	"strings"

	"retrosync/internal/config"
	"retrosync/internal/index"
)

//go:embed ui.html
var uiHTML string

// PeerInfo describes a connected peer node.
type PeerInfo struct {
	ID   string `json:"id"`
	Name string `json:"name"`
	Addr string `json:"addr"`
	Port int    `json:"port"`
}

// StatusInfo holds the runtime status of a node.
type StatusInfo struct {
	ID            string     `json:"id"`
	Name          string     `json:"name"`
	Version       string     `json:"version"`
	HTTPPort      int        `json:"http_port"`
	DiscoveryPort int        `json:"discovery_port"`
	FileCount     int        `json:"file_count"`
	Role          string     `json:"role"`                  // "server", "client", or "" (legacy P2P)
	ServerAddr    string     `json:"server_addr,omitempty"` // client only
	ServerPort    int        `json:"server_port,omitempty"` // client only
	ServerName    string     `json:"server_name,omitempty"` // client only
	Peers         []PeerInfo `json:"peers"`
}

// Server serves the local file index and individual files over HTTP.
type Server struct {
	resolveLocal   func(virtualPath string) (absPath string, ok bool)
	routeIncoming  func(virtualPath string) (destPath string, err error)
	port           int
	getIndex       func() index.Index
	getStatus      func() StatusInfo
	getSyncs       func() []config.SyncGroup
	addGroup       func(name string, paths []string) error
	removeGroup    func(name string) error
	registerPeer   func(id, name string, port int, addr string)
	pauseGroup     func(name string, paused bool) error
	pauseAll       func(paused bool) error
	forceSync      func(group string) error // nil = not available (non-client nodes)
	getServerSyncs func() ([]config.SyncGroup, error)
	events         *EventBuffer
	srv            *http.Server
}

// ServerOpts groups all constructor parameters for NewServer.
type ServerOpts struct {
	Port           int
	GetIndex       func() index.Index
	ResolveLocal   func(string) (string, bool)
	RouteIncoming  func(string) (string, error)
	GetStatus      func() StatusInfo
	GetSyncs       func() []config.SyncGroup
	AddGroup       func(string, []string) error
	RemoveGroup    func(string) error
	RegisterPeer   func(id, name string, port int, addr string)
	PauseGroup     func(string, bool) error
	PauseAll       func(bool) error
	ForceSync      func(string) error // nil = not available (non-client nodes)
	GetServerSyncs func() ([]config.SyncGroup, error)
	Events         *EventBuffer // nil disables /api/log
}

// NewServer creates a Server from ServerOpts.
func NewServer(opts ServerOpts) *Server {
	return &Server{
		port:           opts.Port,
		getIndex:       opts.GetIndex,
		resolveLocal:   opts.ResolveLocal,
		routeIncoming:  opts.RouteIncoming,
		getStatus:      opts.GetStatus,
		getSyncs:       opts.GetSyncs,
		addGroup:       opts.AddGroup,
		removeGroup:    opts.RemoveGroup,
		registerPeer:   opts.RegisterPeer,
		pauseGroup:     opts.PauseGroup,
		pauseAll:       opts.PauseAll,
		forceSync:      opts.ForceSync,
		getServerSyncs: opts.GetServerSyncs,
		events:         opts.Events,
	}
}

// Start begins listening in a background goroutine.
func (s *Server) Start() error {
	mux := http.NewServeMux()
	mux.HandleFunc("/index", s.handleIndex)
	mux.HandleFunc("/files/", s.handleFile)
	mux.HandleFunc("/ui", s.handleUI)
	mux.HandleFunc("/api/status", s.handleStatus)
	mux.HandleFunc("/api/config", s.handleConfig)
	mux.HandleFunc("/api/config/groups", s.handleGroups)
	mux.HandleFunc("/api/config/groups/", s.handleGroupByName)
	mux.HandleFunc("/api/server/config", s.handleServerConfig)
	mux.HandleFunc("/api/log", s.handleLog)
	mux.HandleFunc("/api/force-sync", s.handleForceSync)
	mux.HandleFunc("/api/pause-all", s.handlePauseAll)

	s.srv = &http.Server{Addr: fmt.Sprintf(":%d", s.port), Handler: mux}

	log.Printf("transfer: HTTP server on :%d", s.port)
	go func() {
		if err := s.srv.ListenAndServe(); err != nil && err != http.ErrServerClosed {
			log.Printf("transfer: server error: %v", err)
		}
	}()
	return nil
}

// Stop shuts the server down gracefully.
func (s *Server) Stop() {
	s.srv.Shutdown(context.Background())
}

// tryRegisterPeer reads RetroSync identity headers from an incoming request and
// registers the caller as a peer. This lets the server track clients that
// connect via HTTP without requiring UDP discovery to be on the same port.
func (s *Server) tryRegisterPeer(r *http.Request) {
	if s.registerPeer == nil {
		return
	}
	id := r.Header.Get("X-RetroSync-ID")
	portStr := r.Header.Get("X-RetroSync-Port")
	if id == "" || portStr == "" {
		return
	}
	port, err := strconv.Atoi(portStr)
	if err != nil {
		return
	}
	name := r.Header.Get("X-RetroSync-Name")
	addr := r.RemoteAddr
	if host, _, err := net.SplitHostPort(addr); err == nil {
		addr = host
	}
	s.registerPeer(id, name, port, addr)
}

func (s *Server) handleIndex(w http.ResponseWriter, r *http.Request) {
	s.tryRegisterPeer(r)
	idx := s.getIndex()
	data, err := json.Marshal(idx)
	if err != nil {
		http.Error(w, "internal error", http.StatusInternalServerError)
		return
	}
	w.Header().Set("Content-Type", "application/json")
	w.Write(data)
}

func (s *Server) handleFile(w http.ResponseWriter, r *http.Request) {
	s.tryRegisterPeer(r)
	virtualPath := strings.TrimPrefix(r.URL.Path, "/files/")
	if virtualPath == "" {
		http.Error(w, "not found", http.StatusNotFound)
		return
	}

	switch r.Method {
	case http.MethodGet:
		absPath, ok := s.resolveLocal(virtualPath)
		if !ok {
			http.Error(w, "not found", http.StatusNotFound)
			return
		}
		info, err := os.Stat(absPath)
		if err != nil || info.IsDir() {
			http.Error(w, "not found", http.StatusNotFound)
			return
		}
		if s.events != nil {
			peerName := r.Header.Get("X-RetroSync-Name")
			if peerName == "" {
				peerName = r.Header.Get("X-RetroSync-ID")
			}
			group, filename := "", virtualPath
			if i := strings.Index(virtualPath, "/"); i >= 0 {
				group, filename = virtualPath[:i], virtualPath[i+1:]
			}
			s.events.Append("out", group, filename, peerName, info.Size())
		}
		http.ServeFile(w, r, absPath)

	case http.MethodPut:
		groupName := strings.SplitN(virtualPath, "/", 2)[0]
		for _, g := range s.getSyncs() {
			if g.Name == groupName && g.Paused {
				http.Error(w, "group is paused", http.StatusConflict)
				return
			}
		}
		destPath, err := s.routeIncoming(virtualPath)
		if err != nil {
			http.Error(w, err.Error(), http.StatusBadRequest)
			return
		}
		if err := os.MkdirAll(filepath.Dir(destPath), 0755); err != nil {
			http.Error(w, "internal error", http.StatusInternalServerError)
			return
		}
		tmp, err := os.CreateTemp(filepath.Dir(destPath), ".retrosync-*.tmp")
		if err != nil {
			http.Error(w, "internal error", http.StatusInternalServerError)
			return
		}
		tmpName := tmp.Name()
		if _, err := io.Copy(tmp, r.Body); err != nil {
			tmp.Close()
			os.Remove(tmpName)
			http.Error(w, "internal error", http.StatusInternalServerError)
			return
		}
		tmp.Close()
		if err := os.Rename(tmpName, destPath); err != nil {
			os.Remove(tmpName)
			http.Error(w, "internal error", http.StatusInternalServerError)
			return
		}
		log.Printf("transfer: received %s", virtualPath)
		if s.events != nil {
			peerName := r.Header.Get("X-RetroSync-Name")
			if peerName == "" {
				peerName = r.Header.Get("X-RetroSync-ID")
			}
			var size int64
			if info, err := os.Stat(destPath); err == nil {
				size = info.Size()
			}
			group, filename := "", virtualPath
			if i := strings.Index(virtualPath, "/"); i >= 0 {
				group, filename = virtualPath[:i], virtualPath[i+1:]
			}
			s.events.Append("in", group, filename, peerName, size)
		}
		writeJSON(w, map[string]string{"status": "ok"})

	default:
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
	}
}

func (s *Server) handleUI(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	fmt.Fprint(w, uiHTML)
}

func (s *Server) handleStatus(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, s.getStatus())
}

func (s *Server) handleConfig(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, s.getSyncs())
}

func (s *Server) handleGroups(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	var body struct {
		Name  string   `json:"name"`
		Paths []string `json:"paths"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		http.Error(w, "invalid JSON", http.StatusBadRequest)
		return
	}
	if body.Name == "" || len(body.Paths) == 0 {
		http.Error(w, "name and paths are required", http.StatusBadRequest)
		return
	}
	if err := s.addGroup(body.Name, body.Paths); err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	writeJSON(w, map[string]string{"status": "ok"})
}

func (s *Server) handleGroupByName(w http.ResponseWriter, r *http.Request) {
	name := strings.TrimPrefix(r.URL.Path, "/api/config/groups/")
	if name == "" {
		http.Error(w, "group name required", http.StatusBadRequest)
		return
	}
	switch r.Method {
	case http.MethodDelete:
		if err := s.removeGroup(name); err != nil {
			http.Error(w, err.Error(), http.StatusNotFound)
			return
		}
		writeJSON(w, map[string]string{"status": "ok"})
	case http.MethodPatch:
		var body struct {
			Paused bool `json:"paused"`
		}
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
			http.Error(w, "invalid JSON", http.StatusBadRequest)
			return
		}
		if err := s.pauseGroup(name, body.Paused); err != nil {
			http.Error(w, err.Error(), http.StatusNotFound)
			return
		}
		writeJSON(w, map[string]string{"status": "ok"})
	default:
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
	}
}

func (s *Server) handleServerConfig(w http.ResponseWriter, r *http.Request) {
	if s.getServerSyncs == nil {
		http.Error(w, "not available", http.StatusNotFound)
		return
	}
	syncs, err := s.getServerSyncs()
	if err != nil {
		http.Error(w, "could not reach server", http.StatusBadGateway)
		return
	}
	writeJSON(w, syncs)
}

func (s *Server) handleLog(w http.ResponseWriter, r *http.Request) {
	after := -1
	if v := r.URL.Query().Get("after"); v != "" {
		if n, err := strconv.Atoi(v); err == nil {
			after = n
		}
	}
	var entries []SyncEvent
	if s.events != nil {
		entries = s.events.Since(after)
	}
	if entries == nil {
		entries = []SyncEvent{}
	}
	writeJSON(w, entries)
}

func (s *Server) handleForceSync(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	if s.forceSync == nil {
		http.Error(w, "not available", http.StatusNotFound)
		return
	}
	var body struct {
		Group string `json:"group"` // empty = all groups
	}
	json.NewDecoder(r.Body).Decode(&body)
	if err := s.forceSync(body.Group); err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	writeJSON(w, map[string]string{"status": "ok"})
}

func (s *Server) handlePauseAll(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	var body struct {
		Paused bool `json:"paused"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		http.Error(w, "invalid JSON", http.StatusBadRequest)
		return
	}
	if err := s.pauseAll(body.Paused); err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	writeJSON(w, map[string]string{"status": "ok"})
}

func writeJSON(w http.ResponseWriter, v interface{}) {
	data, err := json.Marshal(v)
	if err != nil {
		http.Error(w, "internal error", http.StatusInternalServerError)
		return
	}
	w.Header().Set("Content-Type", "application/json")
	w.Write(data)
}
