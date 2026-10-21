package api

import (
	"github.com/gosub/gitorum/internal/forum"
)

// postToResponse converts a *forum.Post to the wire type sent to the browser.
func postToResponse(p *forum.Post) PostResponse {
	if p.Tombstoned {
		return PostResponse{
			Filename:   p.Filename,
			Body:       "[deleted]",
			BodyHTML:   "<p><em>[deleted]</em></p>",
			SigStatus:  "deleted",
			Tombstoned: true,
		}
	}
	return PostResponse{
		Author:    p.Author,
		PubKey:    p.PubKey,
		Timestamp: p.TimestampRaw,
		Title:     p.Title,
		Parent:    p.Parent,
		Hash:      p.Hash,
		Body:      p.Body,
		BodyHTML:  p.BodyHTML,
		Filename:  p.Filename,
		SigStatus: sigStatusStr(p.SigStatus),
		SigError:  p.SigError,
	}
}

func sigStatusStr(s forum.SigStatus) string {
	switch s {
	case forum.SigValid:
		return "valid"
	case forum.SigInvalid:
		return "invalid"
	case forum.SigMissing:
		return "missing"
	case forum.SigUntrusted:
		return "untrusted"
	default:
		return "unknown"
	}
}

// threadSummaryFrom builds a ThreadSummary from a lightweight ThreadScan.
func threadSummaryFrom(scan *forum.ThreadScan) ThreadSummary {
	ts := ThreadSummary{
		Slug:        scan.Slug,
		Title:       scan.Slug,
		ReplyCount:  scan.ReplyCount,
		LastReplyAt: scan.LastReplyAt,
		Hidden:      scan.Hidden,
	}
	switch {
	case scan.Hidden:
		ts.Title = "[deleted]"
	case scan.Root != nil:
		ts.Title = scan.Root.DisplayTitle(scan.Slug)
		ts.Author = scan.Root.Author
		ts.CreatedAt = scan.Root.TimestampRaw
	}
	return ts
}
