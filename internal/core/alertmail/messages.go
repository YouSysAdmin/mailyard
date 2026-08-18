// Mailyard, Copyright (c) 2021-2026 YouSysAdmin

package alertmail

import (
	"html"
	"strings"
)

// The alert layout, built the same way systemmail builds its own
// messages: plain string concatenation with inline styles and no
// external assets.
//
// A COPY of that approach rather than a shared helper, because the shape
// differs where it matters. Those messages are one call to action - press
// this link - and the link IS the message, so it gets a button and a
// paste-this-URL fallback. An alert is the opposite: the news is the
// text, the link is optional (an install with no public URL has none),
// and there is a detail block those have no place for.

// alertHTML renders the HTML part.
func alertHTML(heading, note string, detail []string, linkURL string) string {
	var b strings.Builder
	b.WriteString(`<!DOCTYPE html><html><head><meta charset="utf-8">`)
	b.WriteString(`<meta name="viewport" content="width=device-width,initial-scale=1"></head>`)
	b.WriteString(`<body style="margin:0;padding:24px;background:#f5f6f8;`)
	b.WriteString(`font-family:-apple-system,BlinkMacSystemFont,'Segoe UI',Roboto,Helvetica,Arial,sans-serif;color:#1f2329">`)
	b.WriteString(`<div style="max-width:520px;margin:0 auto;background:#ffffff;border-radius:8px;padding:32px">`)

	b.WriteString(`<h1 style="margin:0 0 16px;font-size:20px;font-weight:600">`)
	b.WriteString(html.EscapeString(heading))
	b.WriteString(`</h1>`)

	b.WriteString(`<p style="margin:0 0 20px;font-size:15px;line-height:1.5">`)
	b.WriteString(html.EscapeString(note))
	b.WriteString(`</p>`)

	if len(detail) > 0 {
		b.WriteString(`<div style="margin:0 0 20px;padding:12px 14px;background:#f5f6f8;`)
		b.WriteString(`border-radius:6px;font-size:13px;line-height:1.6;color:#3a4149">`)
		for i, line := range detail {
			if i > 0 {
				b.WriteString(`<br>`)
			}

			b.WriteString(html.EscapeString(line))
		}

		b.WriteString(`</div>`)
	}

	if linkURL != "" {
		b.WriteString(`<p style="margin:0 0 20px"><a href="`)
		b.WriteString(html.EscapeString(linkURL))
		b.WriteString(`" style="display:inline-block;padding:11px 20px;background:#2563eb;color:#ffffff;`)
		b.WriteString(`text-decoration:none;border-radius:6px;font-size:15px;font-weight:500">`)
		b.WriteString(`Open the log</a></p>`)
	}

	b.WriteString(`<p style="margin:0;font-size:13px;color:#5c6470;line-height:1.5">`)
	b.WriteString(`You are receiving this because you administer this installation or own this project. `)
	b.WriteString(`Repeats of the same alert are collapsed for ten minutes - the audit trail has every one.`)
	b.WriteString(`</p></div></body></html>`)

	return b.String()
}

// alertText renders the text/plain alternative. Every client can read
// this one, and it is what a forwarded copy in a ticket looks like.
func alertText(heading, note string, detail []string, linkURL string) string {
	var b strings.Builder
	b.WriteString(heading + "\n\n")
	b.WriteString(note + "\n")
	if len(detail) > 0 {
		b.WriteString("\n" + strings.Join(detail, "\n") + "\n")
	}

	if linkURL != "" {
		b.WriteString("\n" + linkURL + "\n")
	}

	return b.String()
}
