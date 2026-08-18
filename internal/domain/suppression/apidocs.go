package suppression

import "github.com/yousysadmin/mailyard/internal/core/apidoc"

// APIDocs describes the suppression slice of the machine API.
func APIDocs() []apidoc.Route {
	return []apidoc.Route{
		{
			Method:     "GET",
			Path:       "/suppressions",
			Tag:        "suppressions",
			Permission: "suppressions:read",
			Summary:    "List blocked addresses",
			Description: "Cursor paged: follow `next_cursor` until it comes back empty. " +
				"There is deliberately no total - COUNT(*) over a table that is never " +
				"pruned is a full index scan per page load, for a number nobody acts on. " +
				"`search` is prefix-anchored so the index serves it.",
			Query: []apidoc.Param{
				{Name: "kind", Enum: []string{"hard", "bounce", "complaint", "manual"}},
				{Name: "search", Description: "Prefix match on the address."},
				{Name: "limit", Type: "integer"},
				{Name: "cursor", Description: "Opaque cursor from the previous page."},
			},
			Responses: []apidoc.Response{
				apidoc.OK("One page.", ListResponse{}),
				apidoc.BadRequest,
			},
		},
		{
			Method:     "POST",
			Path:       "/suppressions",
			Tag:        "suppressions",
			Permission: "suppressions:write",
			Summary:    "Block an address",
			Request:    createInput{},
			Responses: []apidoc.Response{
				apidoc.Created("The suppression row.", CreateResponse{}),
				apidoc.BadRequest,
			},
		},
		{
			Method:     "DELETE",
			Path:       "/suppressions",
			Tag:        "suppressions",
			Permission: "suppressions:delete",
			Summary:    "Unblock an address",
			Description: "The address rides in the query rather than the path: an email address in a path segment is a needless encoding problem. " +
				"SCOPED: without `list_id` this lifts the global block only, leaving any list opt-out the address has made. " +
				"Pass `list_id` to lift one list opt-out instead.",
			Query: []apidoc.Param{
				{Name: "email", Required: true},
				{Name: "list_id", Description: "Lift this list's opt-out rather than the global block."},
			},
			Responses: []apidoc.Response{apidoc.NoContent, apidoc.BadRequest, apidoc.NotFound},
		},
	}
}
