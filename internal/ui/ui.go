// Package ui holds the built, embedded user interface.
package ui

import (
	"embed"
	"io/fs"
)

// IndexHTML is the self-contained UI document produced by tools/buildui.
//
//go:embed dist/index.html
var IndexHTML string

//go:embed dist
var dist embed.FS

// Assets is the UI as a file system rooted at the page, for the Wails asset server.
func Assets() fs.FS {
	sub, err := fs.Sub(dist, "dist")
	if err != nil {
		panic(err) // dist is embedded above, so it exists
	}
	return sub
}
