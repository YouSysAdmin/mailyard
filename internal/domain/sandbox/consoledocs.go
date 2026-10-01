// Mailyard, Copyright (c) 2021-2026 YouSysAdmin

package sandbox

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
			Path:        "/sandbox/",
			Tag:         "sandbox",
			Summary:     "List",
			Description: "Needs the `sandbox:read` permission.",
			Query: []apidoc.Param{
				{
					Name:        "inbox",
					Type:        "string",
					Format:      "uuid",
					Description: "Narrow the page to captures whose envelope sender is on this inbox's list.",
				},
				{Name: "sender", Description: "Part of the envelope sender, case-insensitive."},
				{Name: "recipient", Description: "Part of any envelope recipient, Bcc included, case-insensitive."},
				{Name: "subject", Description: "Part of the subject, case-insensitive."},
				{Name: "since", Format: "date-time", Description: "Only captures received after this RFC 3339 instant."},
				{Name: "limit", Type: "integer", Description: "Page size. Over-asking is clamped, never refused."},
				{Name: "offset", Type: "integer", Description: "Rows to skip."},
			},
			Responses: []apidoc.Response{apidoc.OK("The result.", ListResponse{})},
		},
		{
			Method:      "DELETE",
			Path:        "/sandbox/:id",
			Tag:         "sandbox",
			Summary:     "Delete",
			Description: "Needs the `sandbox:delete` permission.",
			PathParams:  []apidoc.Param{{Name: "id"}},
			Responses:   []apidoc.Response{apidoc.NoContent},
		},
		{
			Method:      "GET",
			Path:        "/sandbox/:id",
			Tag:         "sandbox",
			Summary:     "Get",
			Description: "Needs the `sandbox:read` permission.",
			PathParams:  []apidoc.Param{{Name: "id"}},
			Responses:   []apidoc.Response{apidoc.OK("The result.", EmailResponse{})},
		},
		{
			Method:      "GET",
			Path:        "/sandbox/:id/attachments/:idx",
			Tag:         "sandbox",
			Summary:     "Attachment",
			Description: "Needs the `sandbox:read` permission.",
			PathParams:  []apidoc.Param{{Name: "id"}, {Name: "idx"}},
			Responses:   []apidoc.Response{apidoc.OctetStream("The decoded attachment bytes.")},
		},
		{
			Method:      "GET",
			Path:        "/sandbox/:id/eml",
			Tag:         "sandbox",
			Summary:     "EML",
			Description: "Needs the `sandbox:read` permission.",
			PathParams:  []apidoc.Param{{Name: "id"}},
			Responses:   []apidoc.Response{apidoc.OctetStream("The captured wire bytes as an .eml download.")},
		},
		{
			Method:      "GET",
			Path:        "/sandbox/:id/raw",
			Tag:         "sandbox",
			Summary:     "Raw",
			Description: "Needs the `sandbox:read` permission.",
			PathParams:  []apidoc.Param{{Name: "id"}},
			Responses:   []apidoc.Response{apidoc.OctetStream("The captured wire bytes, exactly as submitted.")},
		},
		{
			Method:      "POST",
			Path:        "/sandbox/clear",
			Tag:         "sandbox",
			Summary:     "Clear",
			Description: "Needs the `sandbox:delete` permission. Without a body, or with nothing set, every capture in the project is removed. `senders` keeps it to captures whose envelope sender is one of those addresses - an inbox's `addresses` list, for instance - and `recipients` to captures addressed to any of those. `older_than` removes only what was received before that RFC 3339 instant. Set together, a capture must satisfy all of them.",
			Request:     clearInput{},
			Responses:   []apidoc.Response{apidoc.OK("The result.", DeletedResponse{})},
		},
		{
			Method:      "GET",
			Path:        "/sandbox/credentials",
			Tag:         "sandbox",
			Summary:     "List credentials",
			Description: "Needs the `sandbox:read` permission.",
			Responses:   []apidoc.Response{apidoc.OK("The result.", CredentialListResponse{})},
		},
		{
			Method:      "POST",
			Path:        "/sandbox/credentials",
			Tag:         "sandbox",
			Summary:     "Create credential",
			Description: "Needs the `sandbox:write` permission.",
			Request:     credentialInput{},
			Responses:   []apidoc.Response{apidoc.Created("The result.", CredentialCreatedResponse{})},
		},
		{
			Method:      "POST",
			Path:        "/sandbox/credentials/:id/revoke",
			Tag:         "sandbox",
			Summary:     "Revoke credential",
			Description: "Needs the `sandbox:write` permission.",
			PathParams:  []apidoc.Param{{Name: "id"}},
			Responses:   []apidoc.Response{apidoc.OK("The result.", CredentialResponse{})},
		},
		{
			Method:      "GET",
			Path:        "/sandbox/inboxes",
			Tag:         "sandbox",
			Summary:     "List inboxes",
			Description: "Needs the `sandbox:read` permission.",
			Responses:   []apidoc.Response{apidoc.OK("The result.", InboxListResponse{})},
		},
		{
			Method:      "POST",
			Path:        "/sandbox/inboxes",
			Tag:         "sandbox",
			Summary:     "Create inbox",
			Description: "Needs the `sandbox:write` permission.",
			Request:     inboxCreateInput{},
			Responses:   []apidoc.Response{apidoc.Created("The result.", InboxResponse{})},
		},
		{
			Method:      "DELETE",
			Path:        "/sandbox/inboxes/:id",
			Tag:         "sandbox",
			Summary:     "Delete inbox",
			Description: "Needs the `sandbox:delete` permission. No captured mail is removed.",
			PathParams:  []apidoc.Param{{Name: "id"}},
			Responses:   []apidoc.Response{apidoc.NoContent},
		},
		{
			Method:      "GET",
			Path:        "/sandbox/inboxes/:id",
			Tag:         "sandbox",
			Summary:     "Get inbox",
			Description: "Needs the `sandbox:read` permission.",
			PathParams:  []apidoc.Param{{Name: "id"}},
			Responses:   []apidoc.Response{apidoc.OK("The result.", InboxResponse{})},
		},
		{
			Method:      "PATCH",
			Path:        "/sandbox/inboxes/:id",
			Tag:         "sandbox",
			Summary:     "Update inbox",
			Description: "Needs the `sandbox:write` permission.",
			PathParams:  []apidoc.Param{{Name: "id"}},
			Request:     inboxUpdateInput{},
			Responses:   []apidoc.Response{apidoc.OK("The result.", InboxResponse{})},
		},
		{
			Method:      "GET",
			Path:        "/sandbox/info",
			Tag:         "sandbox",
			Summary:     "Info",
			Description: "Needs the `sandbox:read` permission.",
			Responses:   []apidoc.Response{apidoc.OK("The result.", SettingsResponse{})},
		},
	}
}
