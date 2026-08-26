// Mailyard, Copyright (c) 2021-2026 YouSysAdmin

package contact

import "github.com/yousysadmin/mailyard/internal/core/apidoc"

// APIDocs describes the contact slice. No create or update by design:
// the rows are written by the delivery worker as mail finishes. Delete
// is the clean-up, one address or everything idle since a date.
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
		{
			Method:     "DELETE",
			Path:       "/contacts/:id",
			Tag:        "insight",
			Permission: "contacts:delete",
			Summary:    "Delete one contact",
			Description: "Removes the record and its tallies. The next delivery to the address " +
				"creates a fresh contact, so this forgets history and blocks nothing - " +
				"blocking is a suppression.",
			PathParams: []apidoc.Param{{Name: "id", Format: "uuid"}},
			Responses:  []apidoc.Response{apidoc.NoContent, apidoc.NotFound},
		},
		{
			Method:     "DELETE",
			Path:       "/contacts",
			Tag:        "insight",
			Permission: "contacts:delete",
			Summary:    "Delete contacts idle since a date",
			Description: "Removes every contact whose last activity - the later of its last " +
				"send and last failure - is before `inactive_before`. The cut-off is " +
				"required and may not be in the future: erasing every contact is the " +
				"data erasure endpoint's job, behind data:delete and confirm_all.",
			Query: []apidoc.Param{
				{Name: "inactive_before", Required: true, Format: "date-time"},
			},
			Responses: []apidoc.Response{apidoc.OK("How many were removed.", DeleteInactiveResponse{}), apidoc.BadRequest},
		},
	}
}
