package api_test

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/gosub/gitorum/internal/api"
	"github.com/gosub/gitorum/internal/crypto"
	"github.com/gosub/gitorum/internal/forum"
	"github.com/gosub/gitorum/internal/repo"
	"github.com/gosub/gitorum/internal/ui"
)

// setupForum creates a full forum fixture and returns a ready-to-use Server.
func setupForum(t *testing.T) *api.Server {
	t.Helper()
	dir := t.TempDir()

	id, err := crypto.Generate("alice")
	if err != nil {
		t.Fatal(err)
	}

	r, err := repo.Init(dir, repo.ForumMeta{
		Name:        "Test Forum",
		Description: "A test forum",
		AdminPubkey: id.PublicKey,
	}, id)
	if err != nil {
		t.Fatal(err)
	}

	// Create a category.
	catDir := filepath.Join(dir, "general")
	if err := os.MkdirAll(catDir, 0o755); err != nil {
		t.Fatal(err)
	}
	meta := "name = \"General\"\ndescription = \"General discussion\"\n"
	if err := os.WriteFile(filepath.Join(catDir, "META.toml"), []byte(meta), 0o644); err != nil {
		t.Fatal(err)
	}

	// Create a thread with a signed root post and one reply.
	threadDir := filepath.Join(catDir, "hello-world")
	if err := os.MkdirAll(threadDir, 0o755); err != nil {
		t.Fatal(err)
	}

	rootPost, err := forum.SignPost(id, "", "# Hello\n\nThis is the root post.")
	if err != nil {
		t.Fatal(err)
	}
	rootPost.Filename = forum.RootFilename
	rootContent := rootPost.Format()
	if err := os.WriteFile(filepath.Join(threadDir, forum.RootFilename), rootContent, 0o644); err != nil {
		t.Fatal(err)
	}

	reply, err := forum.SignPost(id, forum.PostHash(rootContent), "A reply to the root post.")
	if err != nil {
		t.Fatal(err)
	}
	reply.Filename = forum.NewPostFilename(reply.Body)
	if err := os.WriteFile(filepath.Join(threadDir, reply.Filename), reply.Format(), 0o644); err != nil {
		t.Fatal(err)
	}

	return api.New(8080, dir, r, id)
}

// hit sends a request through the server's handler and returns the recorder.
func hit(t *testing.T, srv *api.Server, method, path string) *httptest.ResponseRecorder {
	t.Helper()
	req := httptest.NewRequest(method, path, nil)
	w := httptest.NewRecorder()
	srv.Handler(ui.StaticFS).ServeHTTP(w, req)
	return w
}

// hitJSON sends a request with a JSON body and returns the recorder.
func hitJSON(t *testing.T, srv *api.Server, method, path string, body any) *httptest.ResponseRecorder {
	t.Helper()
	data, err := json.Marshal(body)
	if err != nil {
		t.Fatal(err)
	}
	req := httptest.NewRequest(method, path, bytes.NewReader(data))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	srv.Handler(ui.StaticFS).ServeHTTP(w, req)
	return w
}

func decodeJSON(t *testing.T, w *httptest.ResponseRecorder, v any) {
	t.Helper()
	if ct := w.Header().Get("Content-Type"); !strings.HasPrefix(ct, "application/json") {
		t.Fatalf("Content-Type: got %q, want application/json", ct)
	}
	if err := json.NewDecoder(w.Body).Decode(v); err != nil {
		t.Fatalf("decode JSON: %v\nbody: %s", err, w.Body.String())
	}
}

// ---- status ----------------------------------------------------------------

func TestHandleStatus(t *testing.T) {
	srv := setupForum(t)
	w := hit(t, srv, "GET", "/api/status")
	if w.Code != http.StatusOK {
		t.Fatalf("status %d", w.Code)
	}
	var resp api.StatusResponse
	decodeJSON(t, w, &resp)

	if resp.Username != "alice" {
		t.Errorf("Username: got %q", resp.Username)
	}
	if !resp.IsAdmin {
		t.Error("IsAdmin: expected true for admin identity")
	}
	if resp.ForumName != "Test Forum" {
		t.Errorf("ForumName: got %q", resp.ForumName)
	}
}

func TestHandleStatus_NoRepo(t *testing.T) {
	srv := api.New(8080, "/nonexistent", nil, nil)
	w := hit(t, srv, "GET", "/api/status")
	if w.Code != http.StatusOK {
		t.Fatalf("status %d", w.Code)
	}
	var resp api.StatusResponse
	decodeJSON(t, w, &resp)
	if resp.IsAdmin {
		t.Error("IsAdmin should be false with no repo/identity")
	}
}

// ---- categories ------------------------------------------------------------

