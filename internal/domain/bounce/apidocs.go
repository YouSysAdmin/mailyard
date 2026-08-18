package bounce

import "github.com/yousysadmin/mailyard/internal/core/apidoc"

// APIDocs describes the bounce slice of the machine API.
func APIDocs() []apidoc.Route {
	return []apidoc.Route{
		{
			Method:      "GET",
			Path:        "/bounces",
			Tag:         "bounces",
			Permission:  "bounces:read",
			Summary:     "List bounce reports",
			Description: "Cursor paged, newest first. See the suppression list for why there is no total.",
			Query: []apidoc.Param{
				{Name: "type", Enum: []string{"hard", "soft", "complaint"}},
				{Name: "search", Description: "Prefix match on the recipient."},
				{Name: "limit", Type: "integer"},
				{Name: "cursor"},
			},
			Responses: []apidoc.Response{apidoc.OK("One page.", ListResponse{}), apidoc.BadRequest},
		},
		{
			Method:     "DELETE",
			Path:       "/bounces",
			Tag:        "bounces",
			Permission: "bounces:delete",
			Summary:    "Delete the bounce reports for an address",
			Description: "Removes every report recorded for one recipient, which is what a " +
				"mailbox that did not exist yet leaves behind once it does. This clears " +
				"the HISTORY only: what stops delivery to a bounced address is the " +
				"suppression the report created, and that is removed with " +
				"`DELETE /suppressions`. Do both to put an address back into circulation.",
			Query: []apidoc.Param{
				{Name: "email", Required: true, Description: "The recipient address."},
			},
			Responses: []apidoc.Response{
				apidoc.OK("How many reports were deleted.", DeleteResponse{}),
				apidoc.BadRequest,
				apidoc.NotFound,
			},
		},
		{
			Method:     "POST",
			Path:       "/webhooks/bounce",
			Tag:        "bounces",
			Permission: "bounces:write",
			Summary:    "Ingest a bounce report",
			Description: "For a feedback loop you run yourself. The report is filed against " +
				"the project the API key belongs to, and is taken at face value: " +
				"`email_id` is optional and is not checked against a real message, " +
				"and `type` defaults to `hard`, which also suppresses the address.\n\n" +
				"That trust is what separates this route from the return-path and SES " +
				"channels, which are reachable by strangers and therefore refuse a " +
				"report they cannot attribute to a message this project actually sent.",
			Request: ingestInput{},
			Responses: []apidoc.Response{
				apidoc.Created("Recorded, and whether it also suppressed the address.", IngestResponse{}),
				apidoc.BadRequest,
			},
		},
	}
}
