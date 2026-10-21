package api

import (
	"errors"
	"log"
	"net/http"
	"time"

	"github.com/gosub/gitorum/internal/crypto"
	"github.com/gosub/gitorum/internal/forum"
	"github.com/gosub/gitorum/internal/repo"
	"github.com/gosub/gitorum/internal/store"
)

const maxBodyBytes = 64 << 10 // 64 KB

// storeError maps a store error to an HTTP response.
func storeError(w http.ResponseWriter, err error) {
	switch {
	case errors.Is(err, store.ErrInvalid):
		apiError(w, http.StatusBadRequest, err.Error())
	case errors.Is(err, store.ErrNotFound):
		apiError(w, http.StatusNotFound, err.Error())
	case errors.Is(err, store.ErrExists):
		apiError(w, http.StatusConflict, err.Error())
	case errors.Is(err, store.ErrForbidden):
		apiError(w, http.StatusForbidden, err.Error())
	default:
		apiError(w, http.StatusInternalServerError, err.Error())
	}
}

// ready returns the store and identity, or writes 503 and returns ok=false.
// When needID is set, a configured identity is required as well.
func (s *Server) ready(w http.ResponseWriter, needID bool) (*store.Store, *crypto.Identity, bool) {
	st, id := s.state()
	if needID && id == nil {
		apiError(w, http.StatusServiceUnavailable, "no identity configured")
		return nil, nil, false
	}
	if st == nil {
		apiError(w, http.StatusServiceUnavailable, "forum not initialized")
		return nil, nil, false
	}
	return st, id, true
}

// pushAfterWrite shares a local commit by syncing with the remote. A failed
// sync is not an error for the write itself (the post is committed locally),
// but the client is told so it can show that the post has not left the
// machine yet.
func pushAfterWrite(w http.ResponseWriter, st *store.Store, status int) {
	resp := OKResponse{OK: true, Pushed: true}
	if _, err := st.Repo().Sync(); err != nil {
		log.Printf("sync after write: %v", err)
		resp.Pushed = false
		resp.Message = "saved locally, but not shared: " + err.Error()
	}
	writeJSON(w, status, resp)
}

