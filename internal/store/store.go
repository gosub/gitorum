// Package store implements forum operations on top of a git repository.
//
// It is the only package that knows the on-disk layout (categories, threads,
// keys, requests, tombstones). Reads come from the HEAD tree through
// repo.Snapshot and are cached until HEAD moves; writes go through
// repo.Commit. Both the HTTP API and the CLI use it.
package store

import (
	"errors"
	"fmt"
	"io/fs"
	"log"
	"path"
	"strings"
	"sync"

	"github.com/BurntSushi/toml"
	"github.com/go-git/go-git/v5/plumbing"

	"github.com/gosub/gitorum/internal/crypto"
	"github.com/gosub/gitorum/internal/forum"
	"github.com/gosub/gitorum/internal/repo"
)

// Sentinel errors; callers match them with errors.Is.
var (
	ErrNotFound  = errors.New("not found")
	ErrExists    = errors.New("already exists")
	ErrForbidden = errors.New("admin access required")
	ErrInvalid   = errors.New("invalid input")
)

// RequestsDir holds pending join requests.
const RequestsDir = "requests"

// Store provides forum-level reads and writes.
type Store struct {
	repo *repo.Repo

	mu   sync.Mutex // guards view
	view *view
}

// view is everything derived from one HEAD commit. It is immutable once
// built, apart from the lazily filled caches guarded by mu.
type view struct {
	hash     plumbing.Hash
	fs       fs.FS
	meta     *repo.ForumMeta
	adminKey string // pinned locally; authoritative over meta.AdminPubkey
	ring     *forum.KeyRing

	mu      sync.Mutex
	threads map[string]*forum.Thread // "cat/slug" -> thread
}

// New returns a Store backed by r.
func New(r *repo.Repo) *Store {
	return &Store{repo: r}
}

// Repo returns the underlying repository.
func (s *Store) Repo() *repo.Repo { return s.repo }

