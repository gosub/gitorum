package forum_test

import (
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"
	"time"

	"github.com/gosub/gitorum/internal/crypto"
	"github.com/gosub/gitorum/internal/forum"
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

// keyRing loads the key ring from root/keys for fixtures where the (single)
// author is also the admin: the first key file's first key is pinned.
func keyRing(t *testing.T, root string) *forum.KeyRing {
	t.Helper()
	admin := ""
	if entries, err := os.ReadDir(filepath.Join(root, "keys")); err == nil {
		for _, e := range entries {
			if strings.HasSuffix(e.Name(), ".pub") {
				data, _ := os.ReadFile(filepath.Join(root, "keys", e.Name()))
				admin = strings.TrimSpace(strings.SplitN(string(data), "\n", 2)[0])
				break
			}
		}
	}
	return keyRingAdmin(t, root, admin)
}

func keyRingAdmin(t *testing.T, root, adminKey string) *forum.KeyRing {
	t.Helper()
	ring, err := forum.LoadKeyRing(os.DirFS(root), adminKey)
	if err != nil {
		t.Fatalf("LoadKeyRing: %v", err)
	}
	return ring
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
			name:    "invalid TOML",
			content: "+++\nauthor = [unclosed\n+++\n\nbody",
			wantErr: "TOML",
		},
		{
			name:    "bad timestamp",
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
	keysDir := filepath.Join(t.TempDir(), "keys")
	writeKey(t, keysDir, "alice", id.PublicKey)

	post, err := forum.SignPost(id, "", "Hello")
	if err != nil {
		t.Fatal(err)
	}
	post.Verify(keyRing(t, filepath.Dir(keysDir)))
	if post.SigStatus != forum.SigValid {
		t.Errorf("SigStatus: got %v, want SigValid; error: %s", post.SigStatus, post.SigError)
	}
}

func TestVerifySignature_AfterRoundTrip(t *testing.T) {
	id := mustGenerate(t, "bob")
	keysDir := filepath.Join(t.TempDir(), "keys")
	writeKey(t, keysDir, "bob", id.PublicKey)

	post, _ := forum.SignPost(id, "", "Post content")
	post.Filename = forum.RootFilename
	content := post.Format()

	parsed, err := forum.ParsePost(forum.RootFilename, content)
	if err != nil {
		t.Fatal(err)
	}
	parsed.Verify(keyRing(t, filepath.Dir(keysDir)))
	if parsed.SigStatus != forum.SigValid {
		t.Errorf("SigStatus after round-trip: %v – %s", parsed.SigStatus, parsed.SigError)
	}
}

func TestVerifySignature_MissingKey(t *testing.T) {
	id := mustGenerate(t, "charlie")
	keysDir := filepath.Join(t.TempDir(), "keys") // empty – no key file

	post, _ := forum.SignPost(id, "", "Body")
	post.Verify(keyRing(t, filepath.Dir(keysDir)))

	if post.SigStatus != forum.SigMissing {
		t.Errorf("SigStatus: got %v, want SigMissing", post.SigStatus)
	}
}

func TestVerifySignature_TamperedBody(t *testing.T) {
	id := mustGenerate(t, "dave")
	keysDir := filepath.Join(t.TempDir(), "keys")
	writeKey(t, keysDir, "dave", id.PublicKey)

	post, _ := forum.SignPost(id, "", "Original body")
	post.Body = "Tampered body" // change body after signing
	post.Verify(keyRing(t, filepath.Dir(keysDir)))

	if post.SigStatus != forum.SigInvalid {
		t.Errorf("SigStatus: got %v, want SigInvalid", post.SigStatus)
	}
}

func TestVerifySignature_WrongKey(t *testing.T) {
	id := mustGenerate(t, "eve")
	otherID := mustGenerate(t, "eve") // different keypair, same username
	keysDir := filepath.Join(t.TempDir(), "keys")
	writeKey(t, keysDir, "eve", otherID.PublicKey) // store different key

	post, _ := forum.SignPost(id, "", "Body")
	post.Verify(keyRing(t, filepath.Dir(keysDir)))

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
		name := forum.NewPostFilename([]byte("some body text"))
		if !re.MatchString(name) {
			t.Errorf("NewPostFilename %q does not match pattern", name)
		}
	}
}

func TestNewPostFilename_DifferentBodies(t *testing.T) {
	n1 := forum.NewPostFilename([]byte("body one"))
	n2 := forum.NewPostFilename([]byte("body two"))
	// Hash part (chars 14-21) should differ.
	if len(n1) > 14 && len(n2) > 14 && n1[14:] == n2[14:] {
		t.Error("different bodies produced identical filename hash parts")
	}
}

