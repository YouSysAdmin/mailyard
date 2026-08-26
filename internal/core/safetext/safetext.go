// Mailyard, Copyright (c) 2021-2026 YouSysAdmin

// Package safetext bounds untrusted strings before they reach a TEXT
// column.
//
// Postgres refuses invalid UTF-8 with 22021, which nothing softens the
// way response.Internal softens a malformed uuid - so a header a caller
// controls could fail the statement that carries it. That failed a
// sign-in over a User-Agent, and let a client suppress its own audit
// trail with one bad byte. Cutting on a BYTE boundary manufactures the
// same poison out of an innocent multi-byte character, which is why the
// cap here counts runes and the cut lands between them.
package safetext

import (
	"strings"
	"unicode/utf8"
)

// MaskAddress renders an email address for a LOG LINE: the first rune
// of the local part, three stars, and the domain in full -
// j***@example.com. The domain is what an operator reading a delivery
// or bounce log actually needs (which provider is refusing us), and the
// local part is what makes the line personal data in a mail platform,
// where logs are shipped somewhere with more readers than the database
// has. Anything without an @ is masked whole.
func MaskAddress(addr string) string {
	local, domain, ok := strings.Cut(strings.TrimSpace(addr), "@")
	if !ok || local == "" {
		return "***"
	}

	first, _ := utf8.DecodeRuneInString(local)

	return string(first) + "***@" + domain
}

// MaskAddresses is MaskAddress over a list.
func MaskAddresses(addrs []string) []string {
	out := make([]string, len(addrs))
	for i, a := range addrs {
		out[i] = MaskAddress(a)
	}

	return out
}

// Clamp returns s cut to at most max runes, with any invalid UTF-8
// replaced away so the result is always storable text. The cut is on a
// rune boundary - a byte slice would split whatever character straddles
// the cap and turn a valid string into an unstorable one.
//
// IT MAY RETURN s ITSELF, and for valid text under the cap it always
// does. So a caller holding a value past the end of its request - a
// header on its way to an asynchronous write - has to clone it: this is
// not the copy that makes a fasthttp-backed string safe to keep.
func Clamp(s string, max int) string {
	// Invalid bytes go FIRST: the []rune conversion below would turn
	// each of them into U+FFFD rather than away, so cutting first
	// would keep replacement characters the caller never sent.
	if !utf8.ValidString(s) {
		s = strings.ToValidUTF8(s, "")
	}

	if utf8.RuneCountInString(s) > max {
		s = string([]rune(s)[:max])
	}

	return s
}
