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

	// A namespace groups unrelated resources, so its types name the
	// namespace and the action: admin.users, admin.revoke, and
	// admin.deleted or admin.updated for any object under it. The event
	// carries the path, which is what says which object.
	if namespaces[segs[0]] {
		last := segs[len(segs)-1]
		if len(segs) > 1 && !looksLikeID(last) {
			return segs[0] + "." + normalize(last)
		}

		return segs[0] + "." + verb(method)
	}

	resource := singular(segs[0])

	// Below the resource the path is read left to right. A collection
	// word names a nested resource (members, credentials), an id names
	// an object of whatever came before it, and any other word is an
	// action (send, revoke, activate). The last noun and the last action
	// decide the type:
	//
	//	POST   /projects/:id/members          project.member.created
	//	DELETE /projects/:id/invitations/:id  project.invitation.deleted
	//	PATCH  /sandbox/credentials/:id       sandbox.credential.updated
	//	POST   /templates/:id/activate/:vid   template.activate
	//	POST   /smtp-servers/:id/test         smtpserver.test
	noun, action := "", ""
	for _, s := range segs[1:] {
		switch {
		case looksLikeID(s):
		case isCollection(s):
			noun, action = singular(s), ""
		default:
			action = normalize(s)
		}
	}

	if noun != "" {
		resource += "." + noun
	}

	if action != "" {
		return resource + "." + action
	}

	return resource + "." + verb(method)
}

// namespaces are the first path segments that are not a resource.
var namespaces = map[string]bool{"admin": true, "my": true}

// isCollection reports whether a path segment names a collection
// rather than an action. Plural nouns are collections and verbs are
// actions. An action word ending in "s" starts with its verb
// (delete-contacts), which is how it is told apart.
func isCollection(s string) bool {
	return strings.HasSuffix(s, "s") && !strings.HasPrefix(s, "delete-")
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
	case strings.HasSuffix(s, "sses"), strings.HasSuffix(s, "xes"):
		return strings.TrimSuffix(s, "es")
	case strings.HasSuffix(s, "s") && !strings.HasSuffix(s, "ss"):
		return strings.TrimSuffix(s, "s")
	}

	return s
}

func normalize(s string) string {
	return strings.ToLower(strings.ReplaceAll(s, "-", ""))
}
