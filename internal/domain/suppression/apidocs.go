package suppression

import "github.com/yousysadmin/mailyard/internal/core/apidoc"

// APIDocs describes the suppression slice of the machine API.
func APIDocs() []apidoc.Route {
	return []apidoc.Route{
		{
			Method:     "GET",
			Path:       "/suppressions",
			Permission: "suppressions:read",
			Summary:    "List blocked addresses",
			Description: "Cursor paged: follow `next_cursor` until it comes back empty. " +
				"There is deliberately no total - COUNT(*) over a table that is never " +
				"pruned is a full index scan per page load, for a number nobody acts on. " +
				"`search` is prefix-anchored so the index serves it.",
			Query: []apidoc.Param{
				{Name: "kind", Enum: []string{"bounce", "complaint", "manual", "list_unsubscribe"}},
				{Name: "search", Description: "Prefix match on the address."},
				{Name: "email", Description: "One whole address, without regard to case. Every row blocking it, across lists."},
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
			Permission: "suppressions:write",
			Summary:    "Block an address",
			Description: "Without `list_id` the block is global. With it, the address is opted out " +
				"of that one unsubscribe list of this project. The same block twice is a 409.",
			Request: createInput{},
			Responses: []apidoc.Response{
				apidoc.Created("The suppression row.", CreateResponse{}),
				apidoc.BadRequest,
				apidoc.Conflict,
			},
		},
		{
			Method:     "POST",
			Path:       "/suppressions/import",
			Permission: "suppressions:write",
			Summary:    "Block a list of addresses",
			Description: "Up to a thousand per call, each entry what a single block takes. " +
				"One malformed address refuses the whole body before anything is written. " +
				"An address already blocked takes the kind and reason sent, so a list " +
				"brought over from another provider can be re-imported.",
			Request: importInput{},
			Responses: []apidoc.Response{
				apidoc.OK("How many were written.", ImportResponse{}),
				apidoc.BadRequest,
			},
		},
		{
			Method:     "DELETE",
			Path:       "/suppressions",
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
