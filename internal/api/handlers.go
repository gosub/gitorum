package api

import (
	"log"
	"net/http"
	"os"
	"path/filepath"
	"regexp"
	"time"

	"github.com/gosub/gitorum/internal/crypto"
	"github.com/gosub/gitorum/internal/forum"
	"github.com/gosub/gitorum/internal/repo"
)

const maxBodyBytes = 64 << 10 // 64 KB

var slugRe = regexp.MustCompile(`^[a-z0-9][a-z0-9-]*$`)

// GET /api/status
func (s *Server) handleStatus(w http.ResponseWriter, r *http.Request) {
	s.mu.Lock()
	lastSyncAt := s.lastSyncAt
	repo := s.repo
	id := s.identity
	s.mu.Unlock()

	resp := StatusResponse{}

	if id != nil {
		resp.Username = id.Username
		resp.PubKey = id.PublicKey
	}

	if repo != nil {
		resp.Initialized = true
		if meta, err := repo.ReadMeta(); err == nil {
			resp.ForumName = meta.Name
			if id != nil {
				resp.IsAdmin = id.PublicKey == meta.AdminPubkey
			}
		}
		resp.Synced, resp.RemoteURL = repo.IsSynced()
	} else {
		resp.ForumName = "Gitorum"
		resp.Synced = true
	}

	if !lastSyncAt.IsZero() {
		resp.LastSyncAt = lastSyncAt.UTC().Format(time.RFC3339)
	}

	writeJSON(w, http.StatusOK, resp)
}

// POST /api/setup
func (s *Server) handleSetup(w http.ResponseWriter, r *http.Request) {
	s.mu.Lock()
	defer s.mu.Unlock()

	if s.repo != nil {
		apiError(w, http.StatusConflict, "forum is already initialized")
		return
	}

	var req SetupRequest
	if err := readJSON(r, &req); err != nil {
		apiError(w, http.StatusBadRequest, "invalid request body: "+err.Error())
		return
	}
	if req.Username == "" {
		apiError(w, http.StatusBadRequest, "username is required")
		return
	}
	if req.ForumName == "" {
		apiError(w, http.StatusBadRequest, "forum_name is required")
		return
	}

	// Reuse existing identity or generate a new one.
	id := s.identity
	if id == nil {
		generated, err := crypto.Generate(req.Username)
		if err != nil {
			apiError(w, http.StatusInternalServerError, "generate identity: "+err.Error())
			return
		}
		identPath := crypto.DefaultIdentityPath()
		if err := generated.Save(identPath); err != nil {
			apiError(w, http.StatusInternalServerError, "save identity: "+err.Error())
			return
		}
		id = generated
	}

	meta := repo.ForumMeta{
		Name:        req.ForumName,
		AdminPubkey: id.PublicKey,
	}
	newRepo, err := repo.Init(s.RepoPath, meta, id)
	if err != nil {
		apiError(w, http.StatusInternalServerError, "init repo: "+err.Error())
		return
	}

	if req.RemoteURL != "" {
		if err := newRepo.AddRemote("origin", req.RemoteURL); err != nil {
			log.Printf("handleSetup: add remote: %v", err)
		} else if err := newRepo.Push(); err != nil {
			log.Printf("handleSetup: push: %v", err)
		}
	}

	s.identity = id
	s.repo = newRepo
	writeJSON(w, http.StatusOK, OKResponse{OK: true})
}

// GET /api/sync
func (s *Server) handleSync(w http.ResponseWriter, r *http.Request) {
	if s.repo == nil {
		apiError(w, http.StatusServiceUnavailable, "forum not initialized")
		return
	}

	if err := s.repo.Pull(); err != nil {
		apiError(w, http.StatusInternalServerError, "pull: "+err.Error())
		return
	}

	s.mu.Lock()
	s.lastSyncAt = time.Now()
	s.mu.Unlock()

	// Auto-approve join requests if the forum is configured to do so and the
	// running identity is the admin.
	if s.identity != nil {
		if meta, err := s.repo.ReadMeta(); err == nil &&
			meta.AutoApproveKeys &&
			s.identity.PublicKey == meta.AdminPubkey {
			if requests, err := s.repo.JoinRequests(); err == nil {
				for _, req := range requests {
					if err := s.repo.ApproveJoinRequest(s.identity, req.Username); err != nil {
						log.Printf("handleSync: auto-approve %s: %v", req.Username, err)
					} else {
						log.Printf("handleSync: auto-approved join request from @%s", req.Username)
					}
				}
			}
		}
	}

	if err := s.repo.Push(); err != nil {
		log.Printf("handleSync: push: %v", err)
	}

	writeJSON(w, http.StatusOK, OKResponse{OK: true})
}

