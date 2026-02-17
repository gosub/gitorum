package forum_test

import (
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"
	"time"

	"github.com/ggeurts/gitorum/internal/crypto"
	"github.com/ggeurts/gitorum/internal/forum"
)

// ---- helpers ----

func mustGenerate(t *testing.T, username string) *crypto.Identity {
	t.Helper()
	id, err := crypto.Generate(username)
	if err != nil {
		t.Fatalf("crypto.Generate: %v", err)
	}
	return id
}

// writeKey writes <username>.pub into keysDir.
func writeKey(t *testing.T, keysDir, username, pubkeyB64 string) {
	t.Helper()
	if err := os.MkdirAll(keysDir, 0o755); err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(keysDir, username+".pub")
	if err := os.WriteFile(path, []byte(pubkeyB64+"\n"), 0o644); err != nil {
		t.Fatal(err)
	}
}

// signedPost signs a post and writes it to dir/filename.
// It returns the raw file bytes so tests can compute PostHash.
func signedPost(t *testing.T, dir, filename string, id *crypto.Identity, parent, body string) []byte {
	t.Helper()
	post, err := forum.SignPost(id, parent, body)
	if err != nil {
		t.Fatalf("SignPost: %v", err)
	}
	post.Filename = filename
	content := post.Format()
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, filename), content, 0o644); err != nil {
		t.Fatal(err)
	}
	return content
}

// ---- ParsePost ----

func TestParsePost_Valid(t *testing.T) {
	raw := []byte("+++\n" +
		`author    = "alice"` + "\n" +
		`pubkey    = "AAAA1234"` + "\n" +
		`timestamp = "2026-02-17T10:00:00Z"` + "\n" +
		`parent    = ""` + "\n" +
		`signature = "SIGDATA"` + "\n" +
		"+++\n\nHello world")

	post, err := forum.ParsePost("0000_root.md", raw)
	if err != nil {
		t.Fatalf("ParsePost: %v", err)
	}
	if post.Author != "alice" {
		t.Errorf("Author: got %q", post.Author)
	}
	if post.PubKey != "AAAA1234" {
		t.Errorf("PubKey: got %q", post.PubKey)
	}
	if post.Parent != "" {
		t.Errorf("Parent: got %q", post.Parent)
	}
	if post.Body != "Hello world" {
		t.Errorf("Body: got %q", post.Body)
	}
	if post.BodyHTML == "" {
		t.Error("BodyHTML is empty")
	}
	if !strings.Contains(post.BodyHTML, "Hello world") {
		t.Errorf("BodyHTML does not contain body: %q", post.BodyHTML)
	}
	if post.Filename != "0000_root.md" {
		t.Errorf("Filename: got %q", post.Filename)
	}
	wantTS := time.Date(2026, 2, 17, 10, 0, 0, 0, time.UTC)
	if !post.Timestamp.Equal(wantTS) {
		t.Errorf("Timestamp: got %v, want %v", post.Timestamp, wantTS)
	}
}

func TestParsePost_Errors(t *testing.T) {
	tests := []struct {
		name    string
		content string
		wantErr string
	}{
		{
			name:    "missing opening fence",
			content: "author = \"alice\"\n",
			wantErr: "+++",
		},
		{
			name:    "unclosed fence",
			content: "+++\nauthor = \"alice\"\n",
			wantErr: "closing",
		},
		{
			name: "invalid TOML",
			content: "+++\nauthor = [unclosed\n+++\n\nbody",
			wantErr: "TOML",
		},
		{
			name: "bad timestamp",
			content: "+++\nauthor=\"a\"\npubkey=\"b\"\ntimestamp=\"not-a-time\"\nparent=\"\"\nsignature=\"s\"\n+++\n\nbody",
			wantErr: "timestamp",
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			_, err := forum.ParsePost("test.md", []byte(tc.content))
			if err == nil {
				t.Fatal("expected error, got nil")
			}
			if !strings.Contains(err.Error(), tc.wantErr) {
				t.Errorf("error %q does not contain %q", err.Error(), tc.wantErr)
			}
		})
	}
}

