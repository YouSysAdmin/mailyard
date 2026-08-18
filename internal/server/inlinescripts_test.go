// Mailyard, Copyright (c) 2021-2026 YouSysAdmin

package server

import (
	"strings"
	"testing"
	"testing/fstest"

	docsite "github.com/yousysadmin/mailyard/docs"
)

// script-src carries no 'unsafe-inline', and the thing that replaced it
// has to actually cover what the binary serves.
//
// The carve-out was there for years on the reasoning that the Vue
// bootstrap was inline. It had stopped being true - the Vite build emits
// one external script tag - and what genuinely needed it was the embedded
// documentation, which writes two inline scripts. So the policy names
// their hashes, computed from the shipped bytes.
//
// A hardcoded hash is the failure this guards: the next docs build changes
// the colour-mode probe by a byte, the browser refuses it, and the only
// symptom is a page rendering without its theme. Nothing would point at a
// constant in a Go file.
func TestScriptSrcNamesTheInlineScriptsItServes(t *testing.T) {
	got := scriptSrcFor(docsite.FS())

	if strings.Contains(got, "unsafe-inline") {
		t.Errorf("script-src carries unsafe-inline: %q", got)
	}

	if !strings.Contains(got, "'self'") {
		t.Errorf("script-src does not allow our own bundles: %q", got)
	}

	// With docs embedded there must be hashes; without them (a plain
	// go build, no `task docs`) there is nothing to hash and /docs is not
	// registered either.
	hashes := strings.Count(got, "'sha256-")
	if docsite.Available() && hashes == 0 {
		t.Error("the docs are embedded but script-src names no hash - every page of them " +
			"will render without its colour mode and nothing will say why")
	}

	if !docsite.Available() && hashes != 0 {
		t.Errorf("no docs are embedded, yet script-src names %d hash(es): %q", hashes, got)
	}

	t.Logf("script-src = %q", got)
}

// The extraction has to pick out exactly what a browser would EXECUTE.
func TestOnlyExecutableInlineScriptsAreHashed(t *testing.T) {
	site := fstest.MapFS{
		"a.html": {Data: []byte(
			`<script>one()</script>` +
				`<script type="module">two()</script>` +
				// External: covered by 'self', and hashing it would be
				// hashing an empty body.
				`<script src="/app/x.js"></script>` +
				// Data blocks. A browser never runs these, so script-src
				// does not apply - hashing them adds noise that reads
				// like protection.
				`<script type="application/ld+json">{"a":1}</script>` +
				`<script type="text/template"><p>hi</p></script>`)},
		// The same script on a second page must not produce a second
		// entry: one is byte-identical across all 94 docs pages.
		"b.html":     {Data: []byte(`<script>one()</script>`)},
		"c.txt":      {Data: []byte(`<script>ignored()</script>`)},
		"sub/d.html": {Data: []byte(`<SCRIPT TYPE="text/javascript">three()</SCRIPT>`)},
	}
	got := inlineScriptHashes(site)

	if len(got) != 3 {
		t.Fatalf("hashed %d script(s) %v, want 3 - one(), two() and three()", len(got), got)
	}

	// Sorted, so a cluster's nodes and a test run agree on one string.
	for i := 1; i < len(got); i++ {
		if got[i-1] > got[i] {
			t.Errorf("hashes are not sorted: %v", got)
		}
	}

	for _, h := range got {
		if !strings.HasPrefix(h, "'sha256-") || !strings.HasSuffix(h, "'") {
			t.Errorf("%q is not a CSP hash source expression", h)
		}
	}
}

// An empty site is not an error: a plain `go build` without `task docs`
// embeds only a placeholder, and the binary still has to boot.
func TestNoDocsMeansNoHashes(t *testing.T) {
	if got := inlineScriptHashes(fstest.MapFS{}); len(got) != 0 {
		t.Errorf("got %v from an empty site", got)
	}

	if got := scriptSrcFor(fstest.MapFS{}); got != "'self'" {
		t.Errorf("script-src = %q for an empty site, want just 'self'", got)
	}
}
