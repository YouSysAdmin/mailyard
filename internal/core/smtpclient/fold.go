// Mailyard, Copyright (c) 2021-2026 YouSysAdmin

package smtpclient

import (
	"fmt"
	"mime"
	"net/mail"
	"strings"
	"unicode/utf8"
)

// Line lengths from RFC 5322 section 2.1.1: a line SHOULD stay within
// 78 characters and MUST stay within 998, both without the CRLF.
const (
	softLineLen = 78
	hardLineLen = 998
)

// maxEncodedText is the encoded text one RFC 2047 word may carry: the
// word is capped at 75 characters and its framing takes 12.
const maxEncodedText = 75 - len("=?UTF-8?Q??=")

// writeHeader writes one header field folded at whitespace, so a long
// value becomes continuation lines rather than one line past the limit
// a receiver may refuse. Folding inserts CRLF before an existing space
// or tab, which unfolding removes again, so the value is unchanged.
func writeHeader(b *strings.Builder, name, value string) {
	line := headerSafe(name) + ": " + headerSafe(value)
	if len(line) <= softLineLen {
		b.WriteString(line)
		b.WriteString("\r\n")

		return
	}

	b.WriteString(fold(line))
	b.WriteString("\r\n")
}

// fold breaks a header line at its spaces. A fold puts CRLF in front of
// an existing space, so unfolding gives back the line exactly. A run
// with no space that would still pass the hard limit is split into
// encoded words, the one way an unbroken value can span lines - a
// reader drops the whitespace between two encoded words.
func fold(line string) string {
	var out strings.Builder
	col := 0
	for i, word := range strings.Split(line, " ") {
		pieces := []string{word}
		if len(word) > hardLineLen-1 {
			pieces = encodeLong(word)
		}

		for j, p := range pieces {
			switch {
			case i == 0 && j == 0:
				// The field name.
			case i == 1 && j == 0, col+1+len(p) <= softLineLen:
				// The first value token always stays beside the name, so
				// no line holds the name alone.
				out.WriteByte(' ')
				col++
			default:
				out.WriteString("\r\n ")
				col = 1
			}

			out.WriteString(p)
			col += len(p)
		}
	}

	return out.String()
}

// encodeLong splits one unbroken run into RFC 2047 encoded words. Each
// word holds whole characters and stays within the 75 the RFC allows.
func encodeLong(word string) []string {
	var words []string
	for len(word) > 0 {
		n, size := 0, 0
		for n < len(word) {
			_, w := utf8.DecodeRuneInString(word[n:])
			enc := len(qWord(word[n : n+w]))
			if size+enc > maxEncodedText {
				break
			}

			n += w
			size += enc
		}

		words = append(words, "=?UTF-8?Q?"+qWord(word[:n])+"?=")
		word = word[n:]
	}

	return words
}

// qWord is the Q encoding of RFC 2047 section 4.2 for one chunk.
func qWord(s string) string {
	var b strings.Builder
	for i := range len(s) {
		c := s[i]
		switch {
		case c == ' ':
			b.WriteByte('_')
		case c > 32 && c < 127 && c != '=' && c != '?' && c != '_':
			b.WriteByte(c)
		default:
			fmt.Fprintf(&b, "=%02X", c)
		}
	}

	return b.String()
}

// unstructured is a custom header value safe to write: non-ASCII text
// becomes RFC 2047 encoded words, since a raw UTF-8 byte in a header is
// not something a receiver has to accept.
func unstructured(value string) string {
	if isASCII(value) {
		return value
	}

	return mime.QEncoding.Encode("UTF-8", value)
}

// addressHeader is an address list safe to write. A list carrying a
// non-ASCII display name is rendered again through mail.Address, which
// encodes the name. One that does not parse is written as given.
func addressHeader(value string) string {
	if isASCII(value) {
		return value
	}

	list, err := mail.ParseAddressList(value)
	if err != nil {
		return value
	}

	out := make([]string, len(list))
	for i, a := range list {
		out[i] = a.String()
	}

	return strings.Join(out, ", ")
}
