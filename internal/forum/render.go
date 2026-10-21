package forum

import (
	"bytes"

	"github.com/yuin/goldmark"
)

var mdRenderer = goldmark.New()

// RenderMarkdown converts Markdown to HTML exactly as posts are rendered.
// Raw HTML and dangerous URLs are not passed through.
func RenderMarkdown(body string) string { return renderMarkdown(body) }

// renderMarkdown converts Markdown source to an HTML string.
// On error it returns the original body unmodified.
func renderMarkdown(body string) string {
	var buf bytes.Buffer
	if err := mdRenderer.Convert([]byte(body), &buf); err != nil {
		return body
	}
	return buf.String()
}
