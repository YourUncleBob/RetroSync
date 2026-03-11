// Package transfer provides the HTTP server and client used to exchange
// file index metadata and raw file content between peers.
package transfer

import (
	"context"
	"encoding/json"
	"fmt"
	"log"
	"net/http"
	"os"
	"strings"

	"retrosync/internal/index"
)

// Server serves the local file index and individual files over HTTP.
type Server struct {
	resolveLocal func(virtualPath string) (absPath string, ok bool)
	port         int
	getIndex     func() index.Index
	srv          *http.Server
}

// NewServer creates a Server. getIndex is called on each /index request so the
// response is always current. resolveLocal maps a virtual path to the absolute
// OS path of the file (looked up from the index, not derived from the URL).
func NewServer(port int, getIndex func() index.Index, resolveLocal func(string) (string, bool)) *Server {
	return &Server{port: port, getIndex: getIndex, resolveLocal: resolveLocal}
}

// Start begins listening in a background goroutine.
func (s *Server) Start() error {
	mux := http.NewServeMux()
	mux.HandleFunc("/index", s.handleIndex)
	mux.HandleFunc("/files/", s.handleFile)

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
}
