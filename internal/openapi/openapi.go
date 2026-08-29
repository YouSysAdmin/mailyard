// Mailyard, Copyright (c) 2021-2026 YouSysAdmin

// Package openapi assembles the API's description from the route
// metadata each domain declares beside its handlers.
//
// Shapes are reflected from the response types rather than written by
// hand, so the document can only disagree with the code about whether an
// endpoint exists at all - which TestEveryV1RouteIsDocumented catches.
// Comparing a handwritten document's paths against the router says
// nothing about its bodies.
//
// It sits beside internal/sdkgen rather than under internal/server. The
// YAML documents are written by `mailyard export-api-spec` and read by
// sdkgen from the same metadata. MachineSpecJSON is what the embedded
// documentation's reference page fetches, from a route under the docs
// session gate - built once per process, since the types it describes
// cannot change while it runs. Both aggregate across every domain, which
// is the opposite of what internal/core is for.
package openapi

import (
	"encoding/json/v2"
	"fmt"

	"gopkg.in/yaml.v3"

	"github.com/yousysadmin/mailyard/internal/core/apidoc"
	"github.com/yousysadmin/mailyard/internal/domain/analytics"
	"github.com/yousysadmin/mailyard/internal/domain/apikey"
	"github.com/yousysadmin/mailyard/internal/domain/bounce"
	"github.com/yousysadmin/mailyard/internal/domain/certificate"
	"github.com/yousysadmin/mailyard/internal/domain/contact"
	"github.com/yousysadmin/mailyard/internal/domain/data"
	"github.com/yousysadmin/mailyard/internal/domain/email"
	"github.com/yousysadmin/mailyard/internal/domain/inbound"
	"github.com/yousysadmin/mailyard/internal/domain/subscriberlist"
	"github.com/yousysadmin/mailyard/internal/domain/suppression"
	"github.com/yousysadmin/mailyard/internal/domain/template"
	"github.com/yousysadmin/mailyard/internal/domain/unsubscribelist"
	"github.com/yousysadmin/mailyard/internal/domain/webhook"
	"github.com/yousysadmin/mailyard/pkg"
)

const description = `The Mailyard API: everything an installation can be asked to do.

Two credentials reach it. An API key (` + "`Authorization: Bearer myk_...`" + `)
is bound to one project and names it implicitly. A browser SESSION
reaches the same routes and names its project with the
` + "`X-Mailyard-Project-Id`" + ` header - the operator console is built on this
surface, which is why there is no second copy of it.

Authorization is per resource: every operation below names the
` + "`resource:action`" + ` permission it needs, from the same catalogue that
governs project members. A test compares those sentences against the
router, so this document cannot describe a rule the server does not
apply.

Operations under ` + "`/admin`" + ` are platform administration - users, plans,
identity providers, the shared SMTP pool. They take a PLATFORM
credential (` + "`mya_...`" + `) or a platform-admin session. A project key is
refused there however wide its permissions are: admin is a different
credential, not a permission.

Responses are bare keyed JSON (` + "`{\"emails\": [...]}`" + `). Errors are
` + "`{\"error\": \"message\", \"fields\": [...]}`" + ` with the HTTP status carrying
the outcome.`

// Routes is every documented endpoint of /api/v1.
//
// Two sources, and that is the point. The hand-written APIDocs beside
// each domain explain refusals and failure modes for the routes an
// integration reaches first - sending, batching, suppressing. The
// generated ConsoleDocs cover the ~150 routes that arrived when the
// product surface moved here, saying what each one is and which
// permission it needs. Hand-written wins on collision.
//
// This is the list the guard test compares against routes.go, so a
// domain missing from it fails the build rather than shipping an
// undocumented surface.
func Routes() []apidoc.Route {
	return mergeDocs(handWrittenDocs(), productDocs())
}

func handWrittenDocs() []apidoc.Route {
	var routes []apidoc.Route
	for _, set := range [][]apidoc.Route{
		email.APIDocs(),
		apikey.APIDocs(),
		certificate.APIDocs(),
		template.APIDocs(),
		suppression.APIDocs(),
		bounce.APIDocs(),
		webhook.APIDocs(),
		analytics.APIDocs(),
		unsubscribelist.APIDocs(),
		contact.APIDocs(),
		inbound.APIDocs(),
		data.APIDocs(),
		subscriberlist.APIDocs(),
	} {
		routes = append(routes, set...)
	}

	return routes
}

// MachineSpec builds the /api/v1 document as YAML.
//
// Not cached here: the document is derived from types that cannot change
// at runtime, so a caller that serves it holds one copy and there is
// nothing to keep warm. The CLI writes this YAML form, the docs route
// serves MachineSpecJSON.
func MachineSpec() ([]byte, error) {
	doc, err := machineDoc()
	if err != nil {
		return nil, err
	}

	return yaml.Marshal(doc)
}

// MachineSpecJSON is MachineSpec as JSON, which is what a browser-side
// reference viewer takes.
func MachineSpecJSON() ([]byte, error) {
	doc, err := machineDoc()
	if err != nil {
		return nil, err
	}

	return marshalJSON(doc)
}

func machineDoc() (map[string]any, error) {
	doc, err := apidoc.Build(apidoc.Info{
		Title:       "Mailyard API",
		Description: description,
		Version:     pkg.Version,
		ServerURL:   "/api/v1",
	}, Routes())
	if err != nil {
		return nil, fmt.Errorf("building the openapi document: %w", err)
	}

	return doc, nil
}

// marshalJSON renders a document deterministically, so two nodes serve
// byte-identical bytes and an etag or a diff of them means something.
// Not response.Marshal: that is the policy for API bodies, and this is a
// file.
func marshalJSON(doc map[string]any) ([]byte, error) {
	return json.Marshal(doc, json.Deterministic(true))
}