func TestNewPostFilename_SameBodyDifferentTime(t *testing.T) {
	// Two calls with the same body should produce the same hash part but may
	// differ in the millis prefix.
	body := "identical"
	n1 := forum.NewPostFilename([]byte(body))
	time.Sleep(2 * time.Millisecond)
	n2 := forum.NewPostFilename([]byte(body))
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
	threadDir := filepath.Join(dir, "cat", "slug")

	writeKey(t, keysDir, "alice", id.PublicKey)
	signedPost(t, threadDir, forum.RootFilename, id, "", "Root post body")

	thread, err := forum.LoadThread(os.DirFS(dir), "cat", "slug", keyRing(t, dir))
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
	threadDir := filepath.Join(dir, "cat", "slug")

	writeKey(t, keysDir, "alice", id.PublicKey)

	rootContent := signedPost(t, threadDir, forum.RootFilename, id, "", "Root")
	rootHash := forum.PostHash(rootContent)

	// Write replies with explicit timestamps via unique bodies so filenames differ.
	bodies := []string{"Reply A", "Reply B", "Reply C"}
	for _, body := range bodies {
		// Ensure distinct unix millis between posts.
		time.Sleep(2 * time.Millisecond)
		filename := forum.NewPostFilename([]byte(body))
		signedPost(t, threadDir, filename, id, rootHash, body)
	}

	thread, err := forum.LoadThread(os.DirFS(dir), "cat", "slug", keyRing(t, dir))
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
	threadDir := filepath.Join(dir, "cat", "slug")

	// Store the "other" key – so alice's actual signature won't verify.
	writeKey(t, keysDir, "alice", other.PublicKey)
	signedPost(t, threadDir, forum.RootFilename, id, "", "Body")

	thread, err := forum.LoadThread(os.DirFS(dir), "cat", "slug", keyRing(t, dir))
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
	threadDir := filepath.Join(dir, "cat", "slug")

	writeKey(t, keysDir, "alice", id.PublicKey)
	signedPost(t, threadDir, forum.RootFilename, id, "", "Root")
	// Write a non-.md file and a subdirectory.
	_ = os.WriteFile(filepath.Join(threadDir, "README.txt"), []byte("ignore me"), 0o644)
	_ = os.MkdirAll(filepath.Join(threadDir, "subdir"), 0o755)

	thread, err := forum.LoadThread(os.DirFS(dir), "cat", "slug", keyRing(t, dir))
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
	threadDir := filepath.Join(dir, "cat", "slug")

	writeKey(t, keysDir, "alice", id.PublicKey)
	// Only a reply, no root post.
	filename := forum.NewPostFilename([]byte("reply body"))
	signedPost(t, threadDir, filename, id, "somehash", "reply body")

	thread, err := forum.LoadThread(os.DirFS(dir), "cat", "slug", keyRing(t, dir))
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

	cat, err := forum.LoadCategory(os.DirFS(dir), "general")
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

	cat, err := forum.LoadCategory(os.DirFS(dir), "meta")
	if err != nil {
		t.Fatal(err)
	}
	if len(cat.ThreadSlugs) != 1 || cat.ThreadSlugs[0] != "valid-thread" {
		t.Errorf("ThreadSlugs: got %v", cat.ThreadSlugs)
	}
}

func TestLoadCategory_MissingMeta(t *testing.T) {
	dir := t.TempDir()
	_, err := forum.LoadCategory(os.DirFS(dir), "nope")
	if err == nil {
		t.Error("expected error for missing META.toml")
	}
}

func TestLoadCategory_EmptyCategory(t *testing.T) {
	dir := t.TempDir()
	catDir := filepath.Join(dir, "empty")
	_ = os.MkdirAll(catDir, 0o755)
	_ = os.WriteFile(filepath.Join(catDir, "META.toml"), []byte("name=\"Empty\"\ndescription=\"\"\n"), 0o644)

	cat, err := forum.LoadCategory(os.DirFS(dir), "empty")
	if err != nil {
		t.Fatal(err)
	}
	if len(cat.ThreadSlugs) != 0 {
		t.Errorf("expected 0 threads, got %v", cat.ThreadSlugs)
	}
}

// ---- Tombstone ----

func TestTombstoneFilename(t *testing.T) {
	cases := []struct{ in, want string }{
		{"0000_root.md", "0000_root.md.tomb"},
		{"1708123456789_a3f9c1b2.md", "1708123456789_a3f9c1b2.md.tomb"},
	}
	for _, tc := range cases {
		got := forum.TombstoneFilename(tc.in)
		if got != tc.want {
			t.Errorf("TombstoneFilename(%q) = %q, want %q", tc.in, got, tc.want)
		}
	}
}

