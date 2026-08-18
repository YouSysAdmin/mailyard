// Mailyard, Copyright (c) 2021-2026 YouSysAdmin

package smtpserver

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
			Path:        "/shared-smtp-servers/",
			Tag:         "smtpserver",
			Summary:     "List",
			Description: "Platform admin.",
			Responses:   []apidoc.Response{apidoc.OK("The result.", SharedListResponse{})},
		},
		{
			Method:      "POST",
			Path:        "/shared-smtp-servers/",
			Tag:         "smtpserver",
			Summary:     "Create",
			Description: "Platform admin.",
			Request:     sharedCreateInput{},
			Responses:   []apidoc.Response{apidoc.Created("The result.", SharedResponse{})},
		},
		{
			Method:      "DELETE",
			Path:        "/shared-smtp-servers/:id",
			Tag:         "smtpserver",
			Summary:     "Delete",
			Description: "Platform admin.",
			PathParams:  []apidoc.Param{{Name: "id"}},
			Responses:   []apidoc.Response{apidoc.NoContent},
		},
		{
			Method:      "GET",
			Path:        "/shared-smtp-servers/:id",
			Tag:         "smtpserver",
			Summary:     "Get",
			Description: "Platform admin.",
			PathParams:  []apidoc.Param{{Name: "id"}},
			Responses:   []apidoc.Response{apidoc.OK("The result.", SharedResponse{})},
		},
		{
			Method:      "PATCH",
			Path:        "/shared-smtp-servers/:id",
			Tag:         "smtpserver",
			Summary:     "Update",
			Description: "Platform admin.",
			PathParams:  []apidoc.Param{{Name: "id"}},
			Request:     sharedUpdateInput{},
			Responses:   []apidoc.Response{apidoc.OK("The result.", SharedResponse{})},
		},
		{
			Method:      "POST",
			Path:        "/shared-smtp-servers/:id/test",
			Tag:         "smtpserver",
			Summary:     "Test",
			Description: "Platform admin.",
			PathParams:  []apidoc.Param{{Name: "id"}},
			Responses:   []apidoc.Response{apidoc.OK("The result.", SharedTestResponse{}), apidoc.OK("The result.", TestResponse{})},
		},
		{
			Method:      "GET",
			Path:        "/smtp-server-groups/",
			Tag:         "smtpserver",
			Summary:     "List",
			Description: "Needs the `smtp:read` permission.",
			Responses:   []apidoc.Response{apidoc.OK("The result.", GroupListResponse{})},
		},
		{
			Method:      "POST",
			Path:        "/smtp-server-groups/",
			Tag:         "smtpserver",
			Summary:     "Create",
			Description: "Needs the `smtp:write` permission.",
			Request:     groupCreateInput{},
			Responses:   []apidoc.Response{apidoc.Created("The result.", GroupResponse{})},
		},
		{
			Method:      "DELETE",
			Path:        "/smtp-server-groups/:id",
			Tag:         "smtpserver",
			Summary:     "Delete",
			Description: "Needs the `smtp:delete` permission.",
			PathParams:  []apidoc.Param{{Name: "id"}},
			Responses:   []apidoc.Response{apidoc.NoContent},
		},
		{
			Method:      "GET",
			Path:        "/smtp-server-groups/:id",
			Tag:         "smtpserver",
			Summary:     "Get",
			Description: "Needs the `smtp:read` permission.",
			PathParams:  []apidoc.Param{{Name: "id"}},
			Responses:   []apidoc.Response{apidoc.OK("The result.", GroupResponse{})},
		},
		{
			Method:      "PATCH",
			Path:        "/smtp-server-groups/:id",
			Tag:         "smtpserver",
			Summary:     "Update",
			Description: "Needs the `smtp:write` permission.",
			PathParams:  []apidoc.Param{{Name: "id"}},
			Request:     groupUpdateInput{},
			Responses:   []apidoc.Response{apidoc.OK("The result.", GroupResponse{})},
		},
		{
			Method:      "GET",
			Path:        "/smtp-servers/",
			Tag:         "smtpserver",
			Summary:     "List",
			Description: "Needs the `smtp:read` permission.",
			Responses:   []apidoc.Response{apidoc.OK("The result.", ListResponse{})},
		},
		{
			Method:      "POST",
			Path:        "/smtp-servers/",
			Tag:         "smtpserver",
			Summary:     "Create",
			Description: "Needs the `smtp:write` permission.",
			Request:     createInput{},
			Responses:   []apidoc.Response{apidoc.Created("The result.", ServerResponse{})},
		},
		{
			Method:      "DELETE",
			Path:        "/smtp-servers/:id",
			Tag:         "smtpserver",
			Summary:     "Delete",
			Description: "Needs the `smtp:delete` permission.",
			PathParams:  []apidoc.Param{{Name: "id"}},
			Responses:   []apidoc.Response{apidoc.NoContent},
		},
		{
			Method:      "GET",
			Path:        "/smtp-servers/:id",
			Tag:         "smtpserver",
			Summary:     "Get",
			Description: "Needs the `smtp:read` permission.",
			PathParams:  []apidoc.Param{{Name: "id"}},
			Responses:   []apidoc.Response{apidoc.OK("The result.", ServerResponse{})},
		},
		{
			Method:      "PATCH",
			Path:        "/smtp-servers/:id",
			Tag:         "smtpserver",
			Summary:     "Update",
			Description: "Needs the `smtp:write` permission.",
			PathParams:  []apidoc.Param{{Name: "id"}},
			Request:     updateInput{},
			Responses:   []apidoc.Response{apidoc.OK("The result.", ServerResponse{})},
		},
		{
			Method:      "POST",
			Path:        "/smtp-servers/:id/disable",
			Tag:         "smtpserver",
			Summary:     "Disable",
			Description: "Needs the `smtp:write` permission.",
			PathParams:  []apidoc.Param{{Name: "id"}},
			Responses:   []apidoc.Response{apidoc.OK("The server, in its new state.", ServerResponse{})},
		},
		{
			Method:      "POST",
			Path:        "/smtp-servers/:id/enable",
			Tag:         "smtpserver",
			Summary:     "Enable",
			Description: "Needs the `smtp:write` permission.",
			PathParams:  []apidoc.Param{{Name: "id"}},
			Responses:   []apidoc.Response{apidoc.OK("The server, in its new state.", ServerResponse{})},
		},
		{
			Method:      "POST",
			Path:        "/smtp-servers/:id/test",
			Tag:         "smtpserver",
			Summary:     "Test",
			Description: "Needs the `smtp:write` permission.",
			PathParams:  []apidoc.Param{{Name: "id"}},
			Responses:   []apidoc.Response{apidoc.OK("The result.", TestResponse{})},
		},
	}
}
