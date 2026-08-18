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

// Clamp returns s cut to at most max runes, with any invalid UTF-8
// replaced away so the result is always storable text. The cut is on a
// rune boundary - a byte slice would split whatever character straddles
// the cap and turn a valid string into an unstorable one.
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
