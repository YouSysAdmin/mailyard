// Mailyard, Copyright (c) 2021-2026 YouSysAdmin

// Package docsite embeds the built documentation site so a running
// instance can serve its own docs at /docs.
//
// It lives beside the Hugo project rather than under internal/ because
// an embed directive cannot reach outside its own package directory:
// keeping the Go file here lets it embed docs/dist directly, where the
// site build already puts it (publishDir in hugo.toml), instead of
// copying the whole site into a second location just to satisfy the
// compiler.
//
// Only a .gitkeep is committed under dist/, so a plain `go build`
// with no docs build still compiles - Available() then reports false
// and the routes are not registered. That mirrors how web/dist works
// for the console.
package docsite

import (
	"embed"
	"io/fs"
)

//go:embed all:dist
var files embed.FS

// FS returns the site root (the contents of dist), or nil when no
// site was built into this binary.
func FS() fs.FS {
	sub, err := fs.Sub(files, "dist")
	if err != nil {
		return nil
	}

	if !Available() {
		return nil
	}

	return sub
}

// Available reports whether a real site was embedded. The marker is
// index.html: the .gitkeep placeholder alone is not a site.
func Available() bool {
	f, err := files.Open("dist/index.html")
	if err != nil {
		return false
	}

	_ = f.Close()

	return true
}
