// Mailyard, Copyright (c) 2021-2026 YouSysAdmin

// Package web embeds the built Vue console so a running instance can
// serve it at /app.
//
// Only a .gitkeep is committed under dist/, so a plain `go build`
// with no `npm run build` still compiles - Available() then reports
// false and the console routes are not registered. Same arrangement
// as docs/dist.
//
// It has to be .gitkeep and not dist/index.html. Committing the shell
// while its hashed asset bundles stay gitignored gives a bare `go build`
// a binary whose /app serves a page pointing at JavaScript that is not
// in it. The console comes up blank with nothing in the log, which reads
// as a bug in the app rather than a missing build step.
package web

import (
	"embed"
	"io/fs"
)

// Frontend is the built console, compiled into the binary.
//
// The embed pattern needs at least one match or the build fails with
// "pattern all:dist: no matching files found", which is why
// dist/.gitkeep is committed and why both `npm run build` and
// `task web` put it back - they empty the directory first. A `git add
// -A` after a frontend build stages its removal, and the next clean
// clone then does not compile at all.
//
//go:embed all:dist
var Frontend embed.FS

// FS returns the console root (the contents of dist), or nil when no
// console was built into this binary.
func FS() fs.FS {
	if !Available() {
		return nil
	}

	sub, err := fs.Sub(Frontend, "dist")
	if err != nil {
		return nil
	}

	return sub
}

// Available reports whether a real build was embedded. The marker is
// index.html: the .gitkeep placeholder alone is not a console.
func Available() bool {
	f, err := Frontend.Open("dist/index.html")
	if err != nil {
		return false
	}

	_ = f.Close()

	return true
}
