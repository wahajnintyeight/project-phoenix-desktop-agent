// Package ui holds the built, embedded user interface.
package ui

import _ "embed"

// IndexHTML is the self-contained UI document produced by tools/buildui.
//
//go:embed dist/index.html
var IndexHTML string
