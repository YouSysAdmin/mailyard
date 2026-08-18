// Mailyard, Copyright (c) 2021-2026 YouSysAdmin

package contact

import "github.com/yousysadmin/mailyard/internal/core/apidoc"

// APIDocs describes the contact slice. Read-only by design: the rows
// are written by the delivery worker as mail finishes, so there is no
// create or update to expose.
func APIDocs() []apidoc.Route {
	return []apidoc.Route{
		{
			Method:     "GET",
			Path:       "/contacts",
			Tag:        "insight",
			Permission: "contacts:read",
			Summary:    "Addresses this project has delivered to",
			Description: "Offset paged with a total, unlike the message logs: this list is " +
				"bounded by how many people the project has mailed. `suppressed` is " +
				"resolved from the suppression list on read, never stored, so it cannot " +
				"drift from the list that governs sending.",
			Query: []apidoc.Param{
				{Name: "search"},
				{Name: "limit", Type: "integer"},
				{Name: "offset", Type: "integer"},
			},
			Responses: []apidoc.Response{apidoc.OK("One page.", ListResponse{})},
		},
		{
			Method:     "GET",
			Path:       "/contacts/:id",
			Tag:        "insight",
			Permission: "contacts:read",
			Summary:    "One contact",
			PathParams: []apidoc.Param{{Name: "id", Format: "uuid"}},
			Responses:  []apidoc.Response{apidoc.OK("The contact.", GetResponse{}), apidoc.NotFound},
		},
	}
}