func TestHandleCategories(t *testing.T) {
	srv := setupForum(t)
	w := hit(t, srv, "GET", "/api/categories")
	if w.Code != http.StatusOK {
		t.Fatalf("status %d", w.Code)
	}
	var resp api.CategoriesResponse
	decodeJSON(t, w, &resp)

	if len(resp.Categories) != 1 {
		t.Fatalf("Categories: got %d, want 1", len(resp.Categories))
	}
	cat := resp.Categories[0]
	if cat.Slug != "general" {
		t.Errorf("Slug: got %q", cat.Slug)
	}
	if cat.Name != "General" {
		t.Errorf("Name: got %q", cat.Name)
	}
	if cat.ThreadCount != 1 {
		t.Errorf("ThreadCount: got %d, want 1", cat.ThreadCount)
	}
}

func TestHandleCategories_NoRepo(t *testing.T) {
	srv := api.New(8080, "/nonexistent", nil, nil)
	w := hit(t, srv, "GET", "/api/categories")
	if w.Code != http.StatusOK {
		t.Fatalf("status %d", w.Code)
	}
	var resp api.CategoriesResponse
	decodeJSON(t, w, &resp)
	if len(resp.Categories) != 0 {
		t.Errorf("expected empty list, got %v", resp.Categories)
	}
}

// ---- threads ---------------------------------------------------------------

func TestHandleThreads(t *testing.T) {
	srv := setupForum(t)
	w := hit(t, srv, "GET", "/api/categories/general/threads")
	if w.Code != http.StatusOK {
		t.Fatalf("status %d", w.Code)
	}
	var resp api.ThreadsResponse
	decodeJSON(t, w, &resp)

	if resp.CategoryName != "General" {
		t.Errorf("CategoryName: got %q", resp.CategoryName)
	}
	if len(resp.Threads) != 1 {
		t.Fatalf("Threads: got %d, want 1", len(resp.Threads))
	}
	th := resp.Threads[0]
	if th.Slug != "hello-world" {
		t.Errorf("Slug: got %q", th.Slug)
	}
	if th.Author != "alice" {
		t.Errorf("Author: got %q", th.Author)
	}
	if th.ReplyCount != 1 {
		t.Errorf("ReplyCount: got %d, want 1", th.ReplyCount)
	}
	if !strings.Contains(th.Title, "Hello") {
		t.Errorf("Title %q should contain 'Hello'", th.Title)
	}
}

func TestHandleThreads_UnknownCategory(t *testing.T) {
	srv := setupForum(t)
	w := hit(t, srv, "GET", "/api/categories/no-such-cat/threads")
	if w.Code != http.StatusNotFound {
		t.Errorf("expected 404, got %d", w.Code)
	}
}

// ---- thread ----------------------------------------------------------------

func TestHandleThread(t *testing.T) {
	srv := setupForum(t)
	w := hit(t, srv, "GET", "/api/threads/general/hello-world")
	if w.Code != http.StatusOK {
		t.Fatalf("status %d\nbody: %s", w.Code, w.Body.String())
	}
	var resp api.ThreadResponse
	decodeJSON(t, w, &resp)

	if len(resp.Posts) != 2 {
		t.Fatalf("Posts: got %d, want 2", len(resp.Posts))
	}
	root := resp.Posts[0]
	if root.Filename != forum.RootFilename {
		t.Errorf("first post filename: got %q", root.Filename)
	}
	if root.SigStatus != "valid" {
		t.Errorf("root sig_status: got %q – %s", root.SigStatus, root.SigError)
	}
	if root.Author != "alice" {
		t.Errorf("root author: got %q", root.Author)
	}
	if root.BodyHTML == "" {
		t.Error("root body_html is empty")
	}
	if resp.Posts[1].SigStatus != "valid" {
		t.Errorf("reply sig_status: got %q", resp.Posts[1].SigStatus)
	}
}

func TestHandleThread_NotFound(t *testing.T) {
	srv := setupForum(t)
	w := hit(t, srv, "GET", "/api/threads/general/no-such-thread")
	if w.Code != http.StatusNotFound {
		t.Errorf("expected 404, got %d", w.Code)
	}
}

// ---- sync ------------------------------------------------------------------

func TestHandleSync_NoRepo(t *testing.T) {
	srv := api.New(8080, t.TempDir(), nil, nil)
	w := hit(t, srv, "GET", "/api/sync")
	if w.Code != http.StatusServiceUnavailable {
		t.Errorf("expected 503, got %d", w.Code)
	}
}

func TestHandleSync_NoRemote(t *testing.T) {
	// setupForum creates a repo without a remote; sync should be a graceful no-op.
	srv := setupForum(t)
	w := hit(t, srv, "GET", "/api/sync")
	if w.Code != http.StatusOK {
		t.Fatalf("status %d\nbody: %s", w.Code, w.Body.String())
	}
	var ok api.OKResponse
	decodeJSON(t, w, &ok)
	if !ok.OK {
		t.Error("ok: expected true")
	}
}

// ---- reply -----------------------------------------------------------------