// GET /api/categories
func (s *Server) handleCategories(w http.ResponseWriter, r *http.Request) {
	if s.repo == nil {
		writeJSON(w, http.StatusOK, CategoriesResponse{Categories: []CategorySummary{}})
		return
	}

	slugs, err := s.repo.Categories()
	if err != nil {
		apiError(w, http.StatusInternalServerError, "list categories: "+err.Error())
		return
	}

	cats := make([]CategorySummary, 0, len(slugs))
	for _, slug := range slugs {
		catDir := filepath.Join(s.repo.Path, slug)
		cat, err := forum.LoadCategory(slug, catDir)
		if err != nil {
			log.Printf("handleCategories: skip %q: %v", slug, err)
			continue
		}
		cats = append(cats, CategorySummary{
			Slug:        cat.Slug,
			Name:        cat.Name,
			Description: cat.Description,
			ThreadCount: len(cat.ThreadSlugs),
		})
	}
	writeJSON(w, http.StatusOK, CategoriesResponse{Categories: cats})
}

// GET /api/categories/{cat}/threads
func (s *Server) handleThreads(w http.ResponseWriter, r *http.Request) {
	catSlug := r.PathValue("cat")

	if s.repo == nil {
		writeJSON(w, http.StatusOK, ThreadsResponse{
			Category: catSlug, CategoryName: catSlug, Threads: []ThreadSummary{},
		})
		return
	}

	catDir := filepath.Join(s.repo.Path, catSlug)
	cat, err := forum.LoadCategory(catSlug, catDir)
	if err != nil {
		apiError(w, http.StatusNotFound, "category not found")
		return
	}

	keysDir := filepath.Join(s.repo.Path, "keys")
	summaries := make([]ThreadSummary, 0, len(cat.ThreadSlugs))
	for _, slug := range cat.ThreadSlugs {
		threadDir := filepath.Join(catDir, slug)
		scan, err := forum.ScanThread(slug, threadDir, keysDir)
		if err != nil {
			log.Printf("handleThreads: skip thread %q: %v", slug, err)
			continue
		}
		summaries = append(summaries, threadSummaryFrom(scan))
	}

	writeJSON(w, http.StatusOK, ThreadsResponse{
		Category:     catSlug,
		CategoryName: cat.Name,
		Threads:      summaries,
	})
}

// GET /api/threads/{cat}/{thread}
func (s *Server) handleThread(w http.ResponseWriter, r *http.Request) {
	catSlug := r.PathValue("cat")
	threadSlug := r.PathValue("thread")

	if s.repo == nil {
		apiError(w, http.StatusServiceUnavailable, "forum not initialized")
		return
	}

	threadDir := filepath.Join(s.repo.Path, catSlug, threadSlug)
	keysDir := filepath.Join(s.repo.Path, "keys")

	thread, err := forum.LoadThread(catSlug, threadSlug, threadDir, keysDir)
	if err != nil {
		apiError(w, http.StatusNotFound, "thread not found")
		return
	}

	posts := make([]PostResponse, 0, len(thread.Posts))
	for _, p := range thread.Posts {
		posts = append(posts, postToResponse(p))
	}

	writeJSON(w, http.StatusOK, ThreadResponse{
		Category: catSlug,
		Slug:     threadSlug,
		Posts:    posts,
	})
}

// POST /api/threads/{cat}/{thread}/reply
func (s *Server) handleReply(w http.ResponseWriter, r *http.Request) {
	catSlug := r.PathValue("cat")
	threadSlug := r.PathValue("thread")

	r.Body = http.MaxBytesReader(w, r.Body, maxBodyBytes)
	var req ReplyRequest
	if err := readJSON(r, &req); err != nil {
		apiError(w, http.StatusBadRequest, "invalid request body: "+err.Error())
		return
	}
	if req.Body == "" {
		apiError(w, http.StatusBadRequest, "body is required")
		return
	}
	if s.identity == nil {
		apiError(w, http.StatusServiceUnavailable, "no identity configured")
		return
	}
	if s.repo == nil {
		apiError(w, http.StatusServiceUnavailable, "forum not initialized")
		return
	}

	rootPath := filepath.Join(s.repo.Path, catSlug, threadSlug, forum.RootFilename)
	rootContent, err := os.ReadFile(rootPath)
	if err != nil {
		apiError(w, http.StatusNotFound, "thread not found")
		return
	}

	post, err := forum.SignPost(s.identity, forum.PostHash(rootContent), req.Body)
	if err != nil {
		apiError(w, http.StatusInternalServerError, "sign post: "+err.Error())
		return
	}
	post.Filename = forum.NewPostFilename(post.Body)

	relPath := filepath.Join(catSlug, threadSlug, post.Filename)
	if err := s.repo.CommitPost(s.identity, relPath, post.Format()); err != nil {
		apiError(w, http.StatusInternalServerError, "commit post: "+err.Error())
		return
	}
	if err := s.repo.Push(); err != nil {
		log.Printf("handleReply: push: %v", err)
	}
	writeJSON(w, http.StatusCreated, OKResponse{OK: true})
}

