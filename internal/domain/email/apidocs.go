// Mailyard, Copyright (c) 2021-2026 YouSysAdmin

package email

import "github.com/yousysadmin/mailyard/internal/core/apidoc"

// APIDocs describes this domain's slice of the machine API.
//
// It lives here rather than beside the route registrations because the
// request types are unexported: sendInput and its siblings are this
// package's business, and exporting them just so a central table could
// name them would widen an API to serve documentation.
//
// Shapes are reflected from those types. What is written by hand is
// only what reflection cannot know - what an endpoint is for and which
// refusals it can produce.
func APIDocs() []apidoc.Route {
	return []apidoc.Route{
		{
			Method:     "POST",
			Path:       "/emails/send",
			Tag:        "emails",
			Permission: "emails:write",
			Summary:    "Queue one email",
			Description: "Delivery is asynchronous: the call returns as soon as the " +
				"message is stored, and the returned id is what you poll. " +
				"Set `dry_run` to validate and render without storing or sending, " +
				"which answers 200 instead of 201. " +
				"A SANDBOX credential answers 201 with a different body - " +
				"`{\"sandbox_email\": {...}, \"sandboxed\": true}` - because the " +
				"message was captured rather than queued. OpenAPI carries one " +
				"schema per status, so that shape is named here rather than " +
				"listed above.",
			Request: sendInput{},
			Responses: []apidoc.Response{
				apidoc.Created("Queued.", SendResponse{}),
				apidoc.OK("A dry run: validated and rendered, nothing stored or sent.", DryRunResponse{}),
				apidoc.BadRequest,
				apidoc.OverQuota,
			},
		},
		{
			Method:     "POST",
			Path:       "/emails/send-template",
			Tag:        "emails",
			Permission: "emails:write",
			Summary:    "Render a stored template and queue the result",
			Description: "Name the template by `template_id` or `template_name`. " +
				"Attachments registered on the template are appended automatically. " +
				"A SANDBOX credential answers 201 with " +
				"`{\"sandbox_email\": {...}, \"sandboxed\": true}` instead, for the " +
				"reason given on /emails/send.",
			Request: templateSendInput{},
			Responses: []apidoc.Response{
				apidoc.Created("Queued.", SendResponse{}),
				apidoc.OK("A dry run, carrying what the template rendered to.", TemplateDryRunResponse{}),
				apidoc.BadRequest,
				apidoc.OverQuota,
			},
		},
		{
			Method:     "POST",
			Path:       "/emails/batch",
			Tag:        "emails",
			Permission: "emails:write",
			Summary:    "Queue up to 100 emails in one call",
			Description: "With a template reference every item renders it against its own " +
				"`data`. Without one each item carries its own subject and body. " +
				"One bad item does not sink the rest, which is why this answers 200 " +
				"and reports per-item outcomes.\n\n" +
				"A sandbox-flagged key is REFUSED here rather than falling through: " +
				"falling through would deliver for real on the one credential handed " +
				"out so that it could not.",
			Request: batchInput{},
			Responses: []apidoc.Response{
				apidoc.OK("Per-item outcomes.", BatchResponse{}),
				apidoc.BadRequest,
				apidoc.OverQuota,
			},
		},
		{
			Method:      "POST",
			Path:        "/emails/preview",
			Tag:         "emails",
			Permission:  "emails:read",
			Summary:     "Render a template without sending",
			Description: "Read-only despite being a POST: the body carries the data to render against.",
			Request:     renderPreviewInput{},
			Responses: []apidoc.Response{
				apidoc.OK("The rendered subject and bodies.", PreviewResponse{}),
				apidoc.BadRequest,
			},
		},
		{
			Method:     "POST",
			Path:       "/emails/verify",
			Tag:        "emails",
			Permission: "emails:read",
			Summary:    "Judge whether an address is worth sending to",
			Description: "Syntax, disposable-domain, role-account and MX checks, with this " +
				"project's own suppression and bounce history layered on top - those two " +
				"are re-read every call, so a cached verdict cannot claim an address is " +
				"fine seconds after you blocked it.\n\n" +
				"There is deliberately no SMTP mailbox probe: probing gets the sending IP " +
				"blocklisted and providers accept-then-bounce anyway, so " +
				"`mailbox_verified` is permanently false. Requires `email_verify.enabled`.",
			Request: verifyInput{},
			Query: []apidoc.Param{
				{Name: "fresh", Type: "boolean", Description: "Skip the cache and re-run the DNS checks."},
			},
			Responses: []apidoc.Response{
				apidoc.OK("The verdict.", VerifyResponse{}),
				apidoc.BadRequest,
			},
		},
		{
			Method:     "GET",
			Path:       "/emails",
			Tag:        "emails",
			Permission: "emails:read",
			Summary:    "List sent and queued emails",
			Query: []apidoc.Param{
				{Name: "status", Enum: []string{"pending", "queued", "scheduled", "processing", "sent", "failed", "suppressed"}},
				{Name: "before", Format: "date-time", Description: "Only rows created before this RFC 3339 instant."},
				{Name: "before_id", Description: "The id of the last row on the previous page. " +
					"Pass it with `before` - two messages can share a created_at, and without the id " +
					"every row tied with the last one is skipped rather than returned on the next page."},
				{Name: "search", Description: "Match a recipient address exactly, or a substring of the subject. The body is not searched."},
				{Name: "limit", Type: "integer", Description: "Page size. Over-asking is clamped, never refused."},
			},
			Responses: []apidoc.Response{
				apidoc.OK("Newest first.", ListResponse{}),
				apidoc.BadRequest,
			},
		},
		{
			Method:     "GET",
			Path:       "/emails/stats",
			Tag:        "emails",
			Permission: "emails:read",
			Summary:    "Count emails by delivery status",
			Responses:  []apidoc.Response{apidoc.OK("Counts keyed by status.", StatsResponse{})},
		},
		{
			Method:      "GET",
			Path:        "/emails/limits",
			Tag:         "emails",
			Permission:  "emails:read",
			Summary:     "What a send may carry on this installation",
			Description: "For validating client-side before paying for a round trip.",
			Responses:   []apidoc.Response{apidoc.OK("The limits.", LimitsResponse{})},
		},
		{
			Method:     "GET",
			Path:       "/emails/:id",
			Tag:        "emails",
			Permission: "emails:read",
			Summary:    "One email with its full delivery record",
			PathParams: []apidoc.Param{{Name: "id", Format: "uuid"}},
			Responses: []apidoc.Response{
				apidoc.OK("The email.", EmailResponse{}),
				apidoc.NotFound,
			},
		},
		{
			Method:      "GET",
			Path:        "/emails/:id/tracked-links",
			Tag:         "emails",
			Permission:  "emails:read",
			Summary:     "Original destinations behind the click redirects",
			Description: "Keyed by link hash. Empty when the message was not tracked.",
			PathParams:  []apidoc.Param{{Name: "id", Format: "uuid"}},
			Responses: []apidoc.Response{
				apidoc.OK("The links.", TrackedLinksResponse{}),
				apidoc.NotFound,
			},
		},
		{
			Method:      "GET",
			Path:        "/emails/:id/status",
			Tag:         "emails",
			Permission:  "emails:read",
			Summary:     "Delivery status only",
			Description: "The polling endpoint: cheaper than the full record, which carries the bodies.",
			PathParams:  []apidoc.Param{{Name: "id", Format: "uuid"}},
			Responses: []apidoc.Response{
				apidoc.OK("The status.", StatusResponse{}),
				apidoc.NotFound,
			},
		},
		{
			Method:      "POST",
			Path:        "/emails/:id/retry",
			Tag:         "emails",
			Permission:  "emails:write",
			Summary:     "Requeue a failed email",
			Description: "Only a message in a terminal failed state can be retried.",
			PathParams:  []apidoc.Param{{Name: "id", Format: "uuid"}},
			Responses: []apidoc.Response{
				apidoc.OK("The requeued email.", EmailResponse{}),
				apidoc.BadRequest,
				apidoc.NotFound,
			},
		},
	}
}
