package template

import "github.com/yousysadmin/mailyard/internal/core/apidoc"

// APIDocs describes the read-only template slice of the machine API.
// Templates are authored in the console, so the machine surface only
// reads them - which is why there is no create or update here.
func APIDocs() []apidoc.Route {
	return []apidoc.Route{
		{
			Method:      "GET",
			Path:        "/templates",
			Tag:         "templates",
			Permission:  "templates:read",
			Summary:     "List templates",
			Description: "By name. The whole list unless `limit` asks for a page, and `total` counts what `q` matches either way.",
			Query: []apidoc.Param{
				{Name: "q", Description: "Part of the name, case-insensitive."},
				{Name: "limit", Type: "integer", Description: "Page size, at most 200. Without it the whole list is answered."},
				{Name: "offset", Type: "integer", Description: "Rows to skip."},
			},
			Responses: []apidoc.Response{apidoc.OK("Every template in the project, or one page of them.", ListResponse{})},
		},
		{
			Method:      "GET",
			Path:        "/templates/:id",
			Tag:         "templates",
			Permission:  "templates:read",
			Summary:     "One template with its version history",
			Description: "The versions come along so a caller can pick one without a second call.",
			PathParams:  []apidoc.Param{{Name: "id", Format: "uuid"}},
			Responses: []apidoc.Response{
				apidoc.OK("The template.", GetResponse{}),
				apidoc.NotFound,
			},
		},
	}
}
