package forum

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
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

// LoadThread reads every .md file in dir, parses and signature-verifies each
// one, then returns a Thread with posts sorted root-first, then by timestamp.
func LoadThread(category, slug, dir, keysDir string) (*Thread, error) {
	entries, err := os.ReadDir(dir)
	if err != nil {
		return nil, fmt.Errorf("read thread dir %s: %w", dir, err)
	}

	t := &Thread{Category: category, Slug: slug}

	for _, entry := range entries {
		name := entry.Name()
		if entry.IsDir() || !strings.HasSuffix(name, ".md") {
			continue
		}
		// Skip tombstoned posts.
		if _, err := os.Stat(filepath.Join(dir, TombstoneFilename(name))); err == nil {
			continue
		}
		content, err := os.ReadFile(filepath.Join(dir, name))
		if err != nil {
			return nil, fmt.Errorf("read %s: %w", name, err)
		}
		post, err := ParsePost(name, content)
		if err != nil {
			// Malformed post: include it with SigInvalid so it is still visible.
			post = &Post{Filename: name, SigStatus: SigInvalid, SigError: err.Error()}
		} else {
			post.VerifySignature(keysDir)
		}
		t.Posts = append(t.Posts, post)
	}

	sort.Slice(t.Posts, func(i, j int) bool {
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
// Format: {unix_millis}_{sha256_of_body[:8]}.md
func NewPostFilename(body string) string {
	h := sha256.Sum256([]byte(body))
	return fmt.Sprintf("%d_%s.md", time.Now().UnixMilli(), hex.EncodeToString(h[:])[:8])
}