// current returns the view for the current HEAD, rebuilding it if HEAD moved.
func (s *Store) current() (*view, error) {
	snap, err := s.repo.Snapshot()
	if err != nil {
		return nil, err
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.view != nil && s.view.hash == snap.Hash {
		return s.view, nil
	}

	v := &view{hash: snap.Hash, fs: snap.FS, threads: map[string]*forum.Thread{}}
	data, err := fs.ReadFile(snap.FS, repo.MetaFile)
	if err != nil {
		return nil, fmt.Errorf("read %s: %w", repo.MetaFile, err)
	}
	var meta repo.ForumMeta
	if _, err := toml.Decode(string(data), &meta); err != nil {
		return nil, fmt.Errorf("decode %s: %w", repo.MetaFile, err)
	}
	v.meta = &meta

	// Trust on first use: a repository without a pinned admin key (cloned by
	// an older version, or with plain git) pins the one it sees now.
	v.adminKey = s.repo.PinnedAdminKey()
	if v.adminKey == "" && meta.AdminPubkey != "" {
		if err := s.repo.PinAdminKey(meta.AdminPubkey); err != nil {
			return nil, err
		}
		log.Printf("pinned forum admin key %s (trust on first use)", forum.Fingerprint(meta.AdminPubkey))
		v.adminKey = meta.AdminPubkey
	}
	if v.ring, err = forum.LoadKeyRing(snap.FS, v.adminKey); err != nil {
		return nil, err
	}
	s.view = v
	return v, nil
}

// ---- reads -----------------------------------------------------------------

// Meta returns the forum metadata.
func (s *Store) Meta() (*repo.ForumMeta, error) {
	v, err := s.current()
	if err != nil {
		return nil, err
	}
	m := *v.meta
	return &m, nil
}

// IsAdmin reports whether id holds the pinned forum admin key.
func (s *Store) IsAdmin(id *crypto.Identity) bool {
	if id == nil {
		return false
	}
	v, err := s.current()
	if err != nil {
		return false
	}
	return v.adminKey != "" && id.PublicKey == v.adminKey
}

// AdminKeyMismatch reports whether GITORUM.toml names a different admin key
// than the one pinned locally, which means someone changed the admin in the
// shared repository. The pinned key keeps being used.
func (s *Store) AdminKeyMismatch() bool {
	v, err := s.current()
	if err != nil {
		return false
	}
	return v.meta.AdminPubkey != v.adminKey
}

// HasKey reports whether username has a registered key.
func (s *Store) HasKey(username string) bool {
	v, err := s.current()
	if err != nil {
		return false
	}
	return v.ring.Has(username)
}

// Categories returns all categories, sorted by slug.
func (s *Store) Categories() ([]*forum.Category, error) {
	v, err := s.current()
	if err != nil {
		return nil, err
	}
	slugs, err := forum.ListCategories(v.fs)
	if err != nil {
		return nil, err
	}
	cats := make([]*forum.Category, 0, len(slugs))
	for _, slug := range slugs {
		c, err := forum.LoadCategory(v.fs, slug)
		if err != nil {
			continue
		}
		cats = append(cats, c)
	}
	return cats, nil
}

// Threads returns a category and lightweight summaries of its threads.
func (s *Store) Threads(cat string) (*forum.Category, []*forum.ThreadScan, error) {
	v, err := s.current()
	if err != nil {
		return nil, nil, err
	}
	c, err := forum.LoadCategory(v.fs, cat)
	if err != nil {
		return nil, nil, fmt.Errorf("category %q: %w", cat, ErrNotFound)
	}
	scans := make([]*forum.ThreadScan, 0, len(c.ThreadSlugs))
	for _, slug := range c.ThreadSlugs {
		scan, err := forum.ScanThread(v.fs, cat, slug, v.ring)
		if err != nil {
			continue
		}
		scans = append(scans, scan)
	}
	return c, scans, nil
}

// Thread loads a full thread.
func (s *Store) Thread(cat, slug string) (*forum.Thread, error) {
	v, err := s.current()
	if err != nil {
		return nil, err
	}
	return v.thread(cat, slug)
}

func (v *view) thread(cat, slug string) (*forum.Thread, error) {
	key := cat + "/" + slug
	v.mu.Lock()
	defer v.mu.Unlock()
	if t, ok := v.threads[key]; ok {
		return t, nil
	}
	if !forum.ValidSlug(cat) || !forum.ValidSlug(slug) {
		return nil, fmt.Errorf("thread %s: %w", key, ErrNotFound)
	}
	t, err := forum.LoadThread(v.fs, cat, slug, v.ring)
	if err != nil {
		return nil, fmt.Errorf("thread %s: %w", key, ErrNotFound)
	}
	v.threads[key] = t
	return t, nil
}

// JoinRequest is a pending key request stored in requests/<username>.pub.
type JoinRequest struct {
	Username string
	PubKey   string
}

// JoinRequests returns pending requests whose username has no key yet.
func (s *Store) JoinRequests() ([]JoinRequest, error) {
	v, err := s.current()
	if err != nil {
		return nil, err
	}
	entries, err := fs.ReadDir(v.fs, RequestsDir)
	if errors.Is(err, fs.ErrNotExist) {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("read requests dir: %w", err)
	}
	var out []JoinRequest
	for _, e := range entries {
		user, ok := strings.CutSuffix(e.Name(), ".pub")
		if e.IsDir() || !ok || !forum.ValidUsername(user) || v.ring.Has(user) {
			continue
		}
		data, err := fs.ReadFile(v.fs, path.Join(RequestsDir, e.Name()))
		if err != nil {
			continue
		}
		out = append(out, JoinRequest{Username: user, PubKey: strings.TrimSpace(string(data))})
	}
	return out, nil
}

// ---- writes ----------------------------------------------------------------

func (s *Store) requireAdmin(id *crypto.Identity) error {
	if !s.IsAdmin(id) {
		return ErrForbidden
	}
	return nil
}

func exists(v *view, name string) bool {
	_, err := fs.Stat(v.fs, name)
	return err == nil
}

// NewThread creates <cat>/<slug>/0000_root.md signed by id.
func (s *Store) NewThread(id *crypto.Identity, cat, slug, body string) error {
	if !forum.ValidSlug(slug) {
		return fmt.Errorf("slug must be lowercase letters, digits, and hyphens: %w", ErrInvalid)
	}
	if strings.TrimSpace(body) == "" {
		return fmt.Errorf("body is required: %w", ErrInvalid)
	}
	v, err := s.current()
	if err != nil {
		return err
	}
	if !forum.ValidSlug(cat) || !exists(v, path.Join(cat, forum.CategoryMetaFile)) {
		return fmt.Errorf("category %q: %w", cat, ErrNotFound)
	}
	if exists(v, path.Join(cat, slug)) {
		return fmt.Errorf("thread slug %q: %w", slug, ErrExists)
	}
	post, err := forum.SignPost(id, "", body)
	if err != nil {
		return fmt.Errorf("sign post: %w", err)
	}
	rel := path.Join(cat, slug, forum.RootFilename)
	return s.repo.Commit(id, "post: add "+rel, map[string][]byte{rel: post.Format()}, nil)
}

// Reply adds a reply to <cat>/<slug> signed by id and returns its filename.
func (s *Store) Reply(id *crypto.Identity, cat, slug, body string) (string, error) {
	if strings.TrimSpace(body) == "" {
		return "", fmt.Errorf("body is required: %w", ErrInvalid)
	}
	v, err := s.current()
	if err != nil {
		return "", err
	}
	if !forum.ValidSlug(cat) || !forum.ValidSlug(slug) {
		return "", fmt.Errorf("thread %s/%s: %w", cat, slug, ErrNotFound)
	}
	root, err := fs.ReadFile(v.fs, path.Join(cat, slug, forum.RootFilename))
	if err != nil {
		return "", fmt.Errorf("thread %s/%s: %w", cat, slug, ErrNotFound)
	}
	post, err := forum.SignPost(id, forum.PostHash(root), body)
	if err != nil {
		return "", fmt.Errorf("sign post: %w", err)
	}
	content := post.Format()
	name := forum.NewPostFilename(content)
	rel := path.Join(cat, slug, name)
	if err := s.repo.Commit(id, "post: add "+rel, map[string][]byte{rel: content}, nil); err != nil {
		return "", err
	}
	return name, nil
}

// Tombstone hides a post by committing an admin-signed <file>.tomb.
func (s *Store) Tombstone(admin *crypto.Identity, cat, slug, filename string) error {
	if !forum.ValidSlug(cat) || !forum.ValidSlug(slug) || !forum.ValidPostFilename(filename) {
		return fmt.Errorf("category, thread, or filename: %w", ErrInvalid)
	}
	if err := s.requireAdmin(admin); err != nil {
		return err
	}
	v, err := s.current()
	if err != nil {
		return err
	}
	rel := path.Join(cat, slug, filename)
	content, err := fs.ReadFile(v.fs, rel)
	if err != nil {
		return fmt.Errorf("post %s: %w", rel, ErrNotFound)
	}
	tomb, err := forum.SignTombstone(admin, content)
	if err != nil {
		return fmt.Errorf("sign tombstone: %w", err)
	}
	tombRel := path.Join(cat, slug, forum.TombstoneFilename(filename))
	return s.repo.Commit(admin, "tombstone: "+rel, map[string][]byte{tombRel: tomb.Format()}, nil)
}

// CreateCategory adds <slug>/META.toml.
func (s *Store) CreateCategory(admin *crypto.Identity, slug, name, description string) error {
	if !forum.ValidSlug(slug) {
		return fmt.Errorf("slug must be lowercase letters, digits, and hyphens: %w", ErrInvalid)
	}
	if name == "" {
		return fmt.Errorf("name is required: %w", ErrInvalid)
	}
	if err := s.requireAdmin(admin); err != nil {
		return err
	}
	v, err := s.current()
	if err != nil {
		return err
	}
	rel := path.Join(slug, forum.CategoryMetaFile)
	if exists(v, rel) {
		return fmt.Errorf("category slug %q: %w", slug, ErrExists)
	}
	return s.repo.Commit(admin, "category: add "+slug,
		map[string][]byte{rel: forum.EncodeCategoryMeta(name, description)}, nil)
}

// UpdateMeta rewrites GITORUM.toml.
func (s *Store) UpdateMeta(admin *crypto.Identity, meta repo.ForumMeta) error {
	if err := s.requireAdmin(admin); err != nil {
		return err
	}
	data, err := meta.Encode()
	if err != nil {
		return err
	}
	return s.repo.Commit(admin, "config: update forum metadata", map[string][]byte{repo.MetaFile: data}, nil)
}

// AddKey registers a public key for a new user and certifies it. With rotate
// set, the key is instead appended to an existing user's keys, so posts made
// with the old key keep verifying.
func (s *Store) AddKey(admin *crypto.Identity, username, pubkey string, rotate bool) error {
	if !forum.ValidUsername(username) {
		return fmt.Errorf("username must be lowercase letters, digits, - and _: %w", ErrInvalid)
	}
	if err := crypto.ValidatePublicKey(pubkey); err != nil {
		return fmt.Errorf("%v: %w", err, ErrInvalid)
	}
	if err := s.requireAdmin(admin); err != nil {
		return err
	}
	v, err := s.current()
	if err != nil {
		return err
	}
	keys := v.ring.Keys(username)
	msg := "keys: add public key for " + username
	switch {
	case rotate && len(keys) == 0:
		return fmt.Errorf("@%s has no key to rotate: %w", username, ErrNotFound)
	case rotate:
		for _, k := range keys {
			if k == pubkey {
				return fmt.Errorf("@%s already has this key: %w", username, ErrExists)
			}
		}
		keys = append(append([]string{}, keys...), pubkey)
		msg = "keys: rotate key for " + username
	case len(keys) > 0:
		return fmt.Errorf("username @%s already has a key: %w", username, ErrExists)
	default:
		keys = []string{pubkey}
	}
	files, err := keyFiles(admin, username, keys)
	if err != nil {
		return err
	}
	return s.repo.Commit(admin, msg, files, nil)
}

// keyFiles returns keys/<username>.pub and its admin certificate.
func keyFiles(admin *crypto.Identity, username string, keys []string) (map[string][]byte, error) {
	pub := forum.EncodeKeyFile(keys)
	sig, err := forum.SignKeyFile(admin, username, pub)
	if err != nil {
		return nil, fmt.Errorf("certify key: %w", err)
	}
	return map[string][]byte{forum.KeyFile(username): pub, forum.CertFile(username): sig}, nil
}

// Certify signs the existing key file of each username with the admin key.
// It is used to migrate forums created before keys were certified; the
// admin should check each key out of band first.
func (s *Store) Certify(admin *crypto.Identity, usernames ...string) error {
	if err := s.requireAdmin(admin); err != nil {
		return err
	}
	v, err := s.current()
	if err != nil {
		return err
	}
	files := map[string][]byte{}
	for _, u := range usernames {
		if !forum.ValidUsername(u) {
			return fmt.Errorf("username %q: %w", u, ErrInvalid)
		}
		pub, err := fs.ReadFile(v.fs, forum.KeyFile(u))
		if err != nil {
			return fmt.Errorf("key for @%s: %w", u, ErrNotFound)
		}
		sig, err := forum.SignKeyFile(admin, u, pub)
		if err != nil {
			return err
		}
		files[forum.CertFile(u)] = sig
	}
	if len(files) == 0 {
		return nil
	}
	return s.repo.Commit(admin, "keys: certify "+strings.Join(usernames, ", "), files, nil)
}

// SubmitJoinRequest writes requests/<username>.pub with id's public key.
// The caller should Push to share it.
func (s *Store) SubmitJoinRequest(id *crypto.Identity) error {
	if !forum.ValidUsername(id.Username) {
		return fmt.Errorf("username %q: %w", id.Username, ErrInvalid)
	}
	if s.HasKey(id.Username) {
		return fmt.Errorf("@%s is already registered: %w", id.Username, ErrExists)
	}
	rel := path.Join(RequestsDir, id.Username+".pub")
	return s.repo.Commit(id, "request: join request from "+id.Username,
		map[string][]byte{rel: []byte(id.PublicKey + "\n")}, nil)
}

// ApproveJoinRequest moves requests/<username>.pub into keys/ in one commit.
func (s *Store) ApproveJoinRequest(admin *crypto.Identity, username string) error {
	if !forum.ValidUsername(username) {
		return fmt.Errorf("username %q: %w", username, ErrInvalid)
	}
	if err := s.requireAdmin(admin); err != nil {
		return err
	}
	req, err := s.findRequest(username)
	if err != nil {
		return err
	}
	if s.HasKey(username) {
		return fmt.Errorf("username @%s already has a key: %w", username, ErrExists)
	}
	reqRel := path.Join(RequestsDir, username+".pub")
	files, err := keyFiles(admin, username, []string{req.PubKey})
	if err != nil {
		return err
	}
	return s.repo.Commit(admin, "keys: approve join request from "+username, files, []string{reqRel})
}

// RejectJoinRequest deletes requests/<username>.pub.
func (s *Store) RejectJoinRequest(admin *crypto.Identity, username string) error {
	if !forum.ValidUsername(username) {
		return fmt.Errorf("username %q: %w", username, ErrInvalid)
	}
	if err := s.requireAdmin(admin); err != nil {
		return err
	}
	if _, err := s.findRequest(username); err != nil {
		return err
	}
	reqRel := path.Join(RequestsDir, username+".pub")
	return s.repo.Commit(admin, "request: reject join request from "+username, nil, []string{reqRel})
}

func (s *Store) findRequest(username string) (*JoinRequest, error) {
	reqs, err := s.JoinRequests()
	if err != nil {
		return nil, err
	}
	for _, r := range reqs {
		if r.Username == username {
			return &r, nil
		}
	}
	return nil, fmt.Errorf("join request from @%s: %w", username, ErrNotFound)
}

// Sync integrates origin and pushes local commits. When auto-approval is
// enabled and id is the admin, pending join requests are approved and the
// approvals are pushed in a second round.
func (s *Store) Sync(id *crypto.Identity) (repo.SyncResult, error) {
	res, err := s.repo.Sync()
	if err != nil {
		return res, err
	}
	meta, err := s.Meta()
	if err != nil || !meta.AutoApproveKeys || !s.IsAdmin(id) {
		return res, nil
	}
	reqs, err := s.JoinRequests()
	if err != nil || len(reqs) == 0 {
		return res, err
	}
	for _, r := range reqs {
		if err := s.ApproveJoinRequest(id, r.Username); err != nil {
			return res, fmt.Errorf("auto-approve %s: %w", r.Username, err)
		}
	}
	res2, err := s.repo.Sync()
	res.Pulled += res2.Pulled
	res.Rebased += res2.Rebased
	res.Pushed = res.Pushed || res2.Pushed
	return res, err
}
