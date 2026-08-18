// Mailyard, Copyright (c) 2021-2026 YouSysAdmin

package campaign

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
			Path:        "/campaigns/",
			Tag:         "campaign",
			Summary:     "List",
			Description: "Needs the `campaigns:read` permission.",
			Responses:   []apidoc.Response{apidoc.OK("The result.", ListResponse{})},
		},
		{
			Method:      "POST",
			Path:        "/campaigns/",
			Tag:         "campaign",
			Summary:     "Create",
			Description: "Needs the `campaigns:write` permission.",
			Request:     upsertInput{},
			Responses:   []apidoc.Response{apidoc.Created("The result.", CampaignResponse{})},
		},
		{
			Method:      "DELETE",
			Path:        "/campaigns/:id",
			Tag:         "campaign",
			Summary:     "Delete",
			Description: "Needs the `campaigns:delete` permission.",
			PathParams:  []apidoc.Param{{Name: "id"}},
			Responses:   []apidoc.Response{apidoc.NoContent},
		},
		{
			Method:      "GET",
			Path:        "/campaigns/:id",
			Tag:         "campaign",
			Summary:     "Get",
			Description: "Needs the `campaigns:read` permission.",
			PathParams:  []apidoc.Param{{Name: "id"}},
			Responses:   []apidoc.Response{apidoc.OK("The result.", CampaignDetailResponse{})},
		},
		{
			Method:      "PATCH",
			Path:        "/campaigns/:id",
			Tag:         "campaign",
			Summary:     "Update",
			Description: "Needs the `campaigns:write` permission.",
			PathParams:  []apidoc.Param{{Name: "id"}},
			Request:     upsertInput{},
			Responses:   []apidoc.Response{apidoc.OK("The result.", CampaignResponse{})},
		},
		{
			Method:      "GET",
			Path:        "/campaigns/:id/analytics",
			Tag:         "campaign",
			Summary:     "Analytics",
			Description: "Needs the `campaigns:read` permission.",
			PathParams:  []apidoc.Param{{Name: "id"}},
			Responses:   []apidoc.Response{apidoc.OK("The result.", AnalyticsResponse{})},
		},
		{
			Method:      "POST",
			Path:        "/campaigns/:id/cancel",
			Tag:         "campaign",
			Summary:     "Cancel",
			Description: "Needs the `campaigns:write` permission.",
			PathParams:  []apidoc.Param{{Name: "id"}},
			Responses:   []apidoc.Response{apidoc.OK("The campaign, in its new state.", CampaignResponse{})},
		},
		{
			Method:      "POST",
			Path:        "/campaigns/:id/duplicate",
			Tag:         "campaign",
			Summary:     "Duplicate",
			Description: "Needs the `campaigns:write` permission.",
			PathParams:  []apidoc.Param{{Name: "id"}},
			Responses:   []apidoc.Response{apidoc.Created("The result.", CampaignResponse{})},
		},
		{
			Method:      "GET",
			Path:        "/campaigns/:id/messages",
			Tag:         "campaign",
			Summary:     "Messages",
			Description: "Needs the `campaigns:read` permission.",
			PathParams:  []apidoc.Param{{Name: "id"}},
			Responses:   []apidoc.Response{apidoc.OK("The result.", MessageListResponse{})},
		},
		{
			Method:      "POST",
			Path:        "/campaigns/:id/pause",
			Tag:         "campaign",
			Summary:     "Pause",
			Description: "Needs the `campaigns:write` permission.",
			PathParams:  []apidoc.Param{{Name: "id"}},
			Responses:   []apidoc.Response{apidoc.OK("The campaign, in its new state.", CampaignResponse{})},
		},
		{
			Method:      "POST",
			Path:        "/campaigns/:id/resume",
			Tag:         "campaign",
			Summary:     "Resume",
			Description: "Needs the `campaigns:write` permission.",
			PathParams:  []apidoc.Param{{Name: "id"}},
			Responses:   []apidoc.Response{apidoc.OK("The campaign, in its new state.", CampaignResponse{})},
		},
		{
			Method:      "POST",
			Path:        "/campaigns/:id/send",
			Tag:         "campaign",
			Summary:     "Send",
			Description: "Needs the `campaigns:write` permission.",
			PathParams:  []apidoc.Param{{Name: "id"}},
			Request:     sendInput{},
			Responses:   []apidoc.Response{apidoc.OK("The result.", CampaignResponse{})},
		},
	}
}
