// Package api implements the Gitorum HTTP server and JSON API.
package api

import (
	"encoding/json"
	"fmt"
	"io/fs"
	"log"
	"mime"
	"net"
	"net/http"
	"net/url"
	"sync"
	"time"

	"github.com/gosub/gitorum/internal/crypto"
	"github.com/gosub/gitorum/internal/repo"
	"github.com/gosub/gitorum/internal/store"
)

// DefaultListenHost is the interface the server binds to unless told
// otherwise. The API is unauthenticated, so it must not be reachable from the
// network by default.
const DefaultListenHost = "127.0.0.1"

// Server holds runtime state for the HTTP server.
type Server struct {
	Port         int
	Host         string // interface to bind; DefaultListenHost when empty
	RepoPath     string
	IdentityPath string // where the setup wizard saves a new identity

	mu         sync.Mutex // guards store, identity, and lastSyncAt
	store      *store.Store
	identity   *crypto.Identity
	lastSyncAt time.Time
}

// New creates a Server. r and id may be nil when the forum has not been
// initialized yet; handlers degrade gracefully in that case.
func New(port int, repoPath string, r *repo.Repo, id *crypto.Identity) *Server {
	s := &Server{Port: port, RepoPath: repoPath, identity: id, IdentityPath: crypto.DefaultIdentityPath()}
	if r != nil {
		s.store = store.New(r)
	}
	return s
}

// state returns the current store and identity, either of which may be nil.
func (s *Server) state() (*store.Store, *crypto.Identity) {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.store, s.identity
}

// Handler returns an http.Handler with all routes registered.
// staticFS is typically ui.StaticFS.
func (s *Server) Handler(staticFS fs.FS) http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("GET /api/status", s.handleStatus)
	mux.HandleFunc("POST /api/setup", s.handleSetup)
	mux.HandleFunc("POST /api/sync", s.handleSync)
	mux.HandleFunc("GET /api/categories", s.handleCategories)
	mux.HandleFunc("GET /api/categories/{cat}/threads", s.handleThreads)
	mux.HandleFunc("GET /api/threads/{cat}/{thread}", s.handleThread)
	mux.HandleFunc("POST /api/threads/{cat}/{thread}/reply", s.handleReply)
	mux.HandleFunc("POST /api/threads", s.handleNewThread)
	mux.HandleFunc("POST /api/categories", s.handleCreateCategory)
	mux.HandleFunc("GET /api/admin/requests", s.handleJoinRequests)
	mux.HandleFunc("POST /api/admin/approve", s.handleApproveRequest)
	mux.HandleFunc("POST /api/admin/reject", s.handleRejectRequest)
	mux.HandleFunc("POST /api/admin/delete", s.handleAdminDelete)
	mux.HandleFunc("POST /api/admin/addkey", s.handleAdminAddKey)
	mux.Handle("/", http.FileServer(http.FS(staticFS)))
	return guard(mux, s.Host != "" && !isLoopback(s.Host))
}

// ListenAndServe starts the HTTP server.
func (s *Server) ListenAndServe(staticFS fs.FS) error {
	host := s.Host
	if host == "" {
		host = DefaultListenHost
	}
	addr := net.JoinHostPort(host, fmt.Sprint(s.Port))
	if !isLoopback(host) {
		log.Printf("WARNING: listening on %s; anyone who can reach this address can post as you", host)
	}
	log.Printf("Gitorum listening on http://%s  (repo: %s)", addr, s.RepoPath)
	return http.ListenAndServe(addr, s.Handler(staticFS))
}

// ---- request guard ---------------------------------------------------------

// guard rejects requests that could come from another website (CSRF) or from
// a hostile DNS name pointing at the local server (DNS rebinding):
//
//   - the Host header must be a loopback name or IP, unless the operator
//     explicitly chose to listen on a non-loopback interface (anyHost);
//   - a present Origin header must match the Host;
//   - state-changing requests must carry Content-Type: application/json,
//     which browsers cannot send cross-origin without a CORS preflight.
func guard(next http.Handler, anyHost bool) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if !anyHost && !isLoopback(hostOnly(r.Host)) {
			apiError(w, http.StatusForbidden, "forbidden host")
			return
		}
		if origin := r.Header.Get("Origin"); origin != "" {
			u, err := url.Parse(origin)
			if err != nil || u.Host != r.Host {
				apiError(w, http.StatusForbidden, "cross-origin request rejected")
				return
			}
		}
		if r.Method != http.MethodGet && r.Method != http.MethodHead {
			mt, _, err := mime.ParseMediaType(r.Header.Get("Content-Type"))
			if err != nil || mt != "application/json" {
				apiError(w, http.StatusUnsupportedMediaType, "Content-Type must be application/json")
				return
			}
		}
		next.ServeHTTP(w, r)
	})
}

func hostOnly(hostport string) string {
	if h, _, err := net.SplitHostPort(hostport); err == nil {
		return h
	}
	return hostport
}

func isLoopback(host string) bool {
	if host == "localhost" {
		return true
	}
	ip := net.ParseIP(host)
	return ip != nil && ip.IsLoopback()
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
