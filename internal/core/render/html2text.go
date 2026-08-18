// Mailyard, Copyright (c) 2021-2026 YouSysAdmin

package render

import (
	"html"
	"regexp"
	"strings"
)

var (
	brRe     = regexp.MustCompile(`(?i)<br\s*/?>`)
	closePRe = regexp.MustCompile(`(?i)</p\s*>`)
	openPRe  = regexp.MustCompile(`(?i)<p[^>]*>`)
	liRe     = regexp.MustCompile(`(?i)<li[^>]*>`)
	linkRe   = regexp.MustCompile(`(?i)<a\s[^>]*href\s*=\s*["']([^"']*)["'][^>]*>(.*?)</a>`)
	tagRe    = regexp.MustCompile(`<[^>]*>`)
	spaceRe  = regexp.MustCompile(`[^\S\n]+`)
	nlRe     = regexp.MustCompile(`\n{3,}`)
)

// HTMLToText converts an HTML string to readable plain text: block
// elements become whitespace, list items become dashes, link URLs are
// kept in parentheses, remaining tags are stripped, and entities are
// decoded. Used to auto-generate the text alternative when a sender
// provides HTML only.
func HTMLToText(input string) string {
	if input == "" {
		return ""
	}

	s := brRe.ReplaceAllString(input, "\n")
	s = closePRe.ReplaceAllString(s, "\n\n")
	s = openPRe.ReplaceAllString(s, "\n\n")
	s = liRe.ReplaceAllString(s, "\n- ")
	s = linkRe.ReplaceAllString(s, "$2 ($1)")
	s = tagRe.ReplaceAllString(s, "")
	s = strings.ReplaceAll(s, "&nbsp;", " ")
	s = html.UnescapeString(s)
	s = spaceRe.ReplaceAllString(s, " ")
	s = nlRe.ReplaceAllString(s, "\n\n")

	return strings.TrimSpace(s)
}
