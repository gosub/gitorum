package forum

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"io/fs"
	"path"
	"sort"
	"time"
)

// RootFilename is the fixed filename for the first post in every thread.
const RootFilename = "0000_root.md"

// Thread holds all posts in a thread, ordered root-first then chronologically.
type Thread struct {
	Category string
	Slug     string
	Root     *Post   // convenience pointer; nil when the root post is missing
	Posts    []*Post // root first, then replies sorted by Timestamp ascending
}

// LoadThread reads every post in <category>/<slug> from fsys, parses and
// verifies each one against ring, and returns the posts sorted root-first,
// then by timestamp.
func LoadThread(fsys fs.FS, category, slug string, ring *KeyRing) (*Thread, error) {
	if !ValidSlug(category) || !ValidSlug(slug) {
		return nil, fmt.Errorf("invalid thread path %s/%s: %w", category, slug, fs.ErrNotExist)
	}
	dir := path.Join(category, slug)
	entries, err := fs.ReadDir(fsys, dir)
	if err != nil {
		return nil, fmt.Errorf("read thread dir %s: %w", dir, err)
	}

	t := &Thread{Category: category, Slug: slug}

	for _, entry := range entries {
		name := entry.Name()
		if entry.IsDir() || !ValidPostFilename(name) {
			continue
		}
		content, err := fs.ReadFile(fsys, path.Join(dir, name))
		if err != nil {
			return nil, fmt.Errorf("read %s: %w", name, err)
		}
		// If a valid admin tombstone exists, include a placeholder so the
		// thread still shows where the post was (like "[deleted]") without
		// revealing the original content. Forged tombstones are ignored.
		if hiddenBy(fsys, dir, name, content, ring.AdminKey) {
			ts, _ := parseFilenameTime(name)
			t.Posts = append(t.Posts, &Post{
				Filename:   name,
				Timestamp:  ts,
				Tombstoned: true,
			})
			continue
		}
		post, err := ParsePost(name, content)
		if err != nil {
			// Malformed post: include it with SigInvalid so it is still visible.
			post = &Post{Filename: name, SigStatus: SigInvalid, SigError: err.Error()}
		} else {
			post.Verify(ring)
		}
		t.Posts = append(t.Posts, post)
	}

	sort.SliceStable(t.Posts, func(i, j int) bool {
		if t.Posts[i].Filename == RootFilename {
			return true
		}
		if t.Posts[j].Filename == RootFilename {
			return false
		}
		return t.Posts[i].Timestamp.Before(t.Posts[j].Timestamp)
	})

	if len(t.Posts) > 0 && t.Posts[0].Filename == RootFilename {
		t.Root = t.Posts[0]
	}
	return t, nil
}

// NewPostFilename generates the filename for a new reply post.
// Format: {unix_millis}_{sha256_of_content[:8]}.md
//
// content should be the signed, formatted post: it includes the signature,
// so two posts with the same body in the same millisecond still differ.
func NewPostFilename(content []byte) string {
	h := sha256.Sum256(content)
	return fmt.Sprintf("%d_%s.md", time.Now().UnixMilli(), hex.EncodeToString(h[:])[:8])
}