func TestParsePost_MultiLineBody(t *testing.T) {
	body := "Line 1\nLine 2\n\nLine 4"
	raw := fmt.Sprintf("+++\nauthor=\"a\"\npubkey=\"b\"\ntimestamp=\"2026-01-01T00:00:00Z\"\nparent=\"\"\nsignature=\"s\"\n+++\n\n%s", body)
	post, err := forum.ParsePost("x.md", []byte(raw))
	if err != nil {
		t.Fatal(err)
	}
	if post.Body != body {
		t.Errorf("Body:\ngot:  %q\nwant: %q", post.Body, body)
	}
}

// ---- Format / round-trip ----

func TestFormat_ParsePost_RoundTrip(t *testing.T) {
	id := mustGenerate(t, "alice")
	original, err := forum.SignPost(id, "", "Round-trip test body")
	if err != nil {
		t.Fatal(err)
	}
	original.Filename = forum.RootFilename

	content := original.Format()
	parsed, err := forum.ParsePost(forum.RootFilename, content)
	if err != nil {
		t.Fatalf("ParsePost after Format: %v", err)
	}

	if parsed.Author != original.Author {
		t.Errorf("Author mismatch")
	}
	if parsed.PubKey != original.PubKey {
		t.Errorf("PubKey mismatch")
	}
	if parsed.TimestampRaw != original.TimestampRaw {
		t.Errorf("TimestampRaw: got %q, want %q", parsed.TimestampRaw, original.TimestampRaw)
	}
	if parsed.Parent != original.Parent {
		t.Errorf("Parent mismatch")
	}
	if parsed.Signature != original.Signature {
		t.Errorf("Signature mismatch")
	}
	if parsed.Body != original.Body {
		t.Errorf("Body: got %q, want %q", parsed.Body, original.Body)
	}
}

// ---- VerifySignature ----

func TestVerifySignature_Valid(t *testing.T) {
	id := mustGenerate(t, "alice")
	keysDir := t.TempDir()
	writeKey(t, keysDir, "alice", id.PublicKey)

	post, err := forum.SignPost(id, "", "Hello")
	if err != nil {
		t.Fatal(err)
	}
	post.VerifySignature(keysDir)
	if post.SigStatus != forum.SigValid {
		t.Errorf("SigStatus: got %v, want SigValid; error: %s", post.SigStatus, post.SigError)
	}
}

func TestVerifySignature_AfterRoundTrip(t *testing.T) {
	id := mustGenerate(t, "bob")
	keysDir := t.TempDir()
	writeKey(t, keysDir, "bob", id.PublicKey)

	post, _ := forum.SignPost(id, "", "Post content")
	post.Filename = forum.RootFilename
	content := post.Format()

	parsed, err := forum.ParsePost(forum.RootFilename, content)
	if err != nil {
		t.Fatal(err)
	}
	parsed.VerifySignature(keysDir)
	if parsed.SigStatus != forum.SigValid {
		t.Errorf("SigStatus after round-trip: %v – %s", parsed.SigStatus, parsed.SigError)
	}
}

func TestVerifySignature_MissingKey(t *testing.T) {
	id := mustGenerate(t, "charlie")
	keysDir := t.TempDir() // empty – no key file

	post, _ := forum.SignPost(id, "", "Body")
	post.VerifySignature(keysDir)

	if post.SigStatus != forum.SigMissing {
		t.Errorf("SigStatus: got %v, want SigMissing", post.SigStatus)
	}
}

func TestVerifySignature_TamperedBody(t *testing.T) {
	id := mustGenerate(t, "dave")
	keysDir := t.TempDir()
	writeKey(t, keysDir, "dave", id.PublicKey)

	post, _ := forum.SignPost(id, "", "Original body")
	post.Body = "Tampered body" // change body after signing
	post.VerifySignature(keysDir)

	if post.SigStatus != forum.SigInvalid {
		t.Errorf("SigStatus: got %v, want SigInvalid", post.SigStatus)
	}
}

func TestVerifySignature_WrongKey(t *testing.T) {
	id := mustGenerate(t, "eve")
	otherID := mustGenerate(t, "eve") // different keypair, same username
	keysDir := t.TempDir()
	writeKey(t, keysDir, "eve", otherID.PublicKey) // store different key

	post, _ := forum.SignPost(id, "", "Body")
	post.VerifySignature(keysDir)

	if post.SigStatus != forum.SigInvalid {
		t.Errorf("SigStatus: got %v, want SigInvalid", post.SigStatus)
	}
}