// POST /api/categories
func (s *Server) handleCreateCategory(w http.ResponseWriter, r *http.Request) {
	var req CreateCategoryRequest
	if err := readJSON(r, &req); err != nil {
		apiError(w, http.StatusBadRequest, "invalid request body: "+err.Error())
		return
	}
	if req.Slug == "" || req.Name == "" {
		apiError(w, http.StatusBadRequest, "slug and name are required")
		return
	}
	if !slugRe.MatchString(req.Slug) {
		apiError(w, http.StatusBadRequest, "slug must be lowercase letters, digits, and hyphens")
		return
	}
	if !s.requireAdmin(w) {
		return
	}

	catMetaPath := filepath.Join(s.repo.Path, req.Slug, "META.toml")
	if _, err := os.Stat(catMetaPath); err == nil {
		apiError(w, http.StatusConflict, "category slug already exists")
		return
	}

	if err := s.repo.CreateCategory(s.identity, req.Slug, req.Name, req.Description); err != nil {
		apiError(w, http.StatusInternalServerError, "create category: "+err.Error())
		return
	}
	if err := s.repo.Push(); err != nil {
		log.Printf("handleCreateCategory: push: %v", err)
	}
	writeJSON(w, http.StatusCreated, OKResponse{OK: true})
}

// POST /api/threads
func (s *Server) handleNewThread(w http.ResponseWriter, r *http.Request) {
	r.Body = http.MaxBytesReader(w, r.Body, maxBodyBytes)
	var req NewThreadRequest
	if err := readJSON(r, &req); err != nil {
		apiError(w, http.StatusBadRequest, "invalid request body: "+err.Error())
		return
	}
	if req.Category == "" || req.Slug == "" || req.Body == "" {
		apiError(w, http.StatusBadRequest, "category, slug, and body are required")
		return
	}
	if !slugRe.MatchString(req.Slug) {
		apiError(w, http.StatusBadRequest, "slug must be lowercase letters, digits, and hyphens")
		return
	}
	if s.identity == nil {
		apiError(w, http.StatusServiceUnavailable, "no identity configured")
		return
	}
	if s.repo == nil {
		apiError(w, http.StatusServiceUnavailable, "forum not initialized")
		return
	}

	catMetaPath := filepath.Join(s.repo.Path, req.Category, "META.toml")
	if _, err := os.Stat(catMetaPath); err != nil {
		apiError(w, http.StatusBadRequest, "category not found: "+req.Category)
		return
	}

	threadDir := filepath.Join(s.repo.Path, req.Category, req.Slug)
	if _, err := os.Stat(threadDir); err == nil {
		apiError(w, http.StatusConflict, "thread slug already exists")
		return
	}

	post, err := forum.SignPost(s.identity, "", req.Body)
	if err != nil {
		apiError(w, http.StatusInternalServerError, "sign post: "+err.Error())
		return
	}
	post.Filename = forum.RootFilename

	relPath := filepath.Join(req.Category, req.Slug, forum.RootFilename)
	if err := s.repo.CommitPost(s.identity, relPath, post.Format()); err != nil {
		apiError(w, http.StatusInternalServerError, "commit post: "+err.Error())
		return
	}
	if err := s.repo.Push(); err != nil {
		log.Printf("handleNewThread: push: %v", err)
	}
	writeJSON(w, http.StatusCreated, OKResponse{OK: true})
}

// GET /api/admin/requests
func (s *Server) handleJoinRequests(w http.ResponseWriter, r *http.Request) {
	if !s.requireAdmin(w) {
		return
	}
	requests, err := s.repo.JoinRequests()
	if err != nil {
		apiError(w, http.StatusInternalServerError, "list requests: "+err.Error())
		return
	}
	summaries := make([]JoinRequestSummary, 0, len(requests))
	for _, req := range requests {
		summaries = append(summaries, JoinRequestSummary{
			Username: req.Username,
			PubKey:   req.PubKey,
		})
	}
	writeJSON(w, http.StatusOK, JoinRequestsResponse{Requests: summaries})
}

