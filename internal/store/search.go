package store

import "strings"

// SearchResult is one matching post.
type SearchResult struct {
	Category     string
	CategoryName string
	Thread       string
	ThreadTitle  string
	Filename     string
	Author       string
	Snippet      string // plain text around the first match
}

// Search returns visible posts whose title, body or author contains query
// (case-insensitive), at most limit results, in category/thread order.
// It scans every thread, which is fine for the forum sizes git can carry;
// results come from the per-HEAD cache.
func (s *Store) Search(query string, limit int) ([]SearchResult, error) {
	q := strings.ToLower(strings.TrimSpace(query))
	if q == "" {
		return nil, nil
	}
	cats, err := s.Categories()
	if err != nil {
		return nil, err
	}
	v, err := s.current()
	if err != nil {
		return nil, err
	}
	var out []SearchResult
	for _, c := range cats {
		for _, slug := range c.ThreadSlugs {
			t, err := v.thread(c.Slug, slug)
			if err != nil {
				continue
			}
			title := slug
			if t.Root != nil && !t.Root.Tombstoned {
				title = t.Root.DisplayTitle(slug)
			}
			for _, p := range t.Posts {
				if p.Tombstoned {
					continue
				}
				hay := strings.ToLower(p.Title + "\n" + p.Body)
				idx := strings.Index(hay, q)
				if idx < 0 && !strings.Contains(strings.ToLower(p.Author), q) {
					continue
				}
				out = append(out, SearchResult{
					Category:     c.Slug,
					CategoryName: c.Name,
					Thread:       slug,
					ThreadTitle:  title,
					Filename:     p.Filename,
					Author:       p.Author,
					Snippet:      snippet(p.Title+"\n"+p.Body, idx, len(q)),
				})
				if limit > 0 && len(out) >= limit {
					return out, nil
				}
			}
		}
	}
	return out, nil
}

// snippet returns about 160 characters of s around byte offset idx
// (or from the start when idx < 0), on one line.
func snippet(s string, idx, n int) string {
	const radius = 70
	start, end := 0, len(s)
	if idx >= 0 {
		start = max(0, idx-radius)
		end = min(len(s), idx+n+radius)
	} else {
		end = min(len(s), 2*radius)
	}
	// Do not cut a UTF-8 sequence in half.
	for start > 0 && start < len(s) && !utf8Start(s[start]) {
		start--
	}
	for end < len(s) && !utf8Start(s[end]) {
		end++
	}
	out := strings.Join(strings.Fields(s[start:end]), " ")
	if start > 0 {
		out = "…" + out
	}
	if end < len(s) {
		out += "…"
	}
	return out
}

func utf8Start(b byte) bool { return b&0xC0 != 0x80 }
