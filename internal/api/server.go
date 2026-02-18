// Package api implements the Gitorum HTTP server and JSON API.
package api

import (
	"encoding/json"
	"fmt"
	"io/fs"
	"log"
	"net/http"
	"sync"
	"time"

	"github.com/gosub/gitorum/internal/crypto"
	"github.com/gosub/gitorum/internal/repo"
)

// Server holds runtime state for the HTTP server.
type Server struct {
	Port     int
	RepoPath string
	repo     *repo.Repo
	identity *crypto.Identity
	mu         sync.Mutex // guards repo, identity, and lastSyncAt
	lastSyncAt time.Time
}

// New creates a Server. repo and identity may be nil when the forum has not
// been initialized yet; handlers degrade gracefully in that case.
func New(port int, repoPath string, r *repo.Repo, id *crypto.Identity) *Server {
	return &Server{Port: port, RepoPath: repoPath, repo: r, identity: id}
}

// Handler returns an http.Handler with all routes registered.
// staticFS is typically ui.StaticFS.
func (s *Server) Handler(staticFS fs.FS) http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("GET /api/status", s.handleStatus)
	mux.HandleFunc("POST /api/setup", s.handleSetup)
	mux.HandleFunc("GET /api/sync", s.handleSync)
	mux.HandleFunc("GET /api/categories", s.handleCategories)
	mux.HandleFunc("GET /api/categories/{cat}/threads", s.handleThreads)
	mux.HandleFunc("GET /api/threads/{cat}/{thread}", s.handleThread)
	mux.HandleFunc("POST /api/threads/{cat}/{thread}/reply", s.handleReply)
	mux.HandleFunc("POST /api/threads", s.handleNewThread)
	mux.HandleFunc("POST /api/categories", s.handleCreateCategory)
	mux.HandleFunc("POST /api/admin/delete", s.handleAdminDelete)
	mux.HandleFunc("POST /api/admin/addkey", s.handleAdminAddKey)
	mux.Handle("/", http.FileServer(http.FS(staticFS)))
	return mux
}

// ListenAndServe starts the HTTP server.
func (s *Server) ListenAndServe(staticFS fs.FS) error {
	addr := fmt.Sprintf(":%d", s.Port)
	log.Printf("Gitorum listening on http://localhost%s  (repo: %s)", addr, s.RepoPath)
	return http.ListenAndServe(addr, s.Handler(staticFS))
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
