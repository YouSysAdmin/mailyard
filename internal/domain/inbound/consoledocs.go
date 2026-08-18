// Mailyard, Copyright (c) 2021-2026 YouSysAdmin

package inbound

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
			Path:        "/inbound-emails/",
			Tag:         "inbound",
			Summary:     "List",
			Description: "Needs the `inbound:read` permission.",
			Responses:   []apidoc.Response{apidoc.OK("The result.", ListResponse{})},
		},
		{
			Method:      "DELETE",
			Path:        "/inbound-emails/:id",
			Tag:         "inbound",
			Summary:     "Delete",
			Description: "Needs the `inbound:delete` permission.",
			PathParams:  []apidoc.Param{{Name: "id"}},
			Responses:   []apidoc.Response{apidoc.NoContent},
		},
		{
			Method:      "GET",
			Path:        "/inbound-emails/:id",
			Tag:         "inbound",
			Summary:     "Get",
			Description: "Needs the `inbound:read` permission.",
			PathParams:  []apidoc.Param{{Name: "id"}},
			Responses:   []apidoc.Response{apidoc.OK("The result.", GetResponse{})},
		},
		{
			Method:      "GET",
			Path:        "/inbound-emails/:id/attachments/:idx",
			Tag:         "inbound",
			Summary:     "Attachment",
			Description: "Needs the `inbound:read` permission.",
			PathParams:  []apidoc.Param{{Name: "id"}, {Name: "idx"}},
			Responses:   []apidoc.Response{apidoc.OctetStream("The decoded attachment bytes.")},
		},
		{
			Method:      "GET",
			Path:        "/inbound-emails/:id/raw",
			Tag:         "inbound",
			Summary:     "Raw",
			Description: "Needs the `inbound:read` permission.",
			PathParams:  []apidoc.Param{{Name: "id"}},
			Responses:   []apidoc.Response{apidoc.OctetStream("The raw RFC 5322 message, exactly as received.")},
		},
		{
			Method:      "POST",
			Path:        "/inbound-emails/:id/retry",
			Tag:         "inbound",
			Summary:     "Retry",
			Description: "Needs the `inbound:write` permission.",
			PathParams:  []apidoc.Param{{Name: "id"}},
			Responses:   []apidoc.Response{apidoc.OK("The result.", EmittedResponse{})},
		},
		{
			Method:      "GET",
			Path:        "/inbound-emails/stats",
			Tag:         "inbound",
			Summary:     "Stats",
			Description: "Needs the `inbound:read` permission.",
			Responses:   []apidoc.Response{apidoc.OK("The result.", StatsResponse{})},
		},
	}
}
