// Mailyard, Copyright (c) 2021-2026 YouSysAdmin

package domains

import "github.com/yousysadmin/mailyard/internal/core/apidoc"

// ConsoleDocs describes this domain's slice of the console API.
//
// Generated, then kept honest by TestEveryConsoleRouteIsDocumented:
// the routes come from routes.go and the shapes from the response
// types each handler actually constructs, which is readable only
// because every handler returns a declared type rather than a map.
// Edit the summaries and descriptions freely - they are the half a
// generator cannot know.
func ConsoleDocs() []apidoc.Route {
	return []apidoc.Route{
		{
			Method:      "GET",
			Path:        "/domains/",
			Summary:     "List",
			Description: "Needs the `domains:read` permission. `shared` lists the domains other projects shared with this one: this project may send as them, and only their owner manages them.",
			Responses:   []apidoc.Response{apidoc.OK("The result.", ListResponse{})},
		},
		{
			Method:      "POST",
			Path:        "/domains/",
			Summary:     "Create",
			Description: "Needs the `domains:write` permission.",
			Request:     createInput{},
			// Created, not OK: the handler answers 201.
			Responses: []apidoc.Response{apidoc.Created("The result.", DetailResponse{})},
		},
		{
			Method:      "DELETE",
			Path:        "/domains/:id",
			Summary:     "Delete",
			Description: "Needs the `domains:delete` permission.",
			PathParams:  []apidoc.Param{{Name: "id"}},
			Responses:   []apidoc.Response{apidoc.NoContent},
		},
		{
			Method:      "GET",
			Path:        "/domains/:id",
			Summary:     "Get",
			Description: "Needs the `domains:read` permission.",
			PathParams:  []apidoc.Param{{Name: "id"}},
			Responses:   []apidoc.Response{apidoc.OK("The result.", DetailResponse{})},
		},
		{
			Method:      "POST",
			Path:        "/domains/:id/verify",
			Summary:     "Verify",
			Description: "Needs the `domains:write` permission. Re-checks every DNS record, and completes a pending DKIM rotation once the new record is published.",
			PathParams:  []apidoc.Param{{Name: "id"}},
			Responses:   []apidoc.Response{apidoc.OK("The result.", DetailResponse{})},
		},
		{
			Method:      "POST",
			Path:        "/domains/:id/dkim/rotate",
			Summary:     "Rotate DKIM key",
			Description: "Needs the `domains:write` permission. Mints the next signing key under the other selector. Signing stays on the current key until verify sees the new record published.",
			PathParams:  []apidoc.Param{{Name: "id"}},
			Responses:   []apidoc.Response{apidoc.OK("The domain with the new record to publish.", DetailResponse{})},
		},
		{
			Method:      "DELETE",
			Path:        "/domains/:id/dkim/rotate",
			Summary:     "Cancel DKIM rotation",
			Description: "Needs the `domains:write` permission. Discards a pending key, the current one is untouched.",
			PathParams:  []apidoc.Param{{Name: "id"}},
			Responses:   []apidoc.Response{apidoc.OK("The result.", DetailResponse{})},
		},
		{
			Method:      "GET",
			Path:        "/domains/:id/grants",
			Summary:     "List grants",
			Description: "Needs the `domains:read` permission. The projects this domain is shared with. Owner only.",
			PathParams:  []apidoc.Param{{Name: "id"}},
			Responses:   []apidoc.Response{apidoc.OK("The result.", GrantsResponse{})},
		},
		{
			Method:      "POST",
			Path:        "/domains/:id/grants",
			Summary:     "Share",
			Description: "Needs the `domains:write` permission. Offers this verified domain to the project named by its slug. Once that project accepts, it may send as the domain and its subdomains, through its own servers, signed with this domain's DKIM key. Inbound mail, DNS records and the key stay with the owner. The grant is `pending` until accepted and covers no sending before. Sharing again is a no-op.",
			PathParams:  []apidoc.Param{{Name: "id"}},
			Request:     grantInput{},
			Responses:   []apidoc.Response{apidoc.OK("Every project the domain is now shared with.", GrantsResponse{})},
		},
		{
			Method:      "DELETE",
			Path:        "/domains/:id/grants/:project_id",
			Summary:     "Unshare",
			Description: "Needs the `domains:write` permission. The project can no longer send as this domain, whether it had accepted or not.",
			PathParams:  []apidoc.Param{{Name: "id"}, {Name: "project_id"}},
			Responses:   []apidoc.Response{apidoc.NoContent},
		},
		{
			Method:      "POST",
			Path:        "/domains/:id/shares/accept",
			Summary:     "Accept a shared domain",
			Description: "Needs the `domains:write` permission. Accepts a domain another project offered to this one, named by the owner's domain id as `shared` on the domain list carries it. This project may send as the domain from then on. Accepting again changes nothing. `404` when no project offered this domain to the caller.",
			PathParams:  []apidoc.Param{{Name: "id"}},
			Responses:   []apidoc.Response{apidoc.OK("The share, now accepted.", ShareResponse{})},
		},
		{
			Method:      "DELETE",
			Path:        "/domains/:id/shares",
			Summary:     "Decline or leave a shared domain",
			Description: "Needs the `domains:write` permission. Declines an offer, or gives up a domain this project had accepted. Mail from that domain is refused from then on. `404` when no project offered this domain to the caller.",
			PathParams:  []apidoc.Param{{Name: "id"}},
			Responses:   []apidoc.Response{apidoc.NoContent},
		},
	}
}