// ---- PostHash ----

func TestPostHash_Deterministic(t *testing.T) {
	content := []byte("hello world")
	h1 := forum.PostHash(content)
	h2 := forum.PostHash(content)
	if h1 != h2 {
		t.Error("PostHash is not deterministic")
	}
}

func TestPostHash_DifferentContent(t *testing.T) {
	h1 := forum.PostHash([]byte("aaa"))
	h2 := forum.PostHash([]byte("bbb"))
	if h1 == h2 {
		t.Error("PostHash collision for different content")
	}
}

func TestPostHash_Length(t *testing.T) {
	h := forum.PostHash([]byte("test"))
	if len(h) != 64 { // sha256 = 32 bytes = 64 hex chars
		t.Errorf("PostHash length: got %d, want 64", len(h))
	}
}

// ---- NewPostFilename ----

func TestNewPostFilename_Format(t *testing.T) {
	re := regexp.MustCompile(`^\d{13}_[0-9a-f]{8}\.md$`)
	for i := 0; i < 5; i++ {
		name := forum.NewPostFilename("some body text")
		if !re.MatchString(name) {
			t.Errorf("NewPostFilename %q does not match pattern", name)
		}
	}
}

func TestNewPostFilename_DifferentBodies(t *testing.T) {
	n1 := forum.NewPostFilename("body one")
	n2 := forum.NewPostFilename("body two")
	// Hash part (chars 14-21) should differ.
	if len(n1) > 14 && len(n2) > 14 && n1[14:] == n2[14:] {
		t.Error("different bodies produced identical filename hash parts")
	}
}

func TestNewPostFilename_SameBodyDifferentTime(t *testing.T) {
	// Two calls with the same body should produce the same hash part but may
	// differ in the millis prefix.
	body := "identical"
	n1 := forum.NewPostFilename(body)
	time.Sleep(2 * time.Millisecond)
	n2 := forum.NewPostFilename(body)
	// Hash part must be identical.
	if len(n1) >= 14 && len(n2) >= 14 {
		hash1 := n1[strings.IndexByte(n1, '_')+1:]
		hash2 := n2[strings.IndexByte(n2, '_')+1:]
		if hash1 != hash2 {
			t.Errorf("same body gave different hash parts: %s vs %s", hash1, hash2)
		}
	}
}

// ---- LoadThread ----

func TestLoadThread_SingleRoot(t *testing.T) {
	id := mustGenerate(t, "alice")
	dir := t.TempDir()
	keysDir := filepath.Join(dir, "keys")
	threadDir := filepath.Join(dir, "threads", "my-thread")

	writeKey(t, keysDir, "alice", id.PublicKey)
	signedPost(t, threadDir, forum.RootFilename, id, "", "Root post body")

	thread, err := forum.LoadThread("general", "my-thread", threadDir, keysDir)
	if err != nil {
		t.Fatalf("LoadThread: %v", err)
	}
	if len(thread.Posts) != 1 {
		t.Fatalf("Posts: got %d, want 1", len(thread.Posts))
	}
	if thread.Root == nil {
		t.Fatal("Root is nil")
	}
	if thread.Root.SigStatus != forum.SigValid {
		t.Errorf("root SigStatus: got %v – %s", thread.Root.SigStatus, thread.Root.SigError)
	}
	if thread.Root.Body != "Root post body" {
		t.Errorf("root body: got %q", thread.Root.Body)
	}
}

