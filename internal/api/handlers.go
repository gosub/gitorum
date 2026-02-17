package api

import (
	"net/http"
)

// NOTE: All handlers currently return stub data.
// Step 5 will replace stubs with real repo/identity access.

// GET /api/status
func (s *Server) handleStatus(w http.ResponseWriter, r *http.Request) {
	// TODO(step5): load identity and repo meta; check remote sync state.
	writeJSON(w, http.StatusOK, StatusResponse{
		Username:  "demo",
		PubKey:    "AAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAA=",
		IsAdmin:   false,
		ForumName: "Gitorum Demo",
		RemoteURL: "",
		Synced:    false,
	})
}

// GET /api/sync
func (s *Server) handleSync(w http.ResponseWriter, r *http.Request) {
	// TODO(step7): pull --rebase, then push.
	writeJSON(w, http.StatusOK, OKResponse{OK: true, Message: "sync not yet implemented"})
}

// GET /api/categories
func (s *Server) handleCategories(w http.ResponseWriter, r *http.Request) {
	// TODO(step5): enumerate category directories in repo.
	writeJSON(w, http.StatusOK, CategoriesResponse{
		Categories: []CategorySummary{
			{Slug: "general", Name: "General", Description: "General discussion", ThreadCount: 1},
			{Slug: "dev", Name: "Development", Description: "Technical topics", ThreadCount: 1},
		},
	})
}

// GET /api/categories/{cat}/threads
func (s *Server) handleThreads(w http.ResponseWriter, r *http.Request) {
	cat := r.PathValue("cat")
	// TODO(step5): load category and enumerate threads from repo.
	resp := ThreadsResponse{
		Category:     cat,
		CategoryName: cat,
		Threads:      []ThreadSummary{},
	}
	switch cat {
	case "general":
		resp.CategoryName = "General"
		resp.Threads = []ThreadSummary{
			{
				Slug:        "hello-world",
				Title:       "Hello, Gitorum!",
				Author:      "alice",
				ReplyCount:  1,
				CreatedAt:   "2026-02-17T10:00:00Z",
				LastReplyAt: "2026-02-17T11:30:00Z",
			},
		}
	case "dev":
		resp.CategoryName = "Development"
		resp.Threads = []ThreadSummary{
			{
				Slug:        "roadmap",
				Title:       "Project roadmap",
				Author:      "admin",
				ReplyCount:  0,
				CreatedAt:   "2026-02-17T09:00:00Z",
				LastReplyAt: "2026-02-17T09:00:00Z",
			},
		}
	}
	writeJSON(w, http.StatusOK, resp)
}

// GET /api/threads/{cat}/{thread}
func (s *Server) handleThread(w http.ResponseWriter, r *http.Request) {
	cat := r.PathValue("cat")
	thread := r.PathValue("thread")
	// TODO(step5): load thread from repo using forum.LoadThread.
	posts := []PostResponse{
		{
			Author:    "alice",
			PubKey:    "Aa1b2c3d",
			Timestamp: "2026-02-17T10:00:00Z",
			Parent:    "",
			Body:      "Hello, Gitorum!\n\nThis is the first post on our **decentralized forum**.\n\nAll content lives in a git repository — no servers, no databases.",
			BodyHTML:  "<p>Hello, Gitorum!</p>\n<p>This is the first post on our <strong>decentralized forum</strong>.</p>\n<p>All content lives in a git repository — no servers, no databases.</p>",
			Filename:  "0000_root.md",
			SigStatus: "valid",
		},
		{
			Author:    "bob",
			PubKey:    "Bb9e8f7a",
			Timestamp: "2026-02-17T11:30:00Z",
			Parent:    "e3b0c44298fc1c149afbf4c8996fb92427ae41e4649b934ca495991b7852b855",
			Body:      "Welcome! This is a great concept.\n\nLooking forward to seeing this develop.",
			BodyHTML:  "<p>Welcome! This is a great concept.</p>\n<p>Looking forward to seeing this develop.</p>",
			Filename:  "1708123456789_a3f9c1b2.md",
			SigStatus: "valid",
		},
	}
	writeJSON(w, http.StatusOK, ThreadResponse{
		Category: cat,
		Slug:     thread,
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
	// TODO(step8): verify caller is admin, write tombstone, commit, push.
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
	// TODO(step8): verify caller is admin, write keys/<username>.pub, commit, push.
	writeJSON(w, http.StatusOK, OKResponse{OK: true, Message: "addkey not yet implemented (step 8)"})
}
