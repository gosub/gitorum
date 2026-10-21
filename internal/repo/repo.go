// Package repo wraps go-git and provides all git-level operations for gitorum.
//
// It knows nothing about posts, keys or categories: it initializes and opens
// repositories, exposes the HEAD tree as a read-only fs.FS, creates commits,
// and talks to the origin remote. Forum semantics live in internal/store.
package repo

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"sync"
	"time"

	gogit "github.com/go-git/go-git/v5"
	"github.com/go-git/go-git/v5/config"
	"github.com/go-git/go-git/v5/plumbing"
	"github.com/go-git/go-git/v5/plumbing/object"

	"github.com/BurntSushi/toml"
	"github.com/gosub/gitorum/internal/crypto"
)

const gitTimeout = 30 * time.Second

// MetaFile is the forum metadata file at the repository root.
const MetaFile = "GITORUM.toml"

// ForumMeta is the data stored in GITORUM.toml at the repository root.
type ForumMeta struct {
	Name            string `toml:"name"`
	Description     string `toml:"description"`
	AdminPubkey     string `toml:"admin_pubkey"`
	AutoApproveKeys bool   `toml:"auto_approve_keys"` // approve join requests automatically on sync
}

// Encode serializes the metadata as TOML.
func (m ForumMeta) Encode() ([]byte, error) {
	var buf bytes.Buffer
	if err := toml.NewEncoder(&buf).Encode(m); err != nil {
		return nil, fmt.Errorf("encode %s: %w", MetaFile, err)
	}
	return buf.Bytes(), nil
}

// Repo wraps a go-git repository and exposes forum-level operations.
//
// A Repo is safe for concurrent use: every exported method holds mu for its
// whole duration, so git operations (which share one index and worktree) are
// serialized. Unexported helpers assume mu is already held.
type Repo struct {
	// Path is the absolute path to the repository working tree.
	Path string
	git  *gogit.Repository
	mu   sync.Mutex

	// Status cache: ahead/behind counts for a (local, remote) hash pair.
	statusKey   [2]plumbing.Hash
	statusCache [2]int
}

// nowFunc is the clock used for commit timestamps.
var nowFunc = time.Now