// POST /api/admin/approve
func (s *Server) handleApproveRequest(w http.ResponseWriter, r *http.Request) {
	var req ApproveRejectRequest
	if err := readJSON(r, &req); err != nil {
		apiError(w, http.StatusBadRequest, err.Error())
		return
	}
	if req.Username == "" {
		apiError(w, http.StatusBadRequest, "username is required")
		return
	}
	if !s.requireAdmin(w) {
		return
	}
	if err := s.repo.ApproveJoinRequest(s.identity, req.Username); err != nil {
		apiError(w, http.StatusInternalServerError, "approve request: "+err.Error())
		return
	}
	if err := s.repo.Push(); err != nil {
		log.Printf("handleApproveRequest: push: %v", err)
	}
	writeJSON(w, http.StatusOK, OKResponse{OK: true})
}

// POST /api/admin/reject
func (s *Server) handleRejectRequest(w http.ResponseWriter, r *http.Request) {
	var req ApproveRejectRequest
	if err := readJSON(r, &req); err != nil {
		apiError(w, http.StatusBadRequest, err.Error())
		return
	}
	if req.Username == "" {
		apiError(w, http.StatusBadRequest, "username is required")
		return
	}
	if !s.requireAdmin(w) {
		return
	}
	if err := s.repo.RejectJoinRequest(s.identity, req.Username); err != nil {
		apiError(w, http.StatusInternalServerError, "reject request: "+err.Error())
		return
	}
	if err := s.repo.Push(); err != nil {
		log.Printf("handleRejectRequest: push: %v", err)
	}
	writeJSON(w, http.StatusOK, OKResponse{OK: true})
}

// POST /api/admin/delete
func (s *Server) handleAdminDelete(w http.ResponseWriter, r *http.Request) {
	var req AdminDeleteRequest
	if err := readJSON(r, &req); err != nil {
		apiError(w, http.StatusBadRequest, err.Error())
		return
	}
	if req.Category == "" || req.Thread == "" || req.Filename == "" {
		apiError(w, http.StatusBadRequest, "category, thread, and filename are required")
		return
	}
	if !s.requireAdmin(w) {
		return
	}

	postPath := filepath.Join(s.repo.Path, req.Category, req.Thread, req.Filename)
	content, err := os.ReadFile(postPath)
	if err != nil {
		apiError(w, http.StatusNotFound, "post not found")
		return
	}

	tomb, err := forum.SignTombstone(s.identity, content)
	if err != nil {
		apiError(w, http.StatusInternalServerError, "sign tombstone: "+err.Error())
		return
	}
	tomb.Filename = forum.TombstoneFilename(req.Filename)

	relPath := filepath.Join(req.Category, req.Thread, tomb.Filename)
	if err := s.repo.CommitPost(s.identity, relPath, tomb.Format()); err != nil {
		apiError(w, http.StatusInternalServerError, "commit tombstone: "+err.Error())
		return
	}
	if err := s.repo.Push(); err != nil {
		log.Printf("handleAdminDelete: push: %v", err)
	}
	writeJSON(w, http.StatusOK, OKResponse{OK: true})
}

// POST /api/admin/addkey
func (s *Server) handleAdminAddKey(w http.ResponseWriter, r *http.Request) {
	var req AdminAddKeyRequest
	if err := readJSON(r, &req); err != nil {
		apiError(w, http.StatusBadRequest, err.Error())
		return
	}
	if req.Username == "" || req.PubKey == "" {
		apiError(w, http.StatusBadRequest, "username and pubkey are required")
		return
	}
	if !s.requireAdmin(w) {
		return
	}

	if err := s.repo.WritePublicKey(s.identity, req.Username, req.PubKey); err != nil {
		apiError(w, http.StatusInternalServerError, "write public key: "+err.Error())
		return
	}
	if err := s.repo.Push(); err != nil {
		log.Printf("handleAdminAddKey: push: %v", err)
	}
	writeJSON(w, http.StatusOK, OKResponse{OK: true})
}

// requireAdmin checks that s.identity is the forum admin. It writes the
// appropriate error response and returns false when the check fails.
func (s *Server) requireAdmin(w http.ResponseWriter) bool {
	if s.identity == nil {
		apiError(w, http.StatusServiceUnavailable, "no identity configured")
		return false
	}
	if s.repo == nil {
		apiError(w, http.StatusServiceUnavailable, "forum not initialized")
		return false
	}
	meta, err := s.repo.ReadMeta()
	if err != nil {
		apiError(w, http.StatusInternalServerError, "read meta: "+err.Error())
		return false
	}
	if s.identity.PublicKey != meta.AdminPubkey {
		apiError(w, http.StatusForbidden, "admin access required")
		return false
	}
	return true
}
