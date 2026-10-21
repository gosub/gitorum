package store

import (
	"fmt"
	"io/fs"
	"path"
	"sort"
	"strings"

	"github.com/gosub/gitorum/internal/forum"
)

// Problem is one finding of Verify.
type Problem struct {
	Path    string
	Message string
}

// Verify audits the whole forum at HEAD: admin key pinning, key
// certificates, post signatures and tombstones. It returns every problem
// found, sorted by path; an empty result means everything checks out.
func (s *Store) Verify() ([]Problem, error) {
	v, err := s.current()
	if err != nil {
		return nil, err
	}
	var out []Problem
	add := func(p, format string, args ...any) {
		out = append(out, Problem{Path: p, Message: fmt.Sprintf(format, args...)})
	}

	if v.adminKey == "" {
		add("GITORUM.toml", "no admin key pinned and none in GITORUM.toml")
	} else if v.meta.AdminPubkey != v.adminKey {
		add("GITORUM.toml", "admin_pubkey %s differs from the locally pinned admin key %s",
			forum.Fingerprint(v.meta.AdminPubkey), forum.Fingerprint(v.adminKey))
	}
	for _, u := range v.ring.Users() {
		if uk := v.ring.User(u); !uk.Certified {
			add(forum.KeyFile(u), "%s", uk.Problem)
		}
	}

	cats, err := forum.ListCategories(v.fs)
	if err != nil {
		return nil, err
	}
	for _, cat := range cats {
		c, err := forum.LoadCategory(v.fs, cat)
		if err != nil {
			add(path.Join(cat, forum.CategoryMetaFile), "%v", err)
			continue
		}
		for _, slug := range c.ThreadSlugs {
			out = append(out, verifyThread(v, cat, slug)...)
		}
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Path < out[j].Path })
	return out, nil
}

func verifyThread(v *view, cat, slug string) []Problem {
	dir := path.Join(cat, slug)
	var out []Problem
	t, err := v.thread(cat, slug)
	if err != nil {
		return []Problem{{Path: dir, Message: err.Error()}}
	}
	for _, p := range t.Posts {
		if p.Tombstoned || p.SigStatus == forum.SigValid {
			continue
		}
		status := map[forum.SigStatus]string{
			forum.SigInvalid:   "invalid signature",
			forum.SigMissing:   "no key for author",
			forum.SigUntrusted: "signed with an uncertified key",
		}[p.SigStatus]
		out = append(out, Problem{Path: path.Join(dir, p.Filename), Message: status + ": " + p.SigError})
	}

	entries, _ := fs.ReadDir(v.fs, dir)
	for _, e := range entries {
		target, ok := strings.CutSuffix(e.Name(), ".tomb")
		if !ok {
			continue
		}
		tomb, _ := fs.ReadFile(v.fs, path.Join(dir, e.Name()))
		content, err := fs.ReadFile(v.fs, path.Join(dir, target))
		if err != nil {
			continue // post purged; the tombstone is just a record
		}
		if err := forum.VerifyTombstone(tomb, content, v.adminKey); err != nil {
			out = append(out, Problem{Path: path.Join(dir, e.Name()), Message: "ignored tombstone: " + err.Error()})
		}
	}
	return out
}