func TestLoadThread_RepliesOrderedByTimestamp(t *testing.T) {
	id := mustGenerate(t, "alice")
	dir := t.TempDir()
	keysDir := filepath.Join(dir, "keys")
	threadDir := filepath.Join(dir, "thread")

	writeKey(t, keysDir, "alice", id.PublicKey)

	rootContent := signedPost(t, threadDir, forum.RootFilename, id, "", "Root")
	rootHash := forum.PostHash(rootContent)

	// Write replies with explicit timestamps via unique bodies so filenames differ.
	bodies := []string{"Reply A", "Reply B", "Reply C"}
	for _, body := range bodies {
		// Ensure distinct unix millis between posts.
		time.Sleep(2 * time.Millisecond)
		filename := forum.NewPostFilename(body)
		signedPost(t, threadDir, filename, id, rootHash, body)
	}

	thread, err := forum.LoadThread("cat", "slug", threadDir, keysDir)
	if err != nil {
		t.Fatal(err)
	}
	if len(thread.Posts) != 4 {
		t.Fatalf("Posts: got %d, want 4", len(thread.Posts))
	}
	if thread.Posts[0].Filename != forum.RootFilename {
		t.Errorf("first post should be root, got %q", thread.Posts[0].Filename)
	}
	for i := 1; i < len(thread.Posts)-1; i++ {
		if !thread.Posts[i].Timestamp.Before(thread.Posts[i+1].Timestamp) &&
			!thread.Posts[i].Timestamp.Equal(thread.Posts[i+1].Timestamp) {
			t.Errorf("posts[%d] timestamp not ≤ posts[%d]", i, i+1)
		}
	}
	// Verify bodies in order.
	for i, want := range bodies {
		got := thread.Posts[i+1].Body
		if got != want {
			t.Errorf("post[%d] body: got %q, want %q", i+1, got, want)
		}
	}
}

func TestLoadThread_InvalidSigVisible(t *testing.T) {
	id := mustGenerate(t, "alice")
	other := mustGenerate(t, "alice") // different key, same username
	dir := t.TempDir()
	keysDir := filepath.Join(dir, "keys")
	threadDir := filepath.Join(dir, "thread")

	// Store the "other" key – so alice's actual signature won't verify.
	writeKey(t, keysDir, "alice", other.PublicKey)
	signedPost(t, threadDir, forum.RootFilename, id, "", "Body")

	thread, err := forum.LoadThread("cat", "s", threadDir, keysDir)
	if err != nil {
		t.Fatal(err)
	}
	if len(thread.Posts) != 1 {
		t.Fatal("expected 1 post")
	}
	if thread.Posts[0].SigStatus == forum.SigValid {
		t.Error("expected invalid sig, got SigValid")
	}
}

func TestLoadThread_SkipsNonMdFiles(t *testing.T) {
	id := mustGenerate(t, "alice")
	dir := t.TempDir()
	keysDir := filepath.Join(dir, "keys")
	threadDir := filepath.Join(dir, "thread")

	writeKey(t, keysDir, "alice", id.PublicKey)
	signedPost(t, threadDir, forum.RootFilename, id, "", "Root")
	// Write a non-.md file and a subdirectory.
	_ = os.WriteFile(filepath.Join(threadDir, "README.txt"), []byte("ignore me"), 0o644)
	_ = os.MkdirAll(filepath.Join(threadDir, "subdir"), 0o755)

	thread, err := forum.LoadThread("cat", "s", threadDir, keysDir)
	if err != nil {
		t.Fatal(err)
	}
	if len(thread.Posts) != 1 {
		t.Errorf("Posts: got %d, want 1 (non-.md files/dirs should be skipped)", len(thread.Posts))
	}
}

func TestLoadThread_MissingRoot(t *testing.T) {
	id := mustGenerate(t, "alice")
	dir := t.TempDir()
	keysDir := filepath.Join(dir, "keys")
	threadDir := filepath.Join(dir, "thread")

	writeKey(t, keysDir, "alice", id.PublicKey)
	// Only a reply, no root post.
	filename := forum.NewPostFilename("reply body")
	signedPost(t, threadDir, filename, id, "somehash", "reply body")

	thread, err := forum.LoadThread("cat", "s", threadDir, keysDir)
	if err != nil {
		t.Fatal(err)
	}
	if thread.Root != nil {
		t.Error("Root should be nil when 0000_root.md is absent")
	}
	if len(thread.Posts) != 1 {
		t.Errorf("Posts: got %d, want 1", len(thread.Posts))
	}
}

// ---- LoadCategory ----

