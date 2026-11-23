// Package ui embeds the built interface (Vue 3 in the system WebView). The content is local to the binary:
// the WebView never loads remote content (cliente-local.md section 1).
package ui

import (
	"embed"
	"io/fs"
)

//go:embed all:dist
var dist embed.FS

// Assets is the built interface (ui/dist, produced by `npm run build`).
func Assets() fs.FS {
	sub, err := fs.Sub(dist, "dist")
	if err != nil {
		panic(err)
	}
	return sub
}
