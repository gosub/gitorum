// Package repo wraps go-git and provides all git-level operations for gitorum.
package repo

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"time"

	gogit "github.com/go-git/go-git/v5"
	"github.com/go-git/go-git/v5/config"
	"github.com/go-git/go-git/v5/plumbing"
	"github.com/go-git/go-git/v5/plumbing/object"

	"github.com/BurntSushi/toml"
	"github.com/gosub/gitorum/internal/crypto"
)

const gitTimeout = 30 * time.Second

// ForumMeta is the data stored in GITORUM.toml at the repository root.
type ForumMeta struct {
	Name        string `toml:"name"`
	Description string `toml:"description"`
	AdminPubkey string `toml:"admin_pubkey"`
}

// Repo wraps a go-git repository and exposes forum-level operations.
type Repo struct {
	// Path is the absolute path to the repository working tree.
	Path string
	git  *gogit.Repository
}

// Init creates a new forum repository at path.
//
// It:
//  1. git-inits the directory (creating it if absent)
//  2. Writes GITORUM.toml
//  3. Creates keys/<username>.pub with the admin's public key
//  4. Stages both files and creates the first commit
func Init(path string, meta ForumMeta, identity *crypto.Identity) (*Repo, error) {
	if err := os.MkdirAll(path, 0o755); err != nil {
		return nil, fmt.Errorf("create repo dir: %w", err)
	}

	gr, err := gogit.PlainInit(path, false)
	if err != nil {
		return nil, fmt.Errorf("git init: %w", err)
	}

	r := &Repo{Path: path, git: gr}

	// Write GITORUM.toml
	if err := r.writeMeta(meta); err != nil {
		return nil, err
	}

	// Write keys/<username>.pub
	if err := r.writePublicKey(identity.Username, identity.PublicKey); err != nil {
		return nil, err
	}

	// Stage and commit
	if err := r.commitFiles(identity, "init: initialize forum repository",
		"GITORUM.toml", filepath.Join("keys", identity.Username+".pub")); err != nil {
		return nil, err
	}

	return r, nil
}

// Open opens an existing forum repository at path.
func Open(path string) (*Repo, error) {
	gr, err := gogit.PlainOpen(path)
	if err != nil {
		return nil, fmt.Errorf("open repo at %s: %w", path, err)
	}
	abs, err := filepath.Abs(path)
	if err != nil {
		return nil, err
	}
	return &Repo{Path: abs, git: gr}, nil
}

// ReadMeta reads and returns the forum metadata from GITORUM.toml.
func (r *Repo) ReadMeta() (*ForumMeta, error) {
	var m ForumMeta
	if _, err := toml.DecodeFile(r.metaPath(), &m); err != nil {
		return nil, fmt.Errorf("read GITORUM.toml: %w", err)
	}
	return &m, nil
}

// AddRemote adds a named remote (e.g. "origin") pointing at url.
// If a remote with that name already exists it is replaced.
func (r *Repo) AddRemote(name, url string) error {
	// Remove existing remote if present (ignore error).
	_ = r.git.DeleteRemote(name)
	_, err := r.git.CreateRemote(&config.RemoteConfig{
		Name: name,
		URLs: []string{url},
	})
	if err != nil {
		return fmt.Errorf("add remote %q: %w", name, err)
	}
	return nil
}

// WritePublicKey writes a user's public key into keys/<username>.pub and
// stages + commits the change.
func (r *Repo) WritePublicKey(identity *crypto.Identity, username, pubkeyB64 string) error {
	if err := r.writePublicKey(username, pubkeyB64); err != nil {
		return err
	}
	return r.commitFiles(identity, fmt.Sprintf("keys: add public key for %s", username),
		filepath.Join("keys", username+".pub"))
}

// CreateCategory creates a new forum category by writing META.toml and
// committing it to the repository.
func (r *Repo) CreateCategory(identity *crypto.Identity, slug, name, description string) error {
	catDir := filepath.Join(r.Path, slug)
	if err := os.MkdirAll(catDir, 0o755); err != nil {
		return fmt.Errorf("create category dir: %w", err)
	}
	content := fmt.Sprintf("name = %q\ndescription = %q\n", name, description)
	if err := os.WriteFile(filepath.Join(catDir, "META.toml"), []byte(content), 0o644); err != nil {
		return fmt.Errorf("write META.toml: %w", err)
	}
	return r.commitFiles(identity, fmt.Sprintf("category: add %s", slug),
		filepath.Join(slug, "META.toml"))
}

// UpdateMeta rewrites GITORUM.toml with new metadata and commits the change.
func (r *Repo) UpdateMeta(identity *crypto.Identity, meta ForumMeta) error {
	if err := r.writeMeta(meta); err != nil {
		return err
	}
	return r.commitFiles(identity, "config: update forum metadata", "GITORUM.toml")
}

// Categories returns the slugs of all forum categories found in the working
// tree (directories that contain a META.toml file), sorted alphabetically.
func (r *Repo) Categories() ([]string, error) {
	entries, err := os.ReadDir(r.Path)
	if err != nil {
		return nil, fmt.Errorf("read repo root: %w", err)
	}
	var cats []string
	for _, e := range entries {
		if !e.IsDir() {
			continue
		}
		if _, err := os.Stat(filepath.Join(r.Path, e.Name(), "META.toml")); err == nil {
			cats = append(cats, e.Name())
		}
	}
	sort.Strings(cats)
	return cats, nil
}