func setupCategory(t *testing.T, dir, slug, name string, threadSlugs []string) {
	t.Helper()
	catDir := filepath.Join(dir, slug)
	if err := os.MkdirAll(catDir, 0o755); err != nil {
		t.Fatal(err)
	}
	meta := fmt.Sprintf("name = %q\ndescription = \"Test category\"\n", name)
	if err := os.WriteFile(filepath.Join(catDir, "META.toml"), []byte(meta), 0o644); err != nil {
		t.Fatal(err)
	}
	for _, ts := range threadSlugs {
		threadDir := filepath.Join(catDir, ts)
		if err := os.MkdirAll(threadDir, 0o755); err != nil {
			t.Fatal(err)
		}
		// Create a minimal root file so LoadCategory recognises the thread.
		rootContent := "+++\nauthor=\"a\"\npubkey=\"b\"\ntimestamp=\"2026-01-01T00:00:00Z\"\nparent=\"\"\nsignature=\"s\"\n+++\n\nbody"
		if err := os.WriteFile(filepath.Join(threadDir, forum.RootFilename), []byte(rootContent), 0o644); err != nil {
			t.Fatal(err)
		}
	}
}

func TestLoadCategory_Basic(t *testing.T) {
	dir := t.TempDir()
	setupCategory(t, dir, "general", "General", []string{"thread-a", "thread-b", "thread-c"})

	cat, err := forum.LoadCategory("general", filepath.Join(dir, "general"))
	if err != nil {
		t.Fatalf("LoadCategory: %v", err)
	}
	if cat.Slug != "general" {
		t.Errorf("Slug: got %q", cat.Slug)
	}
	if cat.Name != "General" {
		t.Errorf("Name: got %q", cat.Name)
	}
	if len(cat.ThreadSlugs) != 3 {
		t.Fatalf("ThreadSlugs: got %d, want 3", len(cat.ThreadSlugs))
	}
	// Must be sorted.
	for i := 0; i < len(cat.ThreadSlugs)-1; i++ {
		if cat.ThreadSlugs[i] > cat.ThreadSlugs[i+1] {
			t.Errorf("ThreadSlugs not sorted: %v", cat.ThreadSlugs)
		}
	}
}

func TestLoadCategory_SkipsDirsWithoutRoot(t *testing.T) {
	dir := t.TempDir()
	catDir := filepath.Join(dir, "meta")
	_ = os.MkdirAll(catDir, 0o755)
	_ = os.WriteFile(filepath.Join(catDir, "META.toml"), []byte("name=\"Meta\"\ndescription=\"\"\n"), 0o644)
	// Directory without 0000_root.md – should be ignored.
	_ = os.MkdirAll(filepath.Join(catDir, "no-root-thread"), 0o755)
	// Directory with root – should be listed.
	validThread := filepath.Join(catDir, "valid-thread")
	_ = os.MkdirAll(validThread, 0o755)
	rootContent := "+++\nauthor=\"a\"\npubkey=\"b\"\ntimestamp=\"2026-01-01T00:00:00Z\"\nparent=\"\"\nsignature=\"s\"\n+++\n\nbody"
	_ = os.WriteFile(filepath.Join(validThread, forum.RootFilename), []byte(rootContent), 0o644)

	cat, err := forum.LoadCategory("meta", catDir)
	if err != nil {
		t.Fatal(err)
	}
	if len(cat.ThreadSlugs) != 1 || cat.ThreadSlugs[0] != "valid-thread" {
		t.Errorf("ThreadSlugs: got %v", cat.ThreadSlugs)
	}
}

func TestLoadCategory_MissingMeta(t *testing.T) {
	dir := t.TempDir()
	_, err := forum.LoadCategory("nope", filepath.Join(dir, "nope"))
	if err == nil {
		t.Error("expected error for missing META.toml")
	}
}

func TestLoadCategory_EmptyCategory(t *testing.T) {
	dir := t.TempDir()
	catDir := filepath.Join(dir, "empty")
	_ = os.MkdirAll(catDir, 0o755)
	_ = os.WriteFile(filepath.Join(catDir, "META.toml"), []byte("name=\"Empty\"\ndescription=\"\"\n"), 0o644)

	cat, err := forum.LoadCategory("empty", catDir)
	if err != nil {
		t.Fatal(err)
	}
	if len(cat.ThreadSlugs) != 0 {
		t.Errorf("expected 0 threads, got %v", cat.ThreadSlugs)
	}
}
