package forum

import (
	"io/fs"
	"strconv"
	"strings"
	"time"
)

// ThreadScan holds the data needed to build a thread-list entry.
type ThreadScan struct {
	Slug        string
	Root        *Post  // nil when the root post is hidden or missing
	Hidden      bool   // the root post is tombstoned
	ReplyCount  int    // visible replies (edits and hidden posts excluded)
	LastReplyAt string // RFC3339 time of the newest visible post
}

// ScanThread loads <category>/<slug> and summarizes it.
func ScanThread(fsys fs.FS, category, slug string, ring *KeyRing) (*ThreadScan, error) {
	t, err := LoadThread(fsys, category, slug, ring)
	if err != nil {
		return nil, err
	}
	return Summarize(t), nil
}

// Summarize builds a thread-list entry from a loaded thread.
func Summarize(t *Thread) *ThreadScan {
	s := &ThreadScan{Slug: t.Slug}
	var last time.Time
	for _, p := range t.Posts {
		if p.Filename == RootFilename {
			if p.Tombstoned {
				s.Hidden = true
				continue
			}
			s.Root = p
		} else if p.Tombstoned {
			continue
		} else {
			s.ReplyCount++
		}
		at := p.Timestamp
		if ts, ok := parseFilenameTime(p.Filename); ok {
			at = ts // millisecond precision
		}
		if at.After(last) {
			last = at
		}
	}
	if !last.IsZero() {
		s.LastReplyAt = last.UTC().Format(time.RFC3339)
	}
	return s
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
