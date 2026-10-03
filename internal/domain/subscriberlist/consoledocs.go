// Mailyard, Copyright (c) 2021-2026 YouSysAdmin

package subscriberlist

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
			Path:        "/subscriber-lists/",
			Summary:     "List",
			Description: "Needs the `subscribers:read` permission. By name, the whole list unless `limit` asks for a page.",
			Query: []apidoc.Param{
				{Name: "limit", Type: "integer", Description: "Page size, at most 200. Without it the whole list is answered."},
				{Name: "offset", Type: "integer", Description: "Rows to skip."},
			},
			Responses: []apidoc.Response{apidoc.OK("The result.", ListResponse{})},
		},
		{
			Method:      "POST",
			Path:        "/subscriber-lists/",
			Summary:     "Create",
			Description: "Needs the `subscribers:write` permission.",
			Request:     upsertInput{},
			Responses:   []apidoc.Response{apidoc.Created("The result.", ListDetailResponse{})},
		},
		{
			Method:      "DELETE",
			Path:        "/subscriber-lists/:id",
			Summary:     "Delete",
			Description: "Needs the `subscribers:delete` permission.",
			PathParams:  []apidoc.Param{{Name: "id"}},
			Responses:   []apidoc.Response{apidoc.NoContent},
		},
		{
			Method:      "GET",
			Path:        "/subscriber-lists/:id",
			Summary:     "Get",
			Description: "Needs the `subscribers:read` permission.",
			PathParams:  []apidoc.Param{{Name: "id"}},
			Responses:   []apidoc.Response{apidoc.OK("The result.", ListDetailResponse{})},
		},
		{
			Method:      "PATCH",
			Path:        "/subscriber-lists/:id",
			Summary:     "Update",
			Description: "Needs the `subscribers:write` permission.",
			PathParams:  []apidoc.Param{{Name: "id"}},
			Request:     upsertInput{},
			Responses:   []apidoc.Response{apidoc.OK("The result.", ListDetailResponse{})},
		},
		{
			Method:      "GET",
			Path:        "/subscriber-lists/:id/members",
			Summary:     "List members",
			Description: "Needs the `subscribers:read` permission.",
			PathParams:  []apidoc.Param{{Name: "id"}},
			Query:       []apidoc.Param{{Name: "limit", Type: "integer", Description: "Page size. Over-asking is clamped, never refused."}, {Name: "offset", Type: "integer", Description: "Rows to skip."}},
			Responses:   []apidoc.Response{apidoc.OK("The result.", MemberListResponse{})},
		},
		{
			Method:      "POST",
			Path:        "/subscriber-lists/:id/members",
			Summary:     "Add member",
			Description: "Needs the `subscribers:write` permission.",
			PathParams:  []apidoc.Param{{Name: "id"}},
			Request:     memberInput{},
			Responses:   []apidoc.Response{apidoc.Created("The result.", SubscriberResponse{})},
		},
		{
			Method:      "DELETE",
			Path:        "/subscriber-lists/:id/members/:subscriberId",
			Summary:     "Remove member",
			Description: "Needs the `subscribers:delete` permission.",
			PathParams:  []apidoc.Param{{Name: "id"}, {Name: "subscriberId"}},
			Responses:   []apidoc.Response{apidoc.NoContent},
		},
		{
			Method:      "GET",
			Path:        "/subscriber-lists/:id/opt-outs",
			Summary:     "Opt-outs",
			Description: "Everyone who opted out of the list, newest first, member or not - a dynamic list has opt-outs and no members. Needs the `subscribers:read` permission.",
			PathParams:  []apidoc.Param{{Name: "id"}},
			Query:       []apidoc.Param{{Name: "limit", Type: "integer"}, {Name: "offset", Type: "integer"}},
			Responses:   []apidoc.Response{apidoc.OK("The result.", OptOutListResponse{})},
		},
		{
			Method:      "POST",
			Path:        "/subscriber-lists/:id/resubscribe",
			Summary:     "Resubscribe by email",
			Description: "Needs the `subscribers:write` permission.",
			PathParams:  []apidoc.Param{{Name: "id"}},
			Request:     listEmailInput{},
			Responses:   []apidoc.Response{apidoc.OK("The result.", MembershipChange{})},
		},
		{
			Method:      "POST",
			Path:        "/subscriber-lists/:id/unsubscribe",
			Summary:     "Unsubscribe by email",
			Description: "Needs the `subscribers:write` permission.",
			PathParams:  []apidoc.Param{{Name: "id"}},
			Request:     listEmailInput{},
			Responses:   []apidoc.Response{apidoc.OK("The result.", MembershipChange{})},
		},
		{
			Method:      "POST",
			Path:        "/subscriber-lists/preview-segment",
			Summary:     "Preview segment",
			Description: "Needs the `subscribers:read` permission.",
			Request:     previewInput{},
			Responses:   []apidoc.Response{apidoc.OK("The result.", SegmentPreviewResponse{})},
		},
	}
}
