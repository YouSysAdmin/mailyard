// Mailyard, Copyright (c) 2021-2026 YouSysAdmin

package tests

import (
	"os"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"testing"
)

// readsPeerAddress matches Fiber's own address readers.
//
// c.IP() answers the TCP peer, which behind a proxy is the proxy, and
// c.IPs() hands back the raw X-Forwarded-For list leftmost-first - which
// is the half of that header the CALLER writes. Both are the wrong answer
// to "who is calling", and clientip.From is the right one.
var readsPeerAddress = regexp.MustCompile(`\bc\.IPs?\(\)`)

// TestNothingElseAsksWhichAddressIsTheCaller keeps the answer in one
// place.
//
// It was in forty-four, and they did not agree. The address reached an api
// key's ip allowlist, a per-ip rate bucket, the audit trail and every
// access log line, and behind two proxies Fiber's reader returned the
// whole header - "203.0.113.9, 10.0.0.7" - so the allowlist refused every
// key and the rate bucket took a key the caller had chosen. Turning on
// Fiber's ip validation only changes which end is wrong: it returns the
// first VALID entry, which a caller sets by sending the header itself.
//
// So internal/core/clientip walks the header from the RIGHT, stopping at
// the first address none of our own proxies wrote, and it is the only
// place allowed to ask Fiber for a peer address at all.
func TestNothingElseAsksWhichAddressIsTheCaller(t *testing.T) {
	root := repoRoot(t)
	// The package that owns the question. The SMTP side of it needs no
	// exemption: there is no fiber context on a connection, so its
	// answer comes from proxylisten instead.
	allowed := filepath.Join("internal", "core", "clientip")

	var findings []string
	err := filepath.Walk(root, func(path string, info os.FileInfo, err error) error {
		if err != nil {
			// A path that vanished between the walk listing it and this
			// callback - the dev database under dev-data churns, and a
			// guard must not report an unrelated outage as a violation.
			if os.IsNotExist(err) {
				return nil
			}

			return err
		}

		if info.IsDir() {
			switch info.Name() {
			case "node_modules", "vendor", ".git", "dist", "sdk", "dev-data":
				return filepath.SkipDir
			}

			return nil
		}

		if !strings.HasSuffix(path, ".go") {
			return nil
		}

		rel, _ := filepath.Rel(root, path)
		if strings.HasPrefix(rel, allowed) {
			return nil
		}

		body, err := os.ReadFile(path)
		if err != nil {
			return err
		}

		for i, line := range strings.Split(string(body), "\n") {
			// Comments are where this rule gets explained, so a mention
			// is not a call.
			if strings.HasPrefix(strings.TrimSpace(line), "//") {
				continue
			}

			if readsPeerAddress.MatchString(line) {
				findings = append(findings, rel+":"+strconv.Itoa(i+1)+" "+strings.TrimSpace(line))
			}
		}

		return nil
	})
	if err != nil {
		t.Fatalf("walk: %v", err)
	}

	if len(findings) > 0 {
		t.Errorf("%d place(s) read an address straight off the request:\n  %s\n\n"+
			"Call clientip.From(c). It answers the caller's address once per request,\n"+
			"resolved right-to-left against server.trusted_proxies, and the string it\n"+
			"returns is safe to keep past the end of the request, which a string read\n"+
			"straight out of a header is not.",
			len(findings), strings.Join(findings, "\n  "))
	}
}