// Init creates a new forum repository at path.
//
// It:
//  1. git-inits the directory (creating it if absent)
//  2. Writes GITORUM.toml
//  3. Creates keys/<username>.pub with the admin's public key
//  4. Stages both files and creates the first commit
func Init(path string, meta ForumMeta, identity *crypto.Identity) (*Repo, error) {
	abs, err := filepath.Abs(path)
	if err != nil {
		return nil, fmt.Errorf("resolve repo path: %w", err)
	}
	if err := os.MkdirAll(abs, 0o755); err != nil {
		return nil, fmt.Errorf("create repo dir: %w", err)
	}

	gr, err := gogit.PlainInit(abs, false)
	if err != nil {
		return nil, fmt.Errorf("git init: %w", err)
	}
	r := &Repo{Path: abs, git: gr}

	metaBytes, err := meta.Encode()
	if err != nil {
		return nil, err
	}
	files := map[string][]byte{
		MetaFile:                             metaBytes,
		"keys/" + identity.Username + ".pub": []byte(identity.PublicKey + "\n"),
	}
	if err := r.commit(identity, "init: initialize forum repository", files, nil); err != nil {
		return nil, err
	}
	if err := r.pinAdminKey(meta.AdminPubkey); err != nil {
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

// Snapshot is a read-only view of the tree at a given commit.
type Snapshot struct {
	Hash plumbing.Hash // zero when the repository has no commits yet
	FS   *TreeFS
}

// Snapshot returns a view of the current HEAD tree. Forum content is always
// read from HEAD rather than the working tree, so stray untracked or modified
// files never show up as posts, and a snapshot never changes underneath a
// reader.
func (r *Repo) Snapshot() (*Snapshot, error) {
	r.mu.Lock()
	defer r.mu.Unlock()

	head, err := r.git.Head()
	if errors.Is(err, plumbing.ErrReferenceNotFound) {
		return &Snapshot{FS: &TreeFS{r: r}}, nil
	}
	if err != nil {
		return nil, fmt.Errorf("head: %w", err)
	}
	commit, err := r.git.CommitObject(head.Hash())
	if err != nil {
		return nil, fmt.Errorf("head commit: %w", err)
	}
	tree, err := commit.Tree()
	if err != nil {
		return nil, fmt.Errorf("head tree: %w", err)
	}
	return &Snapshot{Hash: head.Hash(), FS: &TreeFS{r: r, tree: tree}}, nil
}

// Head returns the current HEAD commit hash (zero if there are no commits).
func (r *Repo) Head() plumbing.Hash {
	r.mu.Lock()
	defer r.mu.Unlock()
	head, err := r.git.Head()
	if err != nil {
		return plumbing.ZeroHash
	}
	return head.Hash()
}

// Commit writes the given files (paths relative to the repo root, using
// forward slashes), removes the given paths, stages exactly those paths, and
// creates one commit authored by identity.
func (r *Repo) Commit(identity *crypto.Identity, message string, write map[string][]byte, remove []string) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	return r.commit(identity, message, write, remove)
}

// AddRemote adds a named remote (e.g. "origin") pointing at url.
// If a remote with that name already exists it is replaced.
func (r *Repo) AddRemote(name, url string) error {
	r.mu.Lock()
	defer r.mu.Unlock()
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

// RemoteURL returns the URL of origin, or "" when none is configured.
func (r *Repo) RemoteURL() string {
	r.mu.Lock()
	defer r.mu.Unlock()
	return r.remoteURL()
}

// Push attempts to push to the origin remote. Returns nil if there is no
// remote configured or the ref is already up to date. A 30-second timeout
// applies.
func (r *Repo) Push() error {
	r.mu.Lock()
	defer r.mu.Unlock()

	if r.remoteURL() == "" {
		return nil
	}
	ctx, cancel := context.WithTimeout(context.Background(), gitTimeout)
	defer cancel()

	err := r.git.PushContext(ctx, &gogit.PushOptions{RemoteName: "origin"})
	if err == nil || err == gogit.NoErrAlreadyUpToDate {
		return nil
	}
	return err
}

// ---- internal helpers ----

func (r *Repo) remoteURL() string {
	cfg, err := r.git.Config()
	if err != nil {
		return ""
	}
	origin, ok := cfg.Remotes["origin"]
	if !ok || len(origin.URLs) == 0 {
		return ""
	}
	return origin.URLs[0]
}

func signature(identity *crypto.Identity) *object.Signature {
	return &object.Signature{
		Name:  identity.Username,
		Email: identity.Username + "@gitorum.local",
		When:  nowFunc(),
	}
}

func (r *Repo) commit(identity *crypto.Identity, message string, write map[string][]byte, remove []string) error {
	sig := signature(identity)
	return r.commitAs(sig, sig, message, write, remove)
}

func (r *Repo) commitAs(author, committer *object.Signature, message string, write map[string][]byte, remove []string) error {
	wt, err := r.git.Worktree()
	if err != nil {
		return fmt.Errorf("worktree: %w", err)
	}

	paths := make([]string, 0, len(write))
	for p := range write {
		paths = append(paths, p)
	}
	sort.Strings(paths)
	for _, p := range paths {
		abs := filepath.Join(r.Path, filepath.FromSlash(p))
		if err := os.MkdirAll(filepath.Dir(abs), 0o755); err != nil {
			return fmt.Errorf("create dirs for %s: %w", p, err)
		}
		if err := os.WriteFile(abs, write[p], 0o644); err != nil {
			return fmt.Errorf("write %s: %w", p, err)
		}
		if _, err := wt.Add(p); err != nil {
			return fmt.Errorf("git add %s: %w", p, err)
		}
	}
	for _, p := range remove {
		if _, err := wt.Remove(p); err != nil {
			return fmt.Errorf("git rm %s: %w", p, err)
		}
	}

	if _, err := wt.Commit(message, &gogit.CommitOptions{
		Author:    author,
		Committer: committer,
	}); err != nil {
		return fmt.Errorf("git commit: %w", err)
	}
	return nil
}
