// Package repo wraps go-git and provides all git-level operations for gitorum.
package repo

import (
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
	if err := r.commitAll(identity, "init: initialize forum repository"); err != nil {
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
	return r.commitAll(identity, fmt.Sprintf("keys: add public key for %s", username))
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

// commitAll stages every change in the working tree and creates a commit.
func (r *Repo) commitAll(identity *crypto.Identity, message string) error {
	wt, err := r.git.Worktree()
	if err != nil {
		return fmt.Errorf("worktree: %w", err)
	}
	if err := wt.AddGlob("."); err != nil {
		return fmt.Errorf("git add: %w", err)
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
