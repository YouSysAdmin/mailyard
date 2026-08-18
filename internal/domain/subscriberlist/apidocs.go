// Mailyard, Copyright (c) 2021-2026 YouSysAdmin

package subscriberlist

import "github.com/yousysadmin/mailyard/internal/core/apidoc"

// APIDocs describes the membership operations. Creating and segmenting
// lists stays on the console surface - what an application needs is to
// move one address in or out.
func APIDocs() []apidoc.Route {
	return []apidoc.Route{
		{
			Method:      "POST",
			Path:        "/subscriber-lists/subscribe",
			Tag:         "subscribers",
			Permission:  "subscribers:write",
			Summary:     "Add an address to a static list",
			Description: "Creates the subscriber when the address is new. Subscribing twice is not an error - it is idempotent. Dynamic lists have no explicit membership and are refused.",
			Request:     subscribeInput{},
			Responses: []apidoc.Response{
				apidoc.Created("The subscriber and the list it joined.", SubscribeResponse{}),
				apidoc.BadRequest,
				apidoc.NotFound,
			},
		},
		{
			Method:     "POST",
			Path:       "/subscriber-lists/:id/unsubscribe",
			Tag:        "subscribers",
			Permission: "subscribers:write",
			Summary:    "Remove an address from a list",
			PathParams: []apidoc.Param{{Name: "id", Format: "uuid"}},
			Request:    listEmailInput{},
			Responses: []apidoc.Response{
				apidoc.OK("Confirmation.", MembershipChange{}),
				apidoc.BadRequest,
				apidoc.NotFound,
			},
		},
		{
			Method:     "POST",
			Path:       "/subscriber-lists/:id/resubscribe",
			Tag:        "subscribers",
			Permission: "subscribers:write",
			Summary:    "Re-add an address that had opted out",
			PathParams: []apidoc.Param{{Name: "id", Format: "uuid"}},
			Request:    listEmailInput{},
			Responses: []apidoc.Response{
				apidoc.OK("Confirmation.", MembershipChange{}),
				apidoc.BadRequest,
				apidoc.NotFound,
			},
		},
	}
}