// IsSynced reports whether the local HEAD matches the remote tracking ref for
// origin and the working tree is clean.  Returns (true, "") when no remote is
// configured (nothing to sync).
func (r *Repo) IsSynced() (synced bool, remoteURL string) {
	cfg, err := r.git.Config()
	if err != nil {
		return true, ""
	}
	origin, ok := cfg.Remotes["origin"]
	if !ok || len(origin.URLs) == 0 {
		return true, ""
	}
	remoteURL = origin.URLs[0]

	// Check for uncommitted changes first.
	wt, err := r.git.Worktree()
	if err == nil {
		if status, err := wt.Status(); err == nil && !status.IsClean() {
			return false, remoteURL
		}
	}

	head, err := r.git.Head()
	if err != nil {
		return false, remoteURL
	}
	remoteRef, err := r.git.Reference(
		plumbing.NewRemoteReferenceName("origin", head.Name().Short()), true)
	if err != nil {
		// Remote ref not found means we haven't pushed yet.
		return false, remoteURL
	}
	return head.Hash() == remoteRef.Hash(), remoteURL
}

// CommitPost writes content to relPath (relative to the repo root), stages
// that single file, and creates a commit.
func (r *Repo) CommitPost(identity *crypto.Identity, relPath string, content []byte) error {
	absPath := filepath.Join(r.Path, relPath)
	if err := os.MkdirAll(filepath.Dir(absPath), 0o755); err != nil {
		return fmt.Errorf("create dirs for %s: %w", relPath, err)
	}
	if err := os.WriteFile(absPath, content, 0o644); err != nil {
		return fmt.Errorf("write post file: %w", err)
	}
	wt, err := r.git.Worktree()
	if err != nil {
		return fmt.Errorf("worktree: %w", err)
	}
	if _, err := wt.Add(relPath); err != nil {
		return fmt.Errorf("git add %s: %w", relPath, err)
	}
	sig := &object.Signature{
		Name:  identity.Username,
		Email: identity.Username + "@gitorum.local",
		When:  time.Now(),
	}
	if _, err := wt.Commit("post: add "+relPath, &gogit.CommitOptions{
		Author:    sig,
		Committer: sig,
	}); err != nil {
		return fmt.Errorf("git commit: %w", err)
	}
	return nil
}

// Pull fetches from origin and fast-forward merges into the current branch.
// Returns nil when there is no remote configured or the ref is already up to
// date. Merge conflicts are returned as errors. A 30-second timeout applies.
func (r *Repo) Pull() error {
	cfg, err := r.git.Config()
	if err != nil {
		return err
	}
	if _, ok := cfg.Remotes["origin"]; !ok {
		return nil
	}

	head, err := r.git.Head()
	if err != nil {
		return fmt.Errorf("head: %w", err)
	}

	wt, err := r.git.Worktree()
	if err != nil {
		return fmt.Errorf("worktree: %w", err)
	}

	ctx, cancel := context.WithTimeout(context.Background(), gitTimeout)
	defer cancel()

	err = wt.PullContext(ctx, &gogit.PullOptions{
		RemoteName:    "origin",
		ReferenceName: head.Name(),
		Force:         false,
	})
	if err == gogit.NoErrAlreadyUpToDate {
		return nil
	}
	return err
}

// Push attempts to push to the origin remote. Returns nil if there is no
// remote configured or the ref is already up to date. A 30-second timeout
// applies.
func (r *Repo) Push() error {
	cfg, err := r.git.Config()
	if err != nil {
		return err
	}
	if _, ok := cfg.Remotes["origin"]; !ok {
		return nil
	}

	ctx, cancel := context.WithTimeout(context.Background(), gitTimeout)
	defer cancel()

	err = r.git.PushContext(ctx, &gogit.PushOptions{RemoteName: "origin"})
	if err == nil || err == gogit.NoErrAlreadyUpToDate {
		return nil
	}
	return err
}

// Git returns the underlying go-git repository for advanced callers.
func (r *Repo) Git() *gogit.Repository { return r.git }

// ---- internal helpers ----

func (r *Repo) metaPath() string {
	return filepath.Join(r.Path, "GITORUM.toml")
}

func (r *Repo) writeMeta(meta ForumMeta) error {
	f, err := os.Create(r.metaPath())
	if err != nil {
		return fmt.Errorf("create GITORUM.toml: %w", err)
	}
	defer f.Close()
	return toml.NewEncoder(f).Encode(meta)
}

func (r *Repo) writePublicKey(username, pubkeyB64 string) error {
	dir := filepath.Join(r.Path, "keys")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return fmt.Errorf("create keys dir: %w", err)
	}
	path := filepath.Join(dir, username+".pub")
	return os.WriteFile(path, []byte(pubkeyB64+"\n"), 0o644)
}

// commitFiles stages the specified relative paths and creates a commit.
func (r *Repo) commitFiles(identity *crypto.Identity, message string, relPaths ...string) error {
	wt, err := r.git.Worktree()
	if err != nil {
		return fmt.Errorf("worktree: %w", err)
	}
	for _, p := range relPaths {
		if _, err := wt.Add(p); err != nil {
			return fmt.Errorf("git add %s: %w", p, err)
		}
	}
	sig := &object.Signature{
		Name:  identity.Username,
		Email: identity.Username + "@gitorum.local",
		When:  time.Now(),
	}
	if _, err := wt.Commit(message, &gogit.CommitOptions{
		Author:    sig,
		Committer: sig,
	}); err != nil {
		return fmt.Errorf("git commit: %w", err)
	}
	return nil
}
