package repo_test

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	gogit "github.com/go-git/go-git/v5"
	"github.com/go-git/go-git/v5/plumbing/object"

	"github.com/gosub/gitorum/internal/crypto"
	"github.com/gosub/gitorum/internal/repo"
)

// newIdentity is a test helper that panics on failure.
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

// ---- Init ----

func TestInit_CreatesFiles(t *testing.T) {
	id := newIdentity(t, "alice")
	dir := t.TempDir()

	meta := repo.ForumMeta{
		Name:        "Test Forum",
		Description: "A forum for tests",
		AdminPubkey: id.PublicKey,
	}
	r, err := repo.Init(dir, meta, id)
	if err != nil {
		t.Fatalf("Init: %v", err)
	}
	if r.Path == "" {
		t.Fatal("Repo.Path is empty")
	}

	// GITORUM.toml must exist
	checkFile(t, filepath.Join(dir, "GITORUM.toml"))

	// keys/alice.pub must exist
	pubPath := filepath.Join(dir, "keys", "alice.pub")
	checkFile(t, pubPath)

	// keys/alice.pub must contain the public key
	data, err := os.ReadFile(pubPath)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(data), id.PublicKey) {
		t.Errorf("keys/alice.pub does not contain the public key")
	}
}

func TestInit_ReadMeta(t *testing.T) {
	id := newIdentity(t, "bob")
	dir := t.TempDir()

	want := repo.ForumMeta{
		Name:        "Bob's Forum",
		Description: "Bob speaks",
		AdminPubkey: id.PublicKey,
	}
	r, err := repo.Init(dir, want, id)
	if err != nil {
		t.Fatalf("Init: %v", err)
	}

	got, err := r.ReadMeta()
	if err != nil {
		t.Fatalf("ReadMeta: %v", err)
	}
	if got.Name != want.Name {
		t.Errorf("Name: got %q, want %q", got.Name, want.Name)
	}
	if got.Description != want.Description {
		t.Errorf("Description: got %q, want %q", got.Description, want.Description)
	}
	if got.AdminPubkey != want.AdminPubkey {
		t.Errorf("AdminPubkey mismatch")
	}
}

func TestInit_InitialCommit(t *testing.T) {
	id := newIdentity(t, "carol")
	dir := t.TempDir()

	r, err := repo.Init(dir, repo.ForumMeta{Name: "Forum", AdminPubkey: id.PublicKey}, id)
	if err != nil {
		t.Fatalf("Init: %v", err)
	}

	gr := r.Git()
	ref, err := gr.Head()
	if err != nil {
		t.Fatalf("Head: %v", err)
	}
	commit, err := gr.CommitObject(ref.Hash())
	if err != nil {
		t.Fatalf("CommitObject: %v", err)
	}

	if !strings.Contains(commit.Message, "init:") {
		t.Errorf("commit message: got %q, want prefix 'init:'", commit.Message)
	}
	if commit.Author.Name != "carol" {
		t.Errorf("author: got %q, want %q", commit.Author.Name, "carol")
	}

	// Must be the only commit (no parent)
	if len(commit.ParentHashes) != 0 {
		t.Errorf("expected 0 parents, got %d", len(commit.ParentHashes))
	}
}

func TestInit_InitialCommitContainsExpectedFiles(t *testing.T) {
	id := newIdentity(t, "diana")
	dir := t.TempDir()

	r, err := repo.Init(dir, repo.ForumMeta{Name: "F", AdminPubkey: id.PublicKey}, id)
	if err != nil {
		t.Fatal(err)
	}

	gr := r.Git()
	ref, _ := gr.Head()
	commit, _ := gr.CommitObject(ref.Hash())
	tree, err := commit.Tree()
	if err != nil {
		t.Fatal(err)
	}

	wantFiles := []string{"GITORUM.toml", "keys/diana.pub"}
	for _, name := range wantFiles {
		if _, err := tree.File(name); err != nil {
			t.Errorf("file %q not in initial commit: %v", name, err)
		}
	}
}

// ---- Open ----

