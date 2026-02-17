// Package api implements the Gitorum HTTP server and JSON API.
package api

import (
	"encoding/json"
	"fmt"
	"io/fs"
	"log"
	"net/http"
)

// Server holds runtime configuration. In step 5 it will also hold a *repo.Repo
// and a *crypto.Identity; for now it serves stub data.
type Server struct {
	Port     int
	RepoPath string
}

// New creates a Server with the given configuration.
func New(port int, repoPath string) *Server {
	return &Server{Port: port, RepoPath: repoPath}
}

// ListenAndServe registers all routes and starts the HTTP server.
// staticFS is typically ui.StaticFS.
func (s *Server) ListenAndServe(staticFS fs.FS) error {
	mux := http.NewServeMux()
	s.registerRoutes(mux, staticFS)
	addr := fmt.Sprintf(":%d", s.Port)
	log.Printf("Gitorum listening on http://localhost%s  (repo: %s)", addr, s.RepoPath)
	return http.ListenAndServe(addr, mux)
}

func (s *Server) registerRoutes(mux *http.ServeMux, staticFS fs.FS) {
	// JSON API – order matters: more specific patterns first.
	mux.HandleFunc("GET /api/status", s.handleStatus)
	mux.HandleFunc("GET /api/sync", s.handleSync)
	mux.HandleFunc("GET /api/categories", s.handleCategories)
	mux.HandleFunc("GET /api/categories/{cat}/threads", s.handleThreads)
	mux.HandleFunc("GET /api/threads/{cat}/{thread}", s.handleThread)
	mux.HandleFunc("POST /api/threads/{cat}/{thread}/reply", s.handleReply)
	mux.HandleFunc("POST /api/threads", s.handleNewThread)
	mux.HandleFunc("POST /api/admin/delete", s.handleAdminDelete)
	mux.HandleFunc("POST /api/admin/addkey", s.handleAdminAddKey)

	// SPA catch-all: serve embedded static files.
	mux.Handle("/", http.FileServer(http.FS(staticFS)))
}

// ---- helpers ---------------------------------------------------------------

func writeJSON(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	if err := json.NewEncoder(w).Encode(v); err != nil {
		log.Printf("writeJSON: %v", err)
	}
}

func readJSON(r *http.Request, v any) error {
	defer r.Body.Close()
	return json.NewDecoder(r.Body).Decode(v)
}

func apiError(w http.ResponseWriter, status int, msg string) {
	writeJSON(w, status, ErrorResponse{Error: msg})
}
