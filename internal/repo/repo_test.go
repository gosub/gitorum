package repo_test

import (
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"testing/fstest"

	gogit "github.com/go-git/go-git/v5"
	"github.com/go-git/go-git/v5/plumbing/object"

	"github.com/gosub/gitorum/internal/crypto"
	"github.com/gosub/gitorum/internal/repo"
)

// newIdentity is a test helper that fails the test on error.
func newIdentity(t *testing.T, username string) *crypto.Identity {
	t.Helper()
	id, err := crypto.Generate(username)
	if err != nil {
		t.Fatalf("crypto.Generate(%q): %v", username, err)
	}
	return id
}

// newBareRemote creates a bare git repository and returns its path.
func newBareRemote(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()
	if _, err := gogit.PlainInit(dir, true); err != nil {
		t.Fatalf("PlainInit bare: %v", err)
	}
	return dir
}

func newForum(t *testing.T, username string) (*repo.Repo, *crypto.Identity) {
	t.Helper()
	id := newIdentity(t, username)
	r, err := repo.Init(t.TempDir(), repo.ForumMeta{Name: "Forum", AdminPubkey: id.PublicKey}, id)
	if err != nil {
		t.Fatalf("Init: %v", err)
	}
	return r, id
}

func headCommit(t *testing.T, path string) *object.Commit {
	t.Helper()
	gr, err := gogit.PlainOpen(path)
	if err != nil {
		t.Fatal(err)
	}
	ref, err := gr.Head()
	if err != nil {
		t.Fatalf("Head: %v", err)
	}
	c, err := gr.CommitObject(ref.Hash())
	if err != nil {
		t.Fatalf("CommitObject: %v", err)
	}
	return c
}

func countCommits(t *testing.T, path string) int {
	t.Helper()
	gr, err := gogit.PlainOpen(path)
	if err != nil {
		t.Fatal(err)
	}
	head, _ := gr.Head()
	iter, _ := gr.Log(&gogit.LogOptions{From: head.Hash()})
	n := 0
	_ = iter.ForEach(func(*object.Commit) error { n++; return nil })
	return n
}

// ---- Init / Open ----

func TestInit_InitialCommit(t *testing.T) {
	r, id := newForum(t, "carol")

	commit := headCommit(t, r.Path)
	if !strings.HasPrefix(commit.Message, "init:") {
		t.Errorf("commit message: got %q, want prefix 'init:'", commit.Message)
	}
	if commit.Author.Name != "carol" {
		t.Errorf("author: got %q, want carol", commit.Author.Name)
	}
	if len(commit.ParentHashes) != 0 {
		t.Errorf("expected 0 parents, got %d", len(commit.ParentHashes))
	}

	snap, err := r.Snapshot()
	if err != nil {
		t.Fatal(err)
	}
	key, err := fs.ReadFile(snap.FS, "keys/carol.pub")
	if err != nil {
		t.Fatalf("keys/carol.pub not in initial commit: %v", err)
	}
	if strings.TrimSpace(string(key)) != id.PublicKey {
		t.Error("keys/carol.pub does not contain the public key")
	}
	meta, err := fs.ReadFile(snap.FS, repo.MetaFile)
	if err != nil {
		t.Fatalf("%s not in initial commit: %v", repo.MetaFile, err)
	}
	if !strings.Contains(string(meta), id.PublicKey) {
		t.Errorf("%s does not contain the admin key", repo.MetaFile)
	}
}

func TestOpen(t *testing.T) {
	r, _ := newForum(t, "eve")
	r2, err := repo.Open(r.Path)
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	if r2.Head() != r.Head() {
		t.Error("reopened repo has a different HEAD")
	}
	if _, err := repo.Open("/nonexistent/path/gitorum"); err == nil {
		t.Error("expected error opening non-existent repo")
	}
}

// ---- Snapshot / TreeFS ----

func TestSnapshot_ReadsHeadNotWorktree(t *testing.T) {
	r, id := newForum(t, "alice")
	if err := r.Commit(id, "add", map[string][]byte{
		"general/META.toml":           []byte("name = \"General\"\n"),
		"general/hello/0000_root.md":  []byte("root"),
		"general/hello/1_deadbeef.md": []byte("reply"),
	}, nil); err != nil {
		t.Fatal(err)
	}
	// Untracked and modified working-tree files must not be visible.
	_ = os.WriteFile(filepath.Join(r.Path, "general", "hello", "2_cafebabe.md"), []byte("untracked"), 0o644)
	_ = os.WriteFile(filepath.Join(r.Path, "general", "hello", "0000_root.md"), []byte("tampered"), 0o644)

	snap, err := r.Snapshot()
	if err != nil {
		t.Fatal(err)
	}
	if err := fstest.TestFS(snap.FS,
		"GITORUM.toml", "keys/alice.pub", "general/META.toml",
		"general/hello/0000_root.md", "general/hello/1_deadbeef.md"); err != nil {
		t.Fatal(err)
	}
	got, _ := fs.ReadFile(snap.FS, "general/hello/0000_root.md")
	if string(got) != "root" {
		t.Errorf("root: got %q, want committed content", got)
	}
	if _, err := fs.Stat(snap.FS, "general/hello/2_cafebabe.md"); err == nil {
		t.Error("untracked file visible in snapshot")
	}
	if _, err := fs.ReadFile(snap.FS, "../GITORUM.toml"); err == nil {
		t.Error("invalid path accepted")
	}
}

