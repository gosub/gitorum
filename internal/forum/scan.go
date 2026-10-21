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
	Hidden      bool  // the root post is tombstoned; Root is nil
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
	if isNotExist(err) {
		// Purged root: the thread stays listed only with a valid tombstone.
		tomb, terr := fs.ReadFile(fsys, path.Join(dir, TombstoneFilename(RootFilename)))
		if terr != nil || VerifyTombstoneSignature(tomb, ring.AdminKey) != nil {
			return nil, fmt.Errorf("read root post: %w", err)
		}
		content, err = nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("read root post: %w", err)
	}
	var root *Post
	lastAt := ""
	hidden := content == nil || hiddenBy(fsys, dir, RootFilename, content, ring.AdminKey)
	if !hidden {
		if root, err = ParsePost(RootFilename, content); err != nil {
			return nil, fmt.Errorf("parse root post: %w", err)
		}
		root.Verify(ring)
		lastAt = root.TimestampRaw
	}

	entries, err := fs.ReadDir(fsys, dir)
	if err != nil {
		return nil, fmt.Errorf("read thread dir: %w", err)
	}

	replyCount := 0
	for _, e := range entries {
		name := e.Name()
		if e.IsDir() || !ValidPostFilename(name) || name == RootFilename {
			continue
		}
		// Skip replies hidden by a valid tombstone.
		if exists(fsys, path.Join(dir, TombstoneFilename(name))) {
			if content, err := fs.ReadFile(fsys, path.Join(dir, name)); err == nil &&
				hiddenBy(fsys, dir, name, content, ring.AdminKey) {
				continue
			}
		}
		replyCount++
		if ts, ok := parseFilenameTime(name); ok {
			lastAt = ts.UTC().Format(time.RFC3339)
		}
	}

	return &ThreadScan{
		Slug:        slug,
		Root:        root,
		Hidden:      hidden,
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
