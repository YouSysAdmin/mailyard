// Mailyard, Copyright (c) 2021-2026 YouSysAdmin

package plan

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
			Path:        "/plans/",
			Tag:         "plan",
			Summary:     "List",
			Description: "Platform admin.",
			Responses:   []apidoc.Response{apidoc.OK("The result.", ListResponse{})},
		},
		{
			Method:      "POST",
			Path:        "/plans/",
			Tag:         "plan",
			Summary:     "Create",
			Description: "Platform admin.",
			Request:     upsertInput{},
			Responses:   []apidoc.Response{apidoc.Created("The result.", PlanResponse{})},
		},
		{
			Method:      "DELETE",
			Path:        "/plans/:id",
			Tag:         "plan",
			Summary:     "Delete",
			Description: "Platform admin.",
			PathParams:  []apidoc.Param{{Name: "id"}},
			Responses:   []apidoc.Response{apidoc.NoContent},
		},
		{
			Method:      "PATCH",
			Path:        "/plans/:id",
			Tag:         "plan",
			Summary:     "Update",
			Description: "Platform admin.",
			PathParams:  []apidoc.Param{{Name: "id"}},
			Request:     upsertInput{},
			Responses:   []apidoc.Response{apidoc.OK("The result.", PlanResponse{})},
		},
		{
			Method:      "PATCH",
			Path:        "/projects/:id/plan",
			Tag:         "plan",
			Summary:     "Assign",
			Description: "Platform admin.",
			PathParams:  []apidoc.Param{{Name: "id"}},
			Request:     assignInput{},
			Responses:   []apidoc.Response{apidoc.OK("The result.", AssignResponse{})},
		},
		{
			Method:      "GET",
			Path:        "/usage",
			Tag:         "plan",
			Summary:     "Usage",
			Description: "Needs the `analytics:read` permission.",

			// nil while the handler served a map. The tiles on the
			// dashboard are built from these numbers, so an unspecified
			// body was the worst place to have one.
			Responses: []apidoc.Response{apidoc.OK("The result.", UsageResponse{})},
		},
	}
}
