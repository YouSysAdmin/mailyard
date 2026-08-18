// Mailyard, Copyright (c) 2021-2026 YouSysAdmin

package relaynode

import "github.com/yousysadmin/mailyard/internal/core/apidoc"

// ConsoleDocs describes this domain's slice of the console API.
//
// Generated, then kept honest by TestEveryConsoleRouteIsDocumented:
// the routes come from routes.go and the shapes from the response
// types each handler actually constructs, which is readable only
// because every handler returns a declared type rather than a map.
// Edit the summaries and descriptions freely - they are the half a
// generator cannot know.
//
// The nine entries here are the console-facing routes, which both
// editions register. The node-facing five are appended by enrolDocs,
// which is empty in the community build - a document describing a route
// the binary does not serve is worse than no document, since three
// clients are generated from it.
func ConsoleDocs() []apidoc.Route {
	return append([]apidoc.Route{
		{
			Method:      "GET",
			Path:        "/my/relay-nodes/",
			Tag:         "relaynode",
			Summary:     "List mine",
			Description: "Needs the `smtp:read` permission.",
			Responses:   []apidoc.Response{apidoc.OK("The calling project's nodes.", listOutput{})},
		},
		{
			Method:      "DELETE",
			Path:        "/my/relay-nodes/:id",
			Tag:         "relaynode",
			Summary:     "Delete mine",
			Description: "Needs the `smtp:delete` permission.",
			PathParams:  []apidoc.Param{{Name: "id"}},
			Responses:   []apidoc.Response{apidoc.NoContent},
		},
		{
			Method:      "POST",
			Path:        "/my/relay-nodes/:id/approve",
			Tag:         "relaynode",
			Summary:     "Approve mine",
			Description: "Needs the `smtp:write` permission.",
			PathParams:  []apidoc.Param{{Name: "id"}},
			Responses:   []apidoc.Response{apidoc.OK("The result.", StatusResponse{})},
		},
		{
			Method:      "POST",
			Path:        "/my/relay-nodes/:id/suspend",
			Tag:         "relaynode",
			Summary:     "Suspend mine",
			Description: "Needs the `smtp:write` permission.",
			PathParams:  []apidoc.Param{{Name: "id"}},
			Responses:   []apidoc.Response{apidoc.OK("The result.", StatusResponse{})},
		},
		{
			Method:      "GET",
			Path:        "/relay-nodes/",
			Tag:         "relaynode",
			Summary:     "List",
			Description: "Platform admin.",
			Responses:   []apidoc.Response{apidoc.OK("Every enrolled node.", listOutput{})},
		},
		{
			Method:  "DELETE",
			Path:    "/relay-nodes/authority",
			Tag:     "relaynode",
			Summary: "Destroy the relay authority",
			Description: "Platform admin. The emergency lever: the private authority that " +
				"signs every node certificate is destroyed, and every node identity goes " +
				"with it.\n\n" +
				"The node identities are not an extra. Destroying the authority alone " +
				"leaves each node holding a certificate signed by a key that is gone - " +
				"still heartbeating, still listed as alive, and undeliverable-to.\n\n" +
				"A new authority is created the moment something enrols. Each node has to " +
				"enrol AGAIN, which means giving it its enrolment token: a node holding a " +
				"stored identity never re-enrols by itself. Other API nodes keep the old " +
				"authority cached until they restart.",
			Responses: []apidoc.Response{
				apidoc.OK("Destroyed.", ResetAuthorityResponse{}),
				apidoc.BadRequest,
			},
		},
		{
			Method:      "DELETE",
			Path:        "/relay-nodes/:id",
			Tag:         "relaynode",
			Summary:     "Delete",
			Description: "Platform admin.",
			PathParams:  []apidoc.Param{{Name: "id"}},
			Responses:   []apidoc.Response{apidoc.NoContent},
		},
		{
			Method:      "POST",
			Path:        "/relay-nodes/:id/approve",
			Tag:         "relaynode",
			Summary:     "Approve",
			Description: "Platform admin.",
			PathParams:  []apidoc.Param{{Name: "id"}},
			Responses:   []apidoc.Response{apidoc.OK("The result.", StatusResponse{})},
		},
		{
			Method:      "POST",
			Path:        "/relay-nodes/:id/suspend",
			Tag:         "relaynode",
			Summary:     "Suspend",
			Description: "Platform admin.",
			PathParams:  []apidoc.Param{{Name: "id"}},
			Responses:   []apidoc.Response{apidoc.OK("The result.", StatusResponse{})},
		},
	}, enrolDocs()...)
}
