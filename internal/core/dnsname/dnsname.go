// Mailyard, Copyright (c) 2021-2026 YouSysAdmin

// Package dnsname answers "which verified name covers this one".
//
// It is here rather than in the domains store because two processes
// on opposite sides of the deployment ask it: the platform, resolving
// a recipient to the project that owns it, and a relay node, deciding
// at RCPT time whether to take a message at all. A node holds no
// database, so it is handed the names and must apply the same rule -
// and a second implementation of this particular rule is how
// evilexample.com eventually gets accepted under example.com.
package dnsname

import "strings"

// Covering lists name and its ancestors, most specific first,
// stopping at two labels.
//
// Whole labels, never a string suffix. strings.HasSuffix
// ("evilexample.com", "example.com") is true, which is the classic
// version of this bug - building the candidate list out of label
// boundaries makes it impossible by construction rather than by a
// check somebody has to remember.
//
// Stopping at two labels keeps a single-label name like "com" out of
// the list. Nobody can verify one, since verification wants a TXT
// record at the apex, so asking would only ever be a wasted lookup.
func Covering(name string) []string {
	name = strings.ToLower(strings.TrimSuffix(strings.TrimSpace(name), "."))
	if name == "" {
		return nil
	}

	labels := strings.Split(name, ".")
	out := make([]string, 0, len(labels))
	for i := 0; i+2 <= len(labels); i++ {
		out = append(out, strings.Join(labels[i:], "."))
	}

	return out
}
