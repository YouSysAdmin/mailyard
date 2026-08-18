// Mailyard, Copyright (c) 2021-2026 YouSysAdmin

package server

import (
	"crypto/sha256"
	"encoding/base64"
	"io/fs"
	"regexp"
	"sort"
	"strings"
)

// scriptTagRE finds every script element and splits its attributes from
// its body.
//
// A regex over HTML, which is wrong in general and right here: the input
// is the output of our own two builders (Vite and Hugo), and the
// shape being matched is a script element with no nesting to get lost in.
// The alternative is an HTML parser in the boot path to read one hash.
var scriptTagRE = regexp.MustCompile(`(?is)<script([^>]*)>(.*?)</script>`)

// inlineScriptHashes returns the CSP source expressions for every
// EXECUTABLE inline script in an embedded site, so `script-src` can name
// them instead of carrying 'unsafe-inline'.
//
// COMPUTED FROM THE BYTES BEING SERVED, never written down. A hardcoded
// hash is the worst version of this: the next docs build changes the theme
// script by a byte, the browser refuses it, the page renders without its
// color mode, and nothing connects that to a constant in a Go file.
// Reading the embedded filesystem cannot drift from what the binary
// actually ships.
//
// Sorted, so the policy is the same string on every node of a cluster and
// in every test run. Map iteration order would otherwise make the header
// vary for no reason.
//
// `type="application/ld+json"` and friends are SKIPPED. A script element
// whose type is not a JavaScript MIME type is a data block that the
// browser never executes, so script-src does not apply to it - hashing
// them would add noise that reads like protection.
func inlineScriptHashes(site fs.FS) []string {
	// NIL is an ordinary state, not a broken one: docsite.FS() returns
	// nil when the documentation was never built, which a plain
	// `go build` without `task docs` is meant to support - the /docs
	// route is simply not registered.
	//
	// Guarded here rather than at the call site because fs.WalkDir
	// stats the root before it can report anything, and stat on a nil
	// FS dereferences the nil interface. It panicked, and only in a
	// tree where nobody had run `task docs` - so a dev machine never saw
	// it and a fresh clone running `go test ./...` saw nothing else.
	if site == nil {
		return nil
	}

	seen := map[string]bool{}
	_ = fs.WalkDir(site, ".", func(path string, d fs.DirEntry, err error) error {
		if err != nil || d.IsDir() || !strings.HasSuffix(path, ".html") {
			// A read error here is not fatal and must not be: the site is
			// embedded, so a failure is a build problem, and the response
			// to it is a policy missing one hash rather than a process that
			// will not boot.
			return nil
		}

		body, rerr := fs.ReadFile(site, path)
		if rerr != nil {
			return nil
		}

		for _, m := range scriptTagRE.FindAllSubmatch(body, -1) {
			attrs, script := string(m[1]), m[2]
			if strings.Contains(strings.ToLower(attrs), "src=") {
				continue // external, covered by 'self'
			}

			if !executableScript(attrs) {
				continue
			}

			sum := sha256.Sum256(script)
			seen["'sha256-"+base64.StdEncoding.EncodeToString(sum[:])+"'"] = true
		}

		return nil
	})

	out := make([]string, 0, len(seen))
	for h := range seen {
		out = append(out, h)
	}

	sort.Strings(out)

	return out
}

// executableScript reports whether a script element's type means the
// browser will run it.
//
// An absent type, or one of the JavaScript MIME types, or `module`.
// Anything else - ld+json, a template, an importmap - is data the browser
// reads rather than code it executes.
func executableScript(attrs string) bool {
	lower := strings.ToLower(attrs)
	_, after, ok := strings.Cut(lower, "type=")
	if !ok {
		return true // no type means JavaScript
	}

	value := strings.TrimSpace(after)
	value = strings.TrimLeft(value, `"'`)
	if end := strings.IndexAny(value, ` "'>`); end >= 0 {
		value = value[:end]
	}

	switch value {
	case "", "module", "text/javascript", "application/javascript",
		"text/ecmascript", "application/ecmascript":
		return true
	}

	return false
}

// scriptSrcFor builds the script-src value for a site's inline scripts.
//
// 'self' for the bundles, plus a hash per inline script. No
// 'unsafe-inline': a browser ignores it once a hash is present anyway, and
// writing it would say the opposite of what this function is for.
func scriptSrcFor(site fs.FS) string {
	sources := append([]string{"'self'"}, inlineScriptHashes(site)...)

	return strings.Join(sources, " ")
}
