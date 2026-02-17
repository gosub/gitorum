package forum

import (
	"bytes"

	"github.com/yuin/goldmark"
)

var mdRenderer = goldmark.New()

// renderMarkdown converts Markdown source to an HTML string.
// On error it returns the original body unmodified.
func renderMarkdown(body string) string {
	var buf bytes.Buffer
	if err := mdRenderer.Convert([]byte(body), &buf); err != nil {
		return body
	}
	return buf.String()
}
