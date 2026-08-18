// Mailyard, Copyright (c) 2021-2026 YouSysAdmin

package sdkgen

import (
	"fmt"
	"sort"
	"strings"

	"github.com/yousysadmin/mailyard/internal/core/apidoc"
	"github.com/yousysadmin/mailyard/internal/openapi"
)

// The Python and Ruby clients, from the same openapi.Routes() the Go
// one and the OpenAPI document come from.
//
// They carry no generated types. Go gets structs because a caller
// there needs them to compile - in these two a body is a dict or a
// hash, which is what a reader of either language expects and what
// every REST client in them looks like. The schemas live in the
// OpenAPI document for anyone who wants to generate models.
//
// So what is generated is only the route surface: one method per
// route, named the same way in all three clients, with the path
// parameters as positional arguments. Everything else - auth, retries,
// errors, paging - is handwritten next to it, small enough to read.

// PythonDir and RubyDir are where each client's generated file goes.
const (
	PythonDir = "sdk/python/mailyard"
	RubyDir   = "sdk/ruby/lib/mailyard"
)

// scriptMethod is one route, reduced to what a dynamic language needs.
type scriptMethod struct {
	Name       string // snake_case
	HTTPMethod string

	// Path with {} where a parameter goes, ready for either language's
	// interpolation.
	Path       string
	PathParams []string // snake_case
	HasBody    bool

	// Raw marks a route answering bytes rather than JSON. Both script
	// clients parse every response as JSON, so an attachment fetch
	// raised a bare ValueError in Python and returned nil in Ruby -
	// losing the payload silently in the language that swallowed it.
	Raw        bool
	Summary    string
	Permission string
}

// scriptMethods is the shared route list, sorted and de-collided.
func scriptMethods() []scriptMethod {
	routes := openapi.Routes()
	sort.Slice(routes, func(i, j int) bool {
		if routes[i].Path != routes[j].Path {
			return routes[i].Path < routes[j].Path
		}

		return routes[i].Method < routes[j].Method
	})

	// The Go generator's names, lowered. Sharing them is the point: a
	// team reading one client and writing another should not have to
	// translate, and the collision rules are already settled there.
	goMethods := make([]method, 0, len(routes))
	for _, r := range routes {
		goMethods = append(goMethods, method{
			Name:       methodName(r),
			HTTPMethod: r.Method,
			route:      r,
		})
	}

	// The real rule, not a copy: two clients that disambiguate
	// differently is exactly the drift this shares names to avoid.
	(&generator{}).uniquifyMethodNames(goMethods)

	out := make([]scriptMethod, 0, len(goMethods))
	for _, gm := range goMethods {
		r := gm.route
		var segs []string
		var params []string
		for seg := range strings.SplitSeq(strings.Trim(r.Path, "/"), "/") {
			if seg == "" {
				continue
			}

			if after, ok := strings.CutPrefix(seg, ":"); ok {
				p := snake(after)
				params = append(params, p)
				segs = append(segs, "{"+p+"}")
				continue
			}

			segs = append(segs, seg)
		}

		out = append(out, scriptMethod{
			Name:       snake(gm.Name),
			HTTPMethod: r.Method,
			Path:       "/" + strings.Join(segs, "/"),
			PathParams: params,
			HasBody:    r.Request != nil,
			Raw:        answersBytes(r),
			Summary:    strings.TrimSpace(r.Summary),
			Permission: strings.TrimSpace(r.Permission),
		})
	}

	return out
}

// snake turns an exported Go name into snake_case.
func snake(s string) string {
	var b strings.Builder
	for i, r := range s {
		if r >= 'A' && r <= 'Z' {
			// Not before the first letter, and not inside a run of
			// capitals - APIKeys is api_keys, not a_p_i_keys.
			if i > 0 && !(s[i-1] >= 'A' && s[i-1] <= 'Z') {
				b.WriteByte('_')
			}

			b.WriteRune(r - 'A' + 'a')
			continue
		}

		b.WriteRune(r)
	}

	out := b.String()

	// A trailing capital run leaves api_keys unsplit at the boundary, so fix the common product words back.
	for _, fix := range [][2]string{
		{"apikeys", "api_keys"}, {"apikey", "api_key"},
		{"smtpservers", "smtp_servers"}, {"smtpserver", "smtp_server"},
		{"smtpcredentials", "smtp_credentials"}, {"smtpcredential", "smtp_credential"},
		{"smtpgroups", "smtp_groups"}, {"smtpgroup", "smtp_group"},
		{"sesfeedback", "ses_feedback"}, {"gdpr", "data"},
	} {
		out = strings.ReplaceAll(out, fix[0], fix[1])
	}

	return out
}

func pyDoc(m scriptMethod) string {
	line := m.Summary
	if line == "" {
		line = m.HTTPMethod + " " + m.Path
	}

	if m.Permission != "" {
		line += fmt.Sprintf(" Needs %s.", m.Permission)
	}

	return line
}

// answersBytes reports whether the route's success response is a byte
// stream. One reading of the metadata, shared by both script clients and
// matching the Go generator's.
func answersBytes(r apidoc.Route) bool {
	for _, resp := range r.Responses {
		if resp.Status >= 200 && resp.Status < 300 && resp.IsBytes() {
			return true
		}
	}

	return false
}
