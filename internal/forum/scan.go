package forum

import (
	"fmt"
	"io/fs"
	"path"
	"strconv"
	"strings"
	"time"
)

// ThreadScan holds the lightweight data needed to build a thread-list entry.
// Only the root post is fully parsed; replies are counted from filenames.
type ThreadScan struct {
	Slug        string
	Root        *Post // nil if 0000_root.md is missing or unparseable
	ReplyCount  int
	LastReplyAt string // RFC3339 timestamp of newest reply, or root's timestamp
}

// ScanThread reads only the root post of <category>/<slug> and counts reply
// files. It is much cheaper than LoadThread for building thread-list views.
func ScanThread(fsys fs.FS, category, slug string, ring *KeyRing) (*ThreadScan, error) {
	if !ValidSlug(category) || !ValidSlug(slug) {
		return nil, fmt.Errorf("invalid thread path %s/%s: %w", category, slug, fs.ErrNotExist)
	}
	dir := path.Join(category, slug)
	content, err := fs.ReadFile(fsys, path.Join(dir, RootFilename))
	if err != nil {
		return nil, fmt.Errorf("read root post: %w", err)
	}
	root, err := ParsePost(RootFilename, content)
	if err != nil {
		return nil, fmt.Errorf("parse root post: %w", err)
	}
	root.Verify(ring)

	entries, err := fs.ReadDir(fsys, dir)
	if err != nil {
		return nil, fmt.Errorf("read thread dir: %w", err)
	}

	replyCount := 0
	lastAt := root.TimestampRaw
	for _, e := range entries {
		name := e.Name()
		if e.IsDir() || !ValidPostFilename(name) || name == RootFilename {
			continue
		}
		// Skip tombstoned replies.
		if exists(fsys, path.Join(dir, TombstoneFilename(name))) {
			continue
		}
		replyCount++
		if ts, ok := parseFilenameTime(name); ok {
			lastAt = ts.UTC().Format(time.RFC3339)
		}
	}

	return &ThreadScan{
		Slug:        slug,
		Root:        root,
		ReplyCount:  replyCount,
		LastReplyAt: lastAt,
	}, nil
}

// parseFilenameTime extracts the UTC timestamp embedded in a reply filename.
// Format: {unix_millis}_{hash8}.md
func parseFilenameTime(name string) (time.Time, bool) {
	base := strings.TrimSuffix(name, ".md")
	idx := strings.IndexByte(base, '_')
	if idx < 0 {
		return time.Time{}, false
	}
	millis, err := strconv.ParseInt(base[:idx], 10, 64)
	if err != nil {
		return time.Time{}, false
	}
	return time.UnixMilli(millis).UTC(), true
}