func TestOpen_ExistingRepo(t *testing.T) {
	id := newIdentity(t, "eve")
	dir := t.TempDir()

	_, err := repo.Init(dir, repo.ForumMeta{Name: "Eve's", AdminPubkey: id.PublicKey}, id)
	if err != nil {
		t.Fatal(err)
	}

	r2, err := repo.Open(dir)
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	meta, err := r2.ReadMeta()
	if err != nil {
		t.Fatal(err)
	}
	if meta.Name != "Eve's" {
		t.Errorf("Name: got %q", meta.Name)
	}
}

func TestOpen_NonExistentRepo(t *testing.T) {
	_, err := repo.Open("/nonexistent/path/gitorum")
	if err == nil {
		t.Error("expected error opening non-existent repo")
	}
}

// ---- AddRemote ----

func TestAddRemote(t *testing.T) {
	id := newIdentity(t, "frank")
	dir := t.TempDir()
	remote := newBareRemote(t)

	r, err := repo.Init(dir, repo.ForumMeta{Name: "F", AdminPubkey: id.PublicKey}, id)
	if err != nil {
		t.Fatal(err)
	}
	if err := r.AddRemote("origin", remote); err != nil {
		t.Fatalf("AddRemote: %v", err)
	}

	remotes, err := r.Git().Remotes()
	if err != nil {
		t.Fatal(err)
	}
	if len(remotes) != 1 {
		t.Fatalf("expected 1 remote, got %d", len(remotes))
	}
	if remotes[0].Config().Name != "origin" {
		t.Errorf("remote name: got %q, want %q", remotes[0].Config().Name, "origin")
	}
	if remotes[0].Config().URLs[0] != remote {
		t.Errorf("remote URL: got %q, want %q", remotes[0].Config().URLs[0], remote)
	}
}

func TestAddRemote_Idempotent(t *testing.T) {
	id := newIdentity(t, "grace")
	dir := t.TempDir()
	remote := newBareRemote(t)

	r, _ := repo.Init(dir, repo.ForumMeta{Name: "F", AdminPubkey: id.PublicKey}, id)
	_ = r.AddRemote("origin", remote)

	remote2 := newBareRemote(t)
	if err := r.AddRemote("origin", remote2); err != nil {
		t.Fatalf("AddRemote (replace): %v", err)
	}

	remotes, _ := r.Git().Remotes()
	if len(remotes) != 1 {
		t.Fatalf("expected 1 remote after replace, got %d", len(remotes))
	}
	if remotes[0].Config().URLs[0] != remote2 {
		t.Error("remote URL not updated")
	}
}

// ---- WritePublicKey ----

func TestWritePublicKey(t *testing.T) {
	adminID := newIdentity(t, "admin")
	userID := newIdentity(t, "heidi")
	dir := t.TempDir()

	r, err := repo.Init(dir, repo.ForumMeta{Name: "F", AdminPubkey: adminID.PublicKey}, adminID)
	if err != nil {
		t.Fatal(err)
	}
	if err := r.WritePublicKey(adminID, userID.Username, userID.PublicKey); err != nil {
		t.Fatalf("WritePublicKey: %v", err)
	}

	pubPath := filepath.Join(dir, "keys", "heidi.pub")
	data, err := os.ReadFile(pubPath)
	if err != nil {
		t.Fatalf("read heidi.pub: %v", err)
	}
	if !strings.Contains(string(data), userID.PublicKey) {
		t.Error("heidi.pub does not contain user's public key")
	}

	// Must have a second commit for the key addition
	gr := r.Git()
	log, err := gr.Log(&gogit.LogOptions{})
	if err != nil {
		t.Fatal(err)
	}
	var commits []*object.Commit
	_ = log.ForEach(func(c *object.Commit) error {
		commits = append(commits, c)
		return nil
	})
	if len(commits) != 2 {
		t.Errorf("expected 2 commits, got %d", len(commits))
	}
	if !strings.Contains(commits[0].Message, "heidi") {
		t.Errorf("key commit message: got %q", commits[0].Message)
	}
}

// ---- helper ----

func checkFile(t *testing.T, path string) {
	t.Helper()
	if _, err := os.Stat(path); err != nil {
		t.Errorf("expected file %s to exist: %v", path, err)
	}
}