func TestHandleReply(t *testing.T) {
	srv := setupForum(t)
	body := map[string]string{"body": "A new reply."}
	w := hitJSON(t, srv, "POST", "/api/threads/general/hello-world/reply", body)
	if w.Code != http.StatusCreated {
		t.Fatalf("status %d\nbody: %s", w.Code, w.Body.String())
	}
	var ok api.OKResponse
	decodeJSON(t, w, &ok)
	if !ok.OK {
		t.Errorf("ok: got false")
	}

	// Verify the reply was committed: thread should now have 3 posts.
	w2 := hit(t, srv, "GET", "/api/threads/general/hello-world")
	if w2.Code != http.StatusOK {
		t.Fatalf("GET thread after reply: status %d", w2.Code)
	}
	var thread api.ThreadResponse
	decodeJSON(t, w2, &thread)
	if len(thread.Posts) != 3 {
		t.Errorf("Posts: got %d, want 3", len(thread.Posts))
	}
	last := thread.Posts[len(thread.Posts)-1]
	if last.Author != "alice" {
		t.Errorf("reply author: got %q", last.Author)
	}
	if last.SigStatus != "valid" {
		t.Errorf("reply sig_status: got %q – %s", last.SigStatus, last.SigError)
	}
}

func TestHandleReply_NoIdentity(t *testing.T) {
	srv := api.New(8080, t.TempDir(), nil, nil)
	body := map[string]string{"body": "A reply."}
	w := hitJSON(t, srv, "POST", "/api/threads/general/hello-world/reply", body)
	if w.Code != http.StatusServiceUnavailable {
		t.Errorf("expected 503, got %d", w.Code)
	}
}

func TestHandleReply_MissingBody(t *testing.T) {
	srv := setupForum(t)
	body := map[string]string{"body": ""}
	w := hitJSON(t, srv, "POST", "/api/threads/general/hello-world/reply", body)
	if w.Code != http.StatusBadRequest {
		t.Errorf("expected 400, got %d", w.Code)
	}
}

func TestHandleReply_ThreadNotFound(t *testing.T) {
	srv := setupForum(t)
	body := map[string]string{"body": "A reply."}
	w := hitJSON(t, srv, "POST", "/api/threads/general/no-such-thread/reply", body)
	if w.Code != http.StatusNotFound {
		t.Errorf("expected 404, got %d", w.Code)
	}
}

// ---- new thread ------------------------------------------------------------

func TestHandleNewThread(t *testing.T) {
	srv := setupForum(t)
	body := map[string]string{
		"category": "general",
		"slug":     "new-thread",
		"body":     "# New Thread\n\nThread body.",
	}
	w := hitJSON(t, srv, "POST", "/api/threads", body)
	if w.Code != http.StatusCreated {
		t.Fatalf("status %d\nbody: %s", w.Code, w.Body.String())
	}
	var ok api.OKResponse
	decodeJSON(t, w, &ok)
	if !ok.OK {
		t.Errorf("ok: got false")
	}

	// Verify the thread can be fetched.
	w2 := hit(t, srv, "GET", "/api/threads/general/new-thread")
	if w2.Code != http.StatusOK {
		t.Fatalf("GET new thread: status %d\nbody: %s", w2.Code, w2.Body.String())
	}
	var thread api.ThreadResponse
	decodeJSON(t, w2, &thread)
	if len(thread.Posts) != 1 {
		t.Fatalf("Posts: got %d, want 1", len(thread.Posts))
	}
	root := thread.Posts[0]
	if root.Author != "alice" {
		t.Errorf("author: got %q", root.Author)
	}
	if root.SigStatus != "valid" {
		t.Errorf("sig_status: got %q – %s", root.SigStatus, root.SigError)
	}
	if root.Parent != "" {
		t.Errorf("parent: expected empty, got %q", root.Parent)
	}
}

func TestHandleNewThread_NoIdentity(t *testing.T) {
	srv := api.New(8080, t.TempDir(), nil, nil)
	body := map[string]string{"category": "general", "slug": "x", "body": "y"}
	w := hitJSON(t, srv, "POST", "/api/threads", body)
	if w.Code != http.StatusServiceUnavailable {
		t.Errorf("expected 503, got %d", w.Code)
	}
}

func TestHandleNewThread_InvalidSlug(t *testing.T) {
	srv := setupForum(t)
	for _, slug := range []string{"", "Has Spaces", "UPPER", "../evil", "-leading"} {
		body := map[string]string{"category": "general", "slug": slug, "body": "y"}
		w := hitJSON(t, srv, "POST", "/api/threads", body)
		if w.Code != http.StatusBadRequest {
			t.Errorf("slug %q: expected 400, got %d", slug, w.Code)
		}
	}
}

func TestHandleNewThread_DuplicateSlug(t *testing.T) {
	srv := setupForum(t)
	body := map[string]string{"category": "general", "slug": "hello-world", "body": "y"}
	w := hitJSON(t, srv, "POST", "/api/threads", body)
	if w.Code != http.StatusConflict {
		t.Errorf("expected 409, got %d", w.Code)
	}
}

func TestHandleNewThread_InvalidCategory(t *testing.T) {
	srv := setupForum(t)
	body := map[string]string{"category": "no-such-cat", "slug": "test", "body": "y"}
	w := hitJSON(t, srv, "POST", "/api/threads", body)
	if w.Code != http.StatusBadRequest {
		t.Errorf("expected 400, got %d", w.Code)
	}
}
