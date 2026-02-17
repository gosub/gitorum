// Package ui exposes the embedded static web assets for the Gitorum SPA.
package ui

import (
	"embed"
	"io/fs"
)

//go:embed static
var rawFS embed.FS

// StaticFS is the embedded file tree rooted at the static/ directory.
// Serve it with http.FileServer(http.FS(ui.StaticFS)).
var StaticFS fs.FS

func init() {
	sub, err := fs.Sub(rawFS, "static")
	if err != nil {
		panic("ui: " + err.Error())
	}
	StaticFS = sub
}
