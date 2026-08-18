// Mailyard, Copyright (c) 2021-2026 YouSysAdmin

package email

import "github.com/yousysadmin/mailyard/internal/core/apidoc"

// ConsoleDocs describes this domain's slice of the console API.
//
// Generated, then kept honest by TestEveryConsoleRouteIsDocumented:
// the routes come from routes.go and the shapes from the response
// types each handler actually constructs, which is readable only
// because every handler returns a declared type rather than a map.
// Edit the summaries and descriptions freely - they are the half a
// generator cannot know.
func ConsoleDocs() []apidoc.Route {
	return []apidoc.Route{
		{
			Method:      "GET",
			Path:        "/emails/",
			Tag:         "email",
			Summary:     "List",
			Description: "Needs the `emails:read` permission.",
			Responses:   []apidoc.Response{apidoc.OK("The result.", ListResponse{})},
		},
		{
			Method:      "GET",
			Path:        "/emails/:id",
			Tag:         "email",
			Summary:     "Get",
			Description: "Needs the `emails:read` permission.",
			PathParams:  []apidoc.Param{{Name: "id"}},
			Responses:   []apidoc.Response{apidoc.OK("The result.", EmailResponse{})},
		},
		{
			Method:      "GET",
			Path:        "/emails/:id/attachments/:idx",
			Tag:         "email",
			Summary:     "Attachment",
			Description: "Needs the `emails:read` permission.",
			PathParams:  []apidoc.Param{{Name: "id"}, {Name: "idx"}},
			Responses:   []apidoc.Response{apidoc.OctetStream("The decoded attachment bytes.")},
		},
		{
			Method:      "POST",
			Path:        "/emails/:id/retry",
			Tag:         "email",
			Summary:     "Retry",
			Description: "Needs the `emails:write` permission.",
			PathParams:  []apidoc.Param{{Name: "id"}},
			Responses:   []apidoc.Response{apidoc.OK("The result.", EmailResponse{})},
		},
		{
			Method:      "GET",
			Path:        "/emails/:id/status",
			Tag:         "email",
			Summary:     "Status",
			Description: "Needs the `emails:read` permission.",
			PathParams:  []apidoc.Param{{Name: "id"}},
			Responses:   []apidoc.Response{apidoc.OK("The result.", StatusResponse{})},
		},
		{
			Method:      "POST",
			Path:        "/emails/batch",
			Tag:         "email",
			Summary:     "Batch",
			Description: "Needs the `emails:write` permission.",
			Request:     batchInput{},
			Responses:   []apidoc.Response{apidoc.OK("The result.", BatchResponse{})},
		},
		{
			Method:      "GET",
			Path:        "/emails/limits",
			Tag:         "email",
			Summary:     "Limits",
			Description: "Needs the `emails:read` permission.",
			Responses:   []apidoc.Response{apidoc.OK("The result.", LimitsResponse{})},
		},
		{
			Method:      "POST",
			Path:        "/emails/preview",
			Tag:         "email",
			Summary:     "Render preview",
			Description: "Needs the `emails:read` permission.",
			Request:     renderPreviewInput{},
			Responses:   []apidoc.Response{apidoc.OK("The result.", PreviewResponse{})},
		},
		{
			Method:      "POST",
			Path:        "/emails/send",
			Tag:         "email",
			Summary:     "Send",
			Description: "Needs the `emails:write` permission.",
			Request:     sendInput{},
			Responses:   []apidoc.Response{apidoc.OK("The result.", DryRunResponse{}), apidoc.Created("The result.", SendResponse{})},
		},
		{
			Method:      "POST",
			Path:        "/emails/send-template",
			Tag:         "email",
			Summary:     "Send template",
			Description: "Needs the `emails:write` permission.",
			Request:     templateSendInput{},
			Responses:   []apidoc.Response{apidoc.Created("The result.", SendResponse{}), apidoc.OK("The result.", TemplateDryRunResponse{})},
		},
		{
			Method:      "GET",
			Path:        "/emails/stats",
			Tag:         "email",
			Summary:     "Stats",
			Description: "Needs the `emails:read` permission.",
			Responses:   []apidoc.Response{apidoc.OK("The result.", StatsResponse{})},
		},
		{
			Method:      "POST",
			Path:        "/emails/verify",
			Tag:         "email",
			Summary:     "Verify",
			Description: "Needs the `emails:read` permission.",
			Request:     verifyInput{},
			Responses:   []apidoc.Response{apidoc.OK("The result.", VerifyResponse{})},
		},
		{
			Method:      "POST",
			Path:        "/templates/:id/send-test",
			Tag:         "email",
			Summary:     "Send test",
			Description: "Needs the `templates:write` permission.",
			PathParams:  []apidoc.Param{{Name: "id"}},
			Request:     testSendInput{},
			Responses:   []apidoc.Response{apidoc.Created("The result.", SendResponse{})},
		},
	}
}
