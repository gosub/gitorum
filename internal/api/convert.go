package api

import (
	"strings"

	"github.com/gosub/gitorum/internal/forum"
)

// postToResponse converts a *forum.Post to the wire type sent to the browser.
func postToResponse(p *forum.Post) PostResponse {
	return PostResponse{
		Author:    p.Author,
		PubKey:    p.PubKey,
		Timestamp: p.TimestampRaw,
		Parent:    p.Parent,
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
	default:
		return "unknown"
	}
}

// threadSummaryFrom builds a ThreadSummary from a lightweight ThreadScan.
func threadSummaryFrom(scan *forum.ThreadScan) ThreadSummary {
	title := scan.Slug
	author := ""
	createdAt := ""

	if scan.Root != nil {
		author = scan.Root.Author
		createdAt = scan.Root.TimestampRaw
		// Title: first non-empty, non-heading-marker line of the body.
		for _, line := range strings.Split(scan.Root.Body, "\n") {
			line = strings.TrimSpace(strings.TrimLeft(line, "#"))
			if line != "" {
				title = line
				break
			}
		}
		if len([]rune(title)) > 100 {
			title = string([]rune(title)[:100]) + "…"
		}
	}

	return ThreadSummary{
		Slug:        scan.Slug,
		Title:       title,
		Author:      author,
		ReplyCount:  scan.ReplyCount,
		CreatedAt:   createdAt,
		LastReplyAt: scan.LastReplyAt,
	}
}