// GET /api/status
func (s *Server) handleStatus(w http.ResponseWriter, r *http.Request) {
	s.mu.Lock()
	lastSyncAt := s.lastSyncAt
	s.mu.Unlock()
	st, id := s.state()

	resp := StatusResponse{}
	if id != nil {
		resp.Username = id.Username
		resp.PubKey = id.PublicKey
	}
	if st != nil {
		resp.Initialized = true
		if meta, err := st.Meta(); err == nil {
			resp.ForumName = meta.Name
		}
		resp.IsAdmin = st.IsAdmin(id)
		sync := st.Repo().Status()
		resp.RemoteURL = sync.RemoteURL
		resp.Ahead, resp.Behind = sync.Ahead, sync.Behind
		resp.Synced = sync.Synced()
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

	if s.store != nil {
		apiError(w, http.StatusConflict, "forum is already initialized")
		return
	}

	var req SetupRequest
	if err := readJSON(r, &req); err != nil {
		apiError(w, http.StatusBadRequest, "invalid request body: "+err.Error())
		return
	}
	if req.ForumName == "" {
		apiError(w, http.StatusBadRequest, "forum_name is required")
		return
	}

	// Reuse existing identity or generate a new one.
	id := s.identity
	if id == nil {
		if !forum.ValidUsername(req.Username) {
			apiError(w, http.StatusBadRequest, "username is required: lowercase letters, digits, - and _")
			return
		}
		generated, err := crypto.Generate(req.Username)
		if err != nil {
			apiError(w, http.StatusInternalServerError, "generate identity: "+err.Error())
			return
		}
		if err := generated.Save(s.IdentityPath); err != nil {
			apiError(w, http.StatusInternalServerError, "save identity: "+err.Error())
			return
		}
		id = generated
	}

	newRepo, err := repo.Init(s.RepoPath, repo.ForumMeta{
		Name:        req.ForumName,
		Description: req.Description,
		AdminPubkey: id.PublicKey,
	}, id)
	if err != nil {
		apiError(w, http.StatusInternalServerError, "init repo: "+err.Error())
		return
	}

	resp := OKResponse{OK: true, Pushed: true}
	if req.RemoteURL != "" {
		if err := newRepo.AddRemote("origin", req.RemoteURL); err != nil {
			resp.Pushed = false
			resp.Message = "add remote: " + err.Error()
		} else if err := newRepo.Push(); err != nil {
			resp.Pushed = false
			resp.Message = "forum created, but push failed: " + err.Error()
		}
	}

	s.identity = id
	s.store = store.New(newRepo)
	writeJSON(w, http.StatusOK, resp)
}

// POST /api/sync
func (s *Server) handleSync(w http.ResponseWriter, r *http.Request) {
	st, id, ok := s.ready(w, false)
	if !ok {
		return
	}
	res, err := st.Sync(id)
	if errors.Is(err, repo.ErrConflict) {
		apiError(w, http.StatusConflict, err.Error())
		return
	}
	if err != nil {
		apiError(w, http.StatusBadGateway, err.Error())
		return
	}
	s.mu.Lock()
	s.lastSyncAt = time.Now()
	s.mu.Unlock()
	writeJSON(w, http.StatusOK, SyncResponse{OK: true, Pulled: res.Pulled, Rebased: res.Rebased, Pushed: res.Pushed})
}

// GET /api/categories
func (s *Server) handleCategories(w http.ResponseWriter, r *http.Request) {
	st, _ := s.state()
	if st == nil {
		writeJSON(w, http.StatusOK, CategoriesResponse{Categories: []CategorySummary{}})
		return
	}
	cats, err := st.Categories()
	if err != nil {
		storeError(w, err)
		return
	}
	out := make([]CategorySummary, 0, len(cats))
	for _, c := range cats {
		out = append(out, CategorySummary{
			Slug:        c.Slug,
			Name:        c.Name,
			Description: c.Description,
			ThreadCount: len(c.ThreadSlugs),
		})
	}
	writeJSON(w, http.StatusOK, CategoriesResponse{Categories: out})
}

// GET /api/categories/{cat}/threads
func (s *Server) handleThreads(w http.ResponseWriter, r *http.Request) {
	catSlug := r.PathValue("cat")
	st, _ := s.state()
	if st == nil {
		writeJSON(w, http.StatusOK, ThreadsResponse{
			Category: catSlug, CategoryName: catSlug, Threads: []ThreadSummary{},
		})
		return
	}
	cat, scans, err := st.Threads(catSlug)
	if err != nil {
		storeError(w, err)
		return
	}
	summaries := make([]ThreadSummary, 0, len(scans))
	for _, scan := range scans {
		summaries = append(summaries, threadSummaryFrom(scan))
	}
	writeJSON(w, http.StatusOK, ThreadsResponse{
		Category:     cat.Slug,
		CategoryName: cat.Name,
		Threads:      summaries,
	})
}

// GET /api/threads/{cat}/{thread}
func (s *Server) handleThread(w http.ResponseWriter, r *http.Request) {
	st, _, ok := s.ready(w, false)
	if !ok {
		return
	}
	thread, err := st.Thread(r.PathValue("cat"), r.PathValue("thread"))
	if err != nil {
		storeError(w, err)
		return
	}
	posts := make([]PostResponse, 0, len(thread.Posts))
	for _, p := range thread.Posts {
		posts = append(posts, postToResponse(p))
	}
	writeJSON(w, http.StatusOK, ThreadResponse{
		Category: thread.Category,
		Slug:     thread.Slug,
		Posts:    posts,
	})
}

// POST /api/threads/{cat}/{thread}/reply
func (s *Server) handleReply(w http.ResponseWriter, r *http.Request) {
	r.Body = http.MaxBytesReader(w, r.Body, maxBodyBytes)
	var req ReplyRequest
	if err := readJSON(r, &req); err != nil {
		apiError(w, http.StatusBadRequest, "invalid request body: "+err.Error())
		return
	}
	st, id, ok := s.ready(w, true)
	if !ok {
		return
	}
	if _, err := st.Reply(id, r.PathValue("cat"), r.PathValue("thread"), req.Body); err != nil {
		storeError(w, err)
		return
	}
	pushAfterWrite(w, st, http.StatusCreated)
}

// POST /api/threads
func (s *Server) handleNewThread(w http.ResponseWriter, r *http.Request) {
	r.Body = http.MaxBytesReader(w, r.Body, maxBodyBytes)
	var req NewThreadRequest
	if err := readJSON(r, &req); err != nil {
		apiError(w, http.StatusBadRequest, "invalid request body: "+err.Error())
		return
	}
	st, id, ok := s.ready(w, true)
	if !ok {
		return
	}
	if err := st.NewThread(id, req.Category, req.Slug, req.Body); err != nil {
		storeError(w, err)
		return
	}
	pushAfterWrite(w, st, http.StatusCreated)
}

// POST /api/categories
func (s *Server) handleCreateCategory(w http.ResponseWriter, r *http.Request) {
	var req CreateCategoryRequest
	if err := readJSON(r, &req); err != nil {
		apiError(w, http.StatusBadRequest, "invalid request body: "+err.Error())
		return
	}
	st, id, ok := s.ready(w, true)
	if !ok {
		return
	}
	if err := st.CreateCategory(id, req.Slug, req.Name, req.Description); err != nil {
		storeError(w, err)
		return
	}
	pushAfterWrite(w, st, http.StatusCreated)
}

// GET /api/admin/requests
func (s *Server) handleJoinRequests(w http.ResponseWriter, r *http.Request) {
	st, id, ok := s.ready(w, true)
	if !ok {
		return
	}
	if !st.IsAdmin(id) {
		storeError(w, store.ErrForbidden)
		return
	}
	requests, err := st.JoinRequests()
	if err != nil {
		storeError(w, err)
		return
	}
	summaries := make([]JoinRequestSummary, 0, len(requests))
	for _, req := range requests {
		summaries = append(summaries, JoinRequestSummary{Username: req.Username, PubKey: req.PubKey})
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
	st, id, ok := s.ready(w, true)
	if !ok {
		return
	}
	if err := st.ApproveJoinRequest(id, req.Username); err != nil {
		storeError(w, err)
		return
	}
	pushAfterWrite(w, st, http.StatusOK)
}

// POST /api/admin/reject
func (s *Server) handleRejectRequest(w http.ResponseWriter, r *http.Request) {
	var req ApproveRejectRequest
	if err := readJSON(r, &req); err != nil {
		apiError(w, http.StatusBadRequest, err.Error())
		return
	}
	st, id, ok := s.ready(w, true)
	if !ok {
		return
	}
	if err := st.RejectJoinRequest(id, req.Username); err != nil {
		storeError(w, err)
		return
	}
	pushAfterWrite(w, st, http.StatusOK)
}

// POST /api/admin/delete
func (s *Server) handleAdminDelete(w http.ResponseWriter, r *http.Request) {
	var req AdminDeleteRequest
	if err := readJSON(r, &req); err != nil {
		apiError(w, http.StatusBadRequest, err.Error())
		return
	}
	st, id, ok := s.ready(w, true)
	if !ok {
		return
	}
	if err := st.Tombstone(id, req.Category, req.Thread, req.Filename); err != nil {
		storeError(w, err)
		return
	}
	pushAfterWrite(w, st, http.StatusOK)
}

// POST /api/admin/addkey
func (s *Server) handleAdminAddKey(w http.ResponseWriter, r *http.Request) {
	var req AdminAddKeyRequest
	if err := readJSON(r, &req); err != nil {
		apiError(w, http.StatusBadRequest, err.Error())
		return
	}
	st, id, ok := s.ready(w, true)
	if !ok {
		return
	}
	if !st.IsAdmin(id) {
		storeError(w, store.ErrForbidden)
		return
	}
	if err := st.AddKey(id, req.Username, req.PubKey); err != nil {
		storeError(w, err)
		return
	}
	pushAfterWrite(w, st, http.StatusOK)
}
