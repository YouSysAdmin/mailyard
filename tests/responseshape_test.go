// Mailyard, Copyright (c) 2021-2026 YouSysAdmin

package tests

import (
	"sort"
	"testing"

	"github.com/yousysadmin/mailyard/internal/core/apidoc"
	"github.com/yousysadmin/mailyard/internal/openapi"
)

// A 2xx says what it answers: a Go type, or a content type.
//
// `apidoc.OK("The result.", nil)` renders as "200, no body". On a route
// that answers something real the generated clients believe it: a
// byte-stream route becomes a method that json-parses an RFC 5322
// message, a typed body publishes no schema, and a redirect described
// as a 200 has a client waiting for a body that never comes.
//
// A body of nil is still correct for 204 and for a 3xx, which is why the
// rule is scoped to 2xx-with-content rather than to "every response".
func TestEveryDocumentedSuccessDeclaresItsShape(t *testing.T) {
	var bad []string
	check := func(surface string, routes []apidoc.Route) {
		for _, r := range routes {
			for _, resp := range r.Responses {
				// 204 answers nothing, by definition. 3xx hands the
				// caller somewhere else and has no body either.
				if resp.Status < 200 || resp.Status >= 300 || resp.Status == 204 {
					continue
				}

				if resp.Body == nil && !resp.IsBinary() {
					bad = append(bad, surface+" "+r.Method+" "+r.Path)
				}
			}
		}
	}
	check("api", openapi.Routes())
	check("console", openapi.ConsoleRoutes())

	sort.Strings(bad)
	for _, b := range bad {
		t.Errorf("%s declares a 2xx with no body and no content type - name the type the handler "+
			"returns, or declare the stream with apidoc.OctetStream / apidoc.EventStream", b)
	}

	// A checker that reads no routes passes, which reads exactly like a
	// document with no gaps.
	if len(openapi.Routes()) == 0 || len(openapi.ConsoleRoutes()) == 0 {
		t.Fatal("one of the two surfaces produced no routes, so this guard is checking nothing")
	}
}

// A response declared by content type must not also claim a Go body, and
// a bytes-returning method must not be generated for a stream.
//
// Both halves exist because IsBinary and IsBytes are read by three
// generators plus the document builder, and the difference between them
// is one a future edit could collapse: an event stream is declared like a
// byte stream and must not become a method that returns its bytes, or the
// method never returns.
func TestBinaryResponsesAreDeclaredConsistently(t *testing.T) {
	var checked int
	for _, r := range append(openapi.Routes(), openapi.ConsoleRoutes()...) {
		for _, resp := range r.Responses {
			if !resp.IsBinary() {
				continue
			}

			checked++

			if resp.Body != nil {
				t.Errorf("%s %s declares a content type AND a Go body - pick one",
					r.Method, r.Path)
			}

			if resp.ContentType == "text/event-stream" && resp.IsBytes() {
				t.Errorf("%s %s is an event stream being generated as a bytes method",
					r.Method, r.Path)
			}
		}
	}

	if checked == 0 {
		t.Error("found no content-type responses at all, so this guard checks nothing")
	}
}
