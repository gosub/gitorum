package forum

import (
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"
)

// ThreadScan holds the lightweight data needed to build a thread-list entry.
// Only the root post is fully parsed; replies are counted from filenames.
type ThreadScan struct {
	Slug        string
	Root        *Post  // nil if 0000_root.md is missing or unparseable
	ReplyCount  int
	LastReplyAt string // RFC3339 timestamp of newest reply, or root's timestamp
}

// ScanThread reads only 0000_root.md from dir and counts reply files.
// It is much cheaper than LoadThread for building thread-list views.
func ScanThread(slug, dir, keysDir string) (*ThreadScan, error) {
	rootPath := filepath.Join(dir, RootFilename)
	content, err := os.ReadFile(rootPath)
	if err != nil {
		return nil, fmt.Errorf("read root post: %w", err)
	}
	root, err := ParsePost(RootFilename, content)
	if err != nil {
		return nil, fmt.Errorf("parse root post: %w", err)
	}
	root.VerifySignature(keysDir)

	entries, err := os.ReadDir(dir)
	if err != nil {
		return nil, fmt.Errorf("read thread dir: %w", err)
	}

	replyCount := 0
	lastAt := root.TimestampRaw
	for _, e := range entries {
		name := e.Name()
		if e.IsDir() || !strings.HasSuffix(name, ".md") || name == RootFilename {
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