// ---- Commit ----

func TestCommit_WriteAndRemove(t *testing.T) {
	r, id := newForum(t, "alice")
	if err := r.Commit(id, "add", map[string][]byte{"requests/bob.pub": []byte("K\n")}, nil); err != nil {
		t.Fatal(err)
	}
	if err := r.Commit(id, "move", map[string][]byte{"keys/bob.pub": []byte("K\n")},
		[]string{"requests/bob.pub"}); err != nil {
		t.Fatal(err)
	}
	snap, _ := r.Snapshot()
	if _, err := fs.Stat(snap.FS, "requests/bob.pub"); err == nil {
		t.Error("requests/bob.pub still present")
	}
	if _, err := fs.Stat(snap.FS, "keys/bob.pub"); err != nil {
		t.Error("keys/bob.pub missing")
	}
	if c := headCommit(t, r.Path); c.Message != "move" || c.Author.Name != "alice" {
		t.Errorf("commit: %q by %q", c.Message, c.Author.Name)
	}
	if n := countCommits(t, r.Path); n != 3 {
		t.Errorf("commits: got %d, want 3", n)
	}
}

// ---- AddRemote ----

func TestAddRemote_Replaces(t *testing.T) {
	r, _ := newForum(t, "frank")
	remote1, remote2 := newBareRemote(t), newBareRemote(t)
	if err := r.AddRemote("origin", remote1); err != nil {
		t.Fatalf("AddRemote: %v", err)
	}
	if err := r.AddRemote("origin", remote2); err != nil {
		t.Fatalf("AddRemote (replace): %v", err)
	}
	if got := r.RemoteURL(); got != remote2 {
		t.Errorf("RemoteURL: got %q, want %q", got, remote2)
	}
}

// ---- Pull / Push ----

// TestRepo_PullPush verifies the basic sync cycle: push initial state, have
// another user push a new commit, then pull and verify the new file appears.
func TestRepo_PullPush(t *testing.T) {
	r, _ := newForum(t, "alice")
	bare := newBareRemote(t)
	if err := r.AddRemote("origin", bare); err != nil {
		t.Fatalf("AddRemote: %v", err)
	}
	if err := r.Push(); err != nil {
		t.Fatalf("initial Push: %v", err)
	}

	// Bob clones the bare remote, adds a file, and pushes back.
	bobDir := t.TempDir()
	if _, err := gogit.PlainClone(bobDir, false, &gogit.CloneOptions{URL: bare}); err != nil {
		t.Fatalf("PlainClone: %v", err)
	}
	bob, err := repo.Open(bobDir)
	if err != nil {
		t.Fatal(err)
	}
	if err := bob.Commit(newIdentity(t, "bob"), "post", map[string][]byte{
		"general/bobs-post/0000_root.md": []byte("hello from bob"),
	}, nil); err != nil {
		t.Fatal(err)
	}
	if err := bob.Push(); err != nil {
		t.Fatalf("bob push: %v", err)
	}

	if err := r.Pull(); err != nil {
		t.Fatalf("Pull: %v", err)
	}
	snap, _ := r.Snapshot()
	if _, err := fs.Stat(snap.FS, "general/bobs-post/0000_root.md"); err != nil {
		t.Errorf("pulled file missing: %v", err)
	}
	if err := r.Pull(); err != nil {
		t.Errorf("second Pull (already up to date): %v", err)
	}
}

// ---- concurrency ----

func TestConcurrentCommits(t *testing.T) {
	r, id := newForum(t, "alice")

	const n = 20
	errs := make(chan error, n)
	for i := 0; i < n; i++ {
		go func(i int) {
			rel := fmt.Sprintf("general/t/%d_%08x.md", 1700000000000+i, i)
			errs <- r.Commit(id, "post", map[string][]byte{rel: []byte(fmt.Sprintf("post %d\n", i))}, nil)
		}(i)
	}
	// Readers run concurrently with the writers.
	for i := 0; i < n; i++ {
		go func() {
			if snap, err := r.Snapshot(); err == nil {
				_, _ = fs.ReadDir(snap.FS, ".")
			}
		}()
	}
	for i := 0; i < n; i++ {
		if err := <-errs; err != nil {
			t.Errorf("Commit: %v", err)
		}
	}
	if got := countCommits(t, r.Path); got != n+1 {
		t.Errorf("commits: got %d, want %d", got, n+1)
	}
}
