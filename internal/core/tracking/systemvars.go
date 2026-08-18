// Mailyard, Copyright (c) 2021-2026 YouSysAdmin

package tracking

import (
	"maps"
	"strings"
)

// The reserved template variables. The {{ mailyard_* }} namespace
// belongs to values the platform injects, and these names are a PUBLIC
// interface - they appear in templates people have already written, so
// they are the one thing here that cannot be renamed.
const (
	VarWebView      = "mailyard_web_view_url"
	VarWebViewAlias = "mailyard_mail_web_link"
	VarUnsubscribe  = "mailyard_unsubscribe_url"
)

// A system link is rendered as a PLACEHOLDER and resolved later.
//
// The two steps exist because the two facts arrive at different times. A
// campaign renders its template once and sends it to everybody, so
// rendering happens before any particular message exists - and these
// URLs identify a particular message. So rendering substitutes something
// stable, and the per-message pass swaps it for the real URL once the
// email has an id.
//
// The placeholders are ROOT-RELATIVE URLs, which is what makes them
// survive the three things that happen to markup in between:
// html/template's URL filter passes them rather than writing ZgotmplZ,
// premailer leaves them alone while inlining CSS, and the click
// rewriter only touches http(s) hrefs so it skips them.
type systemLink struct {
	// Every template name resolving to this link. More than one because
	// a name published once has to keep working.
	names []string

	placeholder string

	// Which of the caller's URLs fills it in.
	urlFrom func(Links) string
}

// Links are the per-message URLs, named rather than positional: they are
// two strings of the same type and the compiler cannot tell them apart,
// so an accidental swap would send everyone the unsubscribe link as the
// web view and nothing would fail.
type Links struct {
	WebView     string
	Unsubscribe string
}

// ONE table, and everything below is derived from it. Adding a third
// system link used to mean editing four places - the constants, the
// injection map, the has-any probe and the substitution - and missing
// any one of them fails silently: the variable renders empty, or the
// placeholder ships to a subscriber as a broken relative link.
var systemLinks = []systemLink{
	{
		names:       []string{VarWebView, VarWebViewAlias},
		placeholder: "/__mailyard_web_view__",
		urlFrom:     func(l Links) string { return l.WebView },
	},
	{
		names:       []string{VarUnsubscribe},
		placeholder: "/__mailyard_unsubscribe__",
		urlFrom:     func(l Links) string { return l.Unsubscribe },
	},
}

// WithSystemVars copies data and adds the reserved variables.
//
// Written LAST, so a caller whose own data happens to carry one of these
// names cannot shadow it. The reserved namespace has to mean the same
// thing in every template or it is not reserved.
func WithSystemVars(data map[string]any) map[string]any {
	out := make(map[string]any, len(data)+len(systemLinks))
	maps.Copy(out, data)

	for _, link := range systemLinks {
		for _, name := range link.names {
			out[name] = link.placeholder
		}
	}

	return out
}

// HasSystemSentinels reports whether any placeholder is still in s, so a
// caller can skip the substitution pass for the many templates that use
// no system variables at all.
func HasSystemSentinels(s string) bool {
	for _, link := range systemLinks {
		if strings.Contains(s, link.placeholder) {
			return true
		}
	}

	return false
}

// SubstituteSystemLinks swaps every placeholder for its real URL.
//
// An EMPTY url removes the placeholder rather than leaving it, which is
// the case that matters: a message with no unsubscribe link would
// otherwise carry `/__mailyard_unsubscribe__` as an href, and a mail
// client resolves that against nothing and shows a dead link.
func SubstituteSystemLinks(s string, links Links) string {
	if s == "" {
		return s
	}

	for _, link := range systemLinks {
		s = strings.ReplaceAll(s, link.placeholder, link.urlFrom(links))
	}

	return s
}
