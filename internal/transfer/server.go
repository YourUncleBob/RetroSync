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
	"net/http"
	"os"
	"path/filepath"
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
	resolveLocal  func(virtualPath string) (absPath string, ok bool)
	routeIncoming func(virtualPath string) (destPath string, err error)
	port          int
	getIndex      func() index.Index
	getStatus     func() StatusInfo
	getSyncs      func() []config.SyncGroup
	addGroup      func(name string, paths []string) error
	removeGroup   func(name string) error
	srv           *http.Server
}

// NewServer creates a Server. getIndex is called on each /index request so the
// response is always current. resolveLocal maps a virtual path to the absolute
// OS path of the file (looked up from the index, not derived from the URL).
// routeIncoming maps a virtual path to a destination path using config (used
// for PUT uploads; works even for files not yet in the index).
func NewServer(
	port int,
	getIndex func() index.Index,
	resolveLocal func(string) (string, bool),
	routeIncoming func(string) (string, error),
	getStatus func() StatusInfo,
	getSyncs func() []config.SyncGroup,
	addGroup func(string, []string) error,
	removeGroup func(string) error,
) *Server {
	return &Server{
		port:          port,
		getIndex:      getIndex,
		resolveLocal:  resolveLocal,
		routeIncoming: routeIncoming,
		getStatus:     getStatus,
		getSyncs:      getSyncs,
		addGroup:      addGroup,
		removeGroup:   removeGroup,
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

func (s *Server) handleIndex(w http.ResponseWriter, r *http.Request) {
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
		http.ServeFile(w, r, absPath)

	case http.MethodPut:
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
	if r.Method != http.MethodDelete {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	name := strings.TrimPrefix(r.URL.Path, "/api/config/groups/")
	if name == "" {
		http.Error(w, "group name required", http.StatusBadRequest)
		return
	}
	if err := s.removeGroup(name); err != nil {
		http.Error(w, err.Error(), http.StatusNotFound)
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
