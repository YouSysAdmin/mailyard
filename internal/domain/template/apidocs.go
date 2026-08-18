package template

import "github.com/yousysadmin/mailyard/internal/core/apidoc"

// APIDocs describes the read-only template slice of the machine API.
// Templates are authored in the console, so the machine surface only
// reads them - which is why there is no create or update here.
func APIDocs() []apidoc.Route {
	return []apidoc.Route{
		{
			Method:     "GET",
			Path:       "/templates",
			Tag:        "templates",
			Permission: "templates:read",
			Summary:    "List templates",
			Query: []apidoc.Param{
				{Name: "limit", Type: "integer", Description: "Page size. Over-asking is clamped."},
				{Name: "offset", Type: "integer"},
			},
			Responses: []apidoc.Response{apidoc.OK("Every template in the project.", ListResponse{})},
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
