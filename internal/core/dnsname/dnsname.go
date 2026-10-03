// Mailyard, Copyright (c) 2021-2026 YouSysAdmin

// Package dnsname answers "which verified name covers this one", and
// whether a string is a host name at all.
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

// Valid reports whether name is a DNS host name: letters, digits and
// hyphens in labels of 1 to 63, no label opening or closing with a
// hyphen, 253 characters in all, and a last label that is not all
// digits, which is what keeps an IPv4 literal out. A trailing dot is
// accepted. A wildcard is not - a caller that allows one strips it
// first.
func Valid(name string) bool {
	name = strings.TrimSuffix(name, ".")
	if name == "" || len(name) > 253 {
		return false
	}

	for label := range strings.SplitSeq(name, ".") {
		if len(label) == 0 || len(label) > 63 {
			return false
		}

		if label[0] == '-' || label[len(label)-1] == '-' {
			return false
		}

		for i := 0; i < len(label); i++ {
			ch := label[i]
			ok := ch >= 'a' && ch <= 'z' || ch >= 'A' && ch <= 'Z' || ch >= '0' && ch <= '9' || ch == '-'
			if !ok {
				return false
			}
		}
	}

	last := name[strings.LastIndexByte(name, '.')+1:]

	return strings.Trim(last, "0123456789") != ""
}