func TestSignTombstone_Valid(t *testing.T) {
	id := mustGenerate(t, "admin")
	original := []byte("some original post content")

	tomb, err := forum.SignTombstone(id, original)
	if err != nil {
		t.Fatalf("SignTombstone: %v", err)
	}
	if tomb.Author != "admin" {
		t.Errorf("Author: got %q", tomb.Author)
	}
	if tomb.Body != "" {
		t.Errorf("Body: expected empty, got %q", tomb.Body)
	}
	if tomb.Parent != forum.PostHash(original) {
		t.Errorf("Parent: got %q, want hash of original", tomb.Parent)
	}
}

func TestLoadThread_TombstonedAsPlaceholder(t *testing.T) {
	id := mustGenerate(t, "alice")
	dir := t.TempDir()
	keysDir := filepath.Join(dir, "keys")
	threadDir := filepath.Join(dir, "cat", "slug")

	writeKey(t, keysDir, "alice", id.PublicKey)
	rootContent := signedPost(t, threadDir, forum.RootFilename, id, "", "Root post")

	// Add two replies, using a consistent filename for reply2 to avoid a
	// timestamp race between the write and the tombstone creation.
	time.Sleep(2 * time.Millisecond)
	reply1Name := forum.NewPostFilename([]byte("Reply one"))
	signedPost(t, threadDir, reply1Name, id, forum.PostHash(rootContent), "Reply one")

	time.Sleep(2 * time.Millisecond)
	reply2Name := forum.NewPostFilename([]byte("Reply two"))
	reply2Content := signedPost(t, threadDir, reply2Name, id, forum.PostHash(rootContent), "Reply two")

	// Tombstone reply 2.
	tomb, _ := forum.SignTombstone(id, reply2Content)
	tomb.Filename = forum.TombstoneFilename(reply2Name)
	_ = os.WriteFile(filepath.Join(threadDir, tomb.Filename), tomb.Format(), 0o644)

	thread, err := forum.LoadThread(os.DirFS(dir), "cat", "slug", keyRing(t, dir))
	if err != nil {
		t.Fatalf("LoadThread: %v", err)
	}
	// Root + reply1 + [deleted] placeholder — tombstoned post stays in list.
	if len(thread.Posts) != 3 {
		t.Errorf("Posts: got %d, want 3 (root + reply1 + deleted placeholder)", len(thread.Posts))
	}
	tombstoned := 0
	for _, p := range thread.Posts {
		if p.Tombstoned {
			tombstoned++
			if p.Body != "" {
				t.Errorf("tombstoned post body should be empty, got %q", p.Body)
			}
		}
		if p.Body == "Reply two" {
			t.Error("tombstoned post body should not be visible")
		}
	}
	if tombstoned != 1 {
		t.Errorf("expected 1 tombstoned post, got %d", tombstoned)
	}
}

func TestScanThread_TombstonedReplyNotCounted(t *testing.T) {
	id := mustGenerate(t, "alice")
	dir := t.TempDir()
	keysDir := filepath.Join(dir, "keys")
	threadDir := filepath.Join(dir, "cat", "slug")

	writeKey(t, keysDir, "alice", id.PublicKey)
	rootContent := signedPost(t, threadDir, forum.RootFilename, id, "", "Root")

	time.Sleep(2 * time.Millisecond)
	replyName := forum.NewPostFilename([]byte("A reply"))
	replyContent := signedPost(t, threadDir, replyName, id, forum.PostHash(rootContent), "A reply")

	// Tombstone the reply.
	tomb, _ := forum.SignTombstone(id, replyContent)
	tomb.Filename = forum.TombstoneFilename(replyName)
	_ = os.WriteFile(filepath.Join(threadDir, tomb.Filename), tomb.Format(), 0o644)

	scan, err := forum.ScanThread(os.DirFS(dir), "cat", "slug", keyRing(t, dir))
	if err != nil {
		t.Fatalf("ScanThread: %v", err)
	}
	if scan.ReplyCount != 0 {
		t.Errorf("ReplyCount: got %d, want 0 (tombstoned reply excluded)", scan.ReplyCount)
	}
}

// ---- trust ----

// trustFixture writes keys for admin and bob into root/keys. Bob's key is
// certified by the admin when certify is set.
func trustFixture(t *testing.T, root string, admin, bob *crypto.Identity, certify bool) {
	t.Helper()
	keysDir := filepath.Join(root, "keys")
	writeKey(t, keysDir, admin.Username, admin.PublicKey)
	writeKey(t, keysDir, bob.Username, bob.PublicKey)
	if certify {
		data, _ := os.ReadFile(filepath.Join(keysDir, bob.Username+".pub"))
		sig, err := forum.SignKeyFile(admin, bob.Username, data)
		if err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(keysDir, bob.Username+".sig"), sig, 0o644); err != nil {
			t.Fatal(err)
		}
	}
}

