package api

import (
	"log"
	"net/http"
	"path/filepath"

	"github.com/gosub/gitorum/internal/forum"
)

// GET /api/status
func (s *Server) handleStatus(w http.ResponseWriter, r *http.Request) {
	resp := StatusResponse{}

	if s.identity != nil {
		resp.Username = s.identity.Username
		resp.PubKey = s.identity.PublicKey
	}

	if s.repo != nil {
		if meta, err := s.repo.ReadMeta(); err == nil {
			resp.ForumName = meta.Name
			if s.identity != nil {
				resp.IsAdmin = s.identity.PublicKey == meta.AdminPubkey
			}
		}
		resp.Synced, resp.RemoteURL = s.repo.IsSynced()
	} else {
		resp.ForumName = "Gitorum"
		resp.Synced = true
	}

	writeJSON(w, http.StatusOK, resp)
}

// GET /api/sync
func (s *Server) handleSync(w http.ResponseWriter, r *http.Request) {
	// TODO(step7): pull --rebase then push.
	writeJSON(w, http.StatusOK, OKResponse{OK: true, Message: "sync not yet implemented"})
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
	var req ReplyRequest
	if err := readJSON(r, &req); err != nil {
		apiError(w, http.StatusBadRequest, "invalid request body: "+err.Error())
		return
	}
	if req.Body == "" {
		apiError(w, http.StatusBadRequest, "body is required")
		return
	}
	// TODO(step6): sign post, write file, git commit, attempt push.
	writeJSON(w, http.StatusCreated, OKResponse{OK: true, Message: "reply not yet persisted (step 6)"})
}

// POST /api/threads
func (s *Server) handleNewThread(w http.ResponseWriter, r *http.Request) {
	var req NewThreadRequest
	if err := readJSON(r, &req); err != nil {
		apiError(w, http.StatusBadRequest, "invalid request body: "+err.Error())
		return
	}
	if req.Category == "" || req.Slug == "" || req.Body == "" {
		apiError(w, http.StatusBadRequest, "category, slug, and body are required")
		return
	}
	// TODO(step6): create thread dir, write 0000_root.md, git commit, push.
	writeJSON(w, http.StatusCreated, OKResponse{OK: true, Message: "thread not yet persisted (step 6)"})
}

// POST /api/admin/delete
func (s *Server) handleAdminDelete(w http.ResponseWriter, r *http.Request) {
	var req AdminDeleteRequest
	if err := readJSON(r, &req); err != nil {
		apiError(w, http.StatusBadRequest, err.Error())
		return
	}
	// TODO(step8): verify admin, write tombstone, commit, push.
	writeJSON(w, http.StatusOK, OKResponse{OK: true, Message: "tombstone not yet implemented (step 8)"})
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
	// TODO(step8): verify admin, write keys/<username>.pub, commit, push.
	writeJSON(w, http.StatusOK, OKResponse{OK: true, Message: "addkey not yet implemented (step 8)"})
}
