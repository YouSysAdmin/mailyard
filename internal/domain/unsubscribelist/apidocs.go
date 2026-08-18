// Mailyard, Copyright (c) 2021-2026 YouSysAdmin

package unsubscribelist

import "github.com/yousysadmin/mailyard/internal/core/apidoc"

// APIDocs describes the read-only opt-out scopes. They are created in
// the console: a list is configuration, and a send only needs to name
// one.
func APIDocs() []apidoc.Route {
	return []apidoc.Route{
		{
			Method:      "GET",
			Path:        "/unsubscribe-lists",
			Tag:         "subscribers",
			Permission:  "suppressions:read",
			Summary:     "List transactional opt-out scopes",
			Description: "Pass a list's id as `unsubscribe_list_id` on a send and Mailyard mints the one-click link and filters against that list.",
			Responses:   []apidoc.Response{apidoc.OK("Every scope in the project.", ListResponse{})},
		},
		{
			Method:     "GET",
			Path:       "/unsubscribe-lists/:id",
			Tag:        "subscribers",
			Permission: "suppressions:read",
			Summary:    "One opt-out scope",
			PathParams: []apidoc.Param{{Name: "id", Format: "uuid"}},
			Responses:  []apidoc.Response{apidoc.OK("The scope.", GetResponse{}), apidoc.NotFound},
		},
	}
}