func TestVerify_Certification(t *testing.T) {
	admin, bob := mustGenerate(t, "alice"), mustGenerate(t, "bob")
	tests := []struct {
		name    string
		certify bool
		tamper  bool // swap bob's key after certification
		author  *crypto.Identity
		want    forum.SigStatus
	}{
		{"admin's own key", false, false, admin, forum.SigValid},
		{"certified user", true, false, bob, forum.SigValid},
		{"uncertified user", false, false, bob, forum.SigUntrusted},
		{"key swapped after certification", true, true, bob, forum.SigUntrusted},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			root := t.TempDir()
			trustFixture(t, root, admin, bob, tt.certify)
			author := tt.author
			if tt.tamper {
				// Mallory replaces bob's key with her own and signs as bob.
				author = mustGenerate(t, "bob")
				writeKey(t, filepath.Join(root, "keys"), "bob", author.PublicKey)
			}
			post, _ := forum.SignPost(author, "", "hello")
			post.Verify(keyRingAdmin(t, root, admin.PublicKey))
			if post.SigStatus != tt.want {
				t.Errorf("SigStatus: got %v, want %v (%s)", post.SigStatus, tt.want, post.SigError)
			}
		})
	}
}

func TestVerify_RotatedKeys(t *testing.T) {
	admin := mustGenerate(t, "alice")
	oldKey, newKey := mustGenerate(t, "bob"), mustGenerate(t, "bob")
	root := t.TempDir()
	keysDir := filepath.Join(root, "keys")
	writeKey(t, keysDir, "alice", admin.PublicKey)
	content := forum.EncodeKeyFile([]string{oldKey.PublicKey, newKey.PublicKey})
	_ = os.WriteFile(filepath.Join(keysDir, "bob.pub"), content, 0o644)
	sig, _ := forum.SignKeyFile(admin, "bob", content)
	_ = os.WriteFile(filepath.Join(keysDir, "bob.sig"), sig, 0o644)

	ring := keyRingAdmin(t, root, admin.PublicKey)
	for _, id := range []*crypto.Identity{oldKey, newKey} {
		post, _ := forum.SignPost(id, "", "body")
		post.Verify(ring)
		if post.SigStatus != forum.SigValid {
			t.Errorf("key %s: %v (%s)", id.Fingerprint(), post.SigStatus, post.SigError)
		}
	}
}

func TestTombstone_OnlyAdminSignedAndBound(t *testing.T) {
	admin, bob := mustGenerate(t, "alice"), mustGenerate(t, "bob")
	root := t.TempDir()
	trustFixture(t, root, admin, bob, true)
	threadDir := filepath.Join(root, "cat", "slug")
	rootContent := signedPost(t, threadDir, forum.RootFilename, admin, "", "Root")
	hash := forum.PostHash(rootContent)
	r1 := signedPost(t, threadDir, "1700000000001_aaaaaaaa.md", bob, hash, "reply one")
	signedPost(t, threadDir, "1700000000002_bbbbbbbb.md", bob, hash, "reply two")
	r3 := signedPost(t, threadDir, "1700000000003_cccccccc.md", bob, hash, "reply three")

	write := func(name string, tomb *forum.Post) {
		_ = os.WriteFile(filepath.Join(threadDir, forum.TombstoneFilename(name)), tomb.Format(), 0o644)
	}
	// 1: forged by a non-admin user.
	forged, _ := forum.SignTombstone(bob, r1)
	write("1700000000001_aaaaaaaa.md", forged)
	// 2: a genuine admin tombstone for post 3, replayed onto post 2.
	genuine, _ := forum.SignTombstone(admin, r3)
	write("1700000000002_bbbbbbbb.md", genuine)
	// 3: genuine.
	write("1700000000003_cccccccc.md", genuine)
	// Root: empty tombstone file.
	_ = os.WriteFile(filepath.Join(threadDir, forum.TombstoneFilename(forum.RootFilename)), nil, 0o644)

	thread, err := forum.LoadThread(os.DirFS(root), "cat", "slug", keyRingAdmin(t, root, admin.PublicKey))
	if err != nil {
		t.Fatal(err)
	}
	hidden := map[string]bool{}
	for _, p := range thread.Posts {
		hidden[p.Filename] = p.Tombstoned
	}
	want := map[string]bool{
		forum.RootFilename:          false,
		"1700000000001_aaaaaaaa.md": false,
		"1700000000002_bbbbbbbb.md": false,
		"1700000000003_cccccccc.md": true,
	}
	for name, w := range want {
		if hidden[name] != w {
			t.Errorf("%s hidden=%v, want %v", name, hidden[name], w)
		}
	}
}
