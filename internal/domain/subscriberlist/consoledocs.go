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
			Tag:         "subscriberlist",
			Summary:     "List",
			Description: "Needs the `subscribers:read` permission.",
			Responses:   []apidoc.Response{apidoc.OK("The result.", ListResponse{})},
		},
		{
			Method:      "POST",
			Path:        "/subscriber-lists/",
			Tag:         "subscriberlist",
			Summary:     "Create",
			Description: "Needs the `subscribers:write` permission.",
			Request:     upsertInput{},
			Responses:   []apidoc.Response{apidoc.Created("The result.", ListDetailResponse{})},
		},
		{
			Method:      "DELETE",
			Path:        "/subscriber-lists/:id",
			Tag:         "subscriberlist",
			Summary:     "Delete",
			Description: "Needs the `subscribers:delete` permission.",
			PathParams:  []apidoc.Param{{Name: "id"}},
			Responses:   []apidoc.Response{apidoc.NoContent},
		},
		{
			Method:      "GET",
			Path:        "/subscriber-lists/:id",
			Tag:         "subscriberlist",
			Summary:     "Get",
			Description: "Needs the `subscribers:read` permission.",
			PathParams:  []apidoc.Param{{Name: "id"}},
			// nil until the handler stopped returning a fiber.Map. The
			// sibling PATCH on this same path always declared the shape,
			// which is what made the gap visible.
			Responses: []apidoc.Response{apidoc.OK("The result.", ListDetailResponse{})},
		},
		{
			Method:      "PATCH",
			Path:        "/subscriber-lists/:id",
			Tag:         "subscriberlist",
			Summary:     "Update",
			Description: "Needs the `subscribers:write` permission.",
			PathParams:  []apidoc.Param{{Name: "id"}},
			Request:     upsertInput{},
			Responses:   []apidoc.Response{apidoc.OK("The result.", ListDetailResponse{})},
		},
		{
			Method:      "GET",
			Path:        "/subscriber-lists/:id/members",
			Tag:         "subscriberlist",
			Summary:     "List members",
			Description: "Needs the `subscribers:read` permission.",
			PathParams:  []apidoc.Param{{Name: "id"}},
			Responses:   []apidoc.Response{apidoc.OK("The result.", MemberListResponse{})},
		},
		{
			Method:      "POST",
			Path:        "/subscriber-lists/:id/members",
			Tag:         "subscriberlist",
			Summary:     "Add member",
			Description: "Needs the `subscribers:write` permission.",
			PathParams:  []apidoc.Param{{Name: "id"}},
			Request:     memberInput{},
			Responses:   []apidoc.Response{apidoc.Created("The result.", SubscriberResponse{})},
		},
		{
			Method:      "DELETE",
			Path:        "/subscriber-lists/:id/members/:subscriberId",
			Tag:         "subscriberlist",
			Summary:     "Remove member",
			Description: "Needs the `subscribers:delete` permission.",
			PathParams:  []apidoc.Param{{Name: "id"}, {Name: "subscriberId"}},
			Responses:   []apidoc.Response{apidoc.NoContent},
		},
		{
			Method:      "POST",
			Path:        "/subscriber-lists/:id/resubscribe",
			Tag:         "subscriberlist",
			Summary:     "Resubscribe by email",
			Description: "Needs the `subscribers:write` permission.",
			PathParams:  []apidoc.Param{{Name: "id"}},
			Request:     listEmailInput{},
			Responses:   []apidoc.Response{apidoc.OK("The result.", MembershipChange{})},
		},
		{
			Method:      "POST",
			Path:        "/subscriber-lists/:id/unsubscribe",
			Tag:         "subscriberlist",
			Summary:     "Unsubscribe by email",
			Description: "Needs the `subscribers:write` permission.",
			PathParams:  []apidoc.Param{{Name: "id"}},
			Request:     listEmailInput{},
			Responses:   []apidoc.Response{apidoc.OK("The result.", MembershipChange{})},
		},
		{
			Method:      "POST",
			Path:        "/subscriber-lists/preview-segment",
			Tag:         "subscriberlist",
			Summary:     "Preview segment",
			Description: "Needs the `subscribers:read` permission.",
			Request:     previewInput{},
			Responses:   []apidoc.Response{apidoc.OK("The result.", SegmentPreviewResponse{})},
		},
	}
}
