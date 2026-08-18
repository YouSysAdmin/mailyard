// Mailyard, Copyright (c) 2021-2026 YouSysAdmin

package audit

import "strings"

// RouteType derives an event type like "apikey.created" from the
// request method and path.
//
// Deriving rather than enumerating is deliberate. A hand-maintained
// map of every mutating route drifts the moment somebody adds an
// endpoint and forgets the audit line, and the gap is invisible - the
// trail simply has a hole in it. Derivation covers new routes the day
// they are added. The cost is that the type strings track the URL
// shape, so a rename shows up in the trail as a new type.
//
// The event also carries the raw method and path, so a type this
// function gets wrong is still traceable.
func RouteType(method, path string) string {
	segs := splitPath(path)
	// Drop the leading "api" (and "v1" on the machine surface).
	if len(segs) > 0 && segs[0] == "api" {
		segs = segs[1:]
	}

	if len(segs) > 0 && segs[0] == "v1" {
		segs = segs[1:]
	}

	if len(segs) == 0 {
		return "request." + verb(method)
	}

	resource := singular(segs[0])
	if len(segs) == 1 {
		return resource + "." + verb(method)
	}

	last := segs[len(segs)-1]

	// Trailing identifier: the action is on a specific object. When
	// the segment before it is a collection word the object is a
	// nested resource - DELETE /api/projects/:id/members/:userId
	// is project.member.deleted, not project.deleted.
	if looksLikeID(last) {
		if len(segs) >= 4 && isCollection(segs[len(segs)-2]) {
			return resource + "." + singular(segs[len(segs)-2]) + "." + verb(method)
		}

		return resource + "." + verb(method)
	}

	// Trailing collection word: creating or listing a nested
	// resource. POST /api/projects/:id/members -> member.created.
	if len(segs) >= 3 && isCollection(last) {
		return resource + "." + singular(last) + "." + verb(method)
	}

	// Anything else trailing is a named action:
	// POST /api/smtp-servers/:id/test -> smtpserver.test
	// POST /api/emails/send            -> email.send
	return resource + "." + normalize(last)
}

// isCollection reports whether a path segment names a collection
// rather than an action. Plural nouns are collections, verbs are
// actions, and none of the action words in this API end in "s".
func isCollection(s string) bool {
	return strings.HasSuffix(s, "s")
}

func splitPath(path string) []string {
	out := make([]string, 0, 6)
	for s := range strings.SplitSeq(path, "/") {
		if s != "" {
			out = append(out, s)
		}
	}

	return out
}

// verb maps an HTTP method to a past-tense action.
func verb(method string) string {
	switch method {
	case "POST":
		return "created"
	case "PUT", "PATCH":
		return "updated"
	case "DELETE":
		return "deleted"
	default:
		return strings.ToLower(method)
	}
}

// looksLikeID reports whether a segment is an identifier rather than
// a literal route word. IDs here are uuids, but numeric and opaque
// tokens are treated the same way.
func looksLikeID(s string) bool {
	if len(s) >= 32 && strings.Count(s, "-") >= 4 {
		return true // uuid
	}

	if len(s) >= 24 {
		return true // opaque token
	}

	allDigits := len(s) > 0
	for _, r := range s {
		if r < '0' || r > '9' {
			allDigits = false
			break
		}
	}

	return allDigits
}

// singular trims a trailing plural and flattens the hyphen so
// "smtp-servers" becomes "smtpserver".
func singular(s string) string {
	s = normalize(s)
	switch {
	case strings.HasSuffix(s, "ies"):
		return strings.TrimSuffix(s, "ies") + "y"
	case strings.HasSuffix(s, "sses"):
		return strings.TrimSuffix(s, "es")
	case strings.HasSuffix(s, "s") && !strings.HasSuffix(s, "ss"):
		return strings.TrimSuffix(s, "s")
	}

	return s
}

func normalize(s string) string {
	return strings.ToLower(strings.ReplaceAll(s, "-", ""))
}
