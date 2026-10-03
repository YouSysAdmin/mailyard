// Mailyard, Copyright (c) 2021-2026 YouSysAdmin

package template

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
			Path:        "/templates/",
			Summary:     "List",
			Description: "Needs the `templates:read` permission. By name, the whole list unless `limit` asks for a page.",
			Query: []apidoc.Param{
				{Name: "q", Description: "Part of the name, case-insensitive."},
				{Name: "limit", Type: "integer", Description: "Page size, at most 200. Without it the whole list is answered."},
				{Name: "offset", Type: "integer", Description: "Rows to skip."},
			},
			Responses: []apidoc.Response{apidoc.OK("The result.", ListResponse{})},
		},
		{
			Method:      "POST",
			Path:        "/templates/",
			Summary:     "Create",
			Description: "Needs the `templates:write` permission.",
			Request:     createInput{},
			Responses:   []apidoc.Response{apidoc.Created("The result.", TemplateResponse{})},
		},
		{
			Method:      "GET",
			Path:        "/template-assets/",
			Summary:     "List builder images",
			Description: "Needs the `templates:read` permission. Newest first, the whole list unless `limit` asks for a page.",
			Query: []apidoc.Param{
				{Name: "limit", Type: "integer", Description: "Page size, at most 200. Without it the whole list is answered."},
				{Name: "offset", Type: "integer", Description: "Rows to skip."},
			},
			Responses: []apidoc.Response{apidoc.OK("The images, each with the absolute URL a template references.", AssetListResponse{})},
		},
		{
			Method:  "POST",
			Path:    "/template-assets/",
			Summary: "Upload builder image",
			Description: "Needs the `templates:write` permission. PNG, JPEG, GIF or WebP, decided from the bytes, up to " +
				"`sending.max_attachment_size`. Refused while `server.public_url` is unset. The same bytes uploaded " +
				"again answer the image already stored with 200.",
			Request: assetInput{},
			Responses: []apidoc.Response{
				apidoc.Created("The stored image.", AssetResponse{}),
				apidoc.OK("The project already holds these bytes.", AssetResponse{}),
				apidoc.BadRequest,
			},
		},
		{
			Method:  "DELETE",
			Path:    "/template-assets/:id",
			Summary: "Delete builder image",
			Description: "Needs the `templates:delete` permission. Refused with 409 while a template localization references " +
				"the image. Mail already delivered stops showing it.",
			PathParams: []apidoc.Param{{Name: "id"}},
			Responses:  []apidoc.Response{apidoc.NoContent, apidoc.NotFound, apidoc.Conflict},
		},
		{
			Method:      "DELETE",
			Path:        "/templates/:id",
			Summary:     "Delete",
			Description: "Needs the `templates:delete` permission. Refused with 409 while a campaign that is not sent or cancelled renders the template.",
			PathParams:  []apidoc.Param{{Name: "id"}},
			Responses:   []apidoc.Response{apidoc.NoContent, apidoc.NotFound, apidoc.Conflict},
		},
		{
			Method:      "GET",
			Path:        "/templates/:id",
			Summary:     "Get",
			Description: "Needs the `templates:read` permission.",
			PathParams:  []apidoc.Param{{Name: "id"}},
			Responses:   []apidoc.Response{apidoc.OK("The result.", GetResponse{})},
		},
		{
			Method:      "PATCH",
			Path:        "/templates/:id",
			Summary:     "Update",
			Description: "Needs the `templates:write` permission. Writes only the fields the body names.",
			PathParams:  []apidoc.Param{{Name: "id"}},
			Request:     updateInput{},
			Responses:   []apidoc.Response{apidoc.OK("The result.", TemplateResponse{})},
		},
		{
			Method:      "POST",
			Path:        "/templates/:id/activate/:versionId",
			Summary:     "Activate",
			Description: "Needs the `templates:write` permission.",
			PathParams:  []apidoc.Param{{Name: "id"}, {Name: "versionId"}},
			Responses:   []apidoc.Response{apidoc.OK("The result.", ActiveVersionResponse{})},
		},
		{
			Method:      "GET",
			Path:        "/templates/:id/attachments",
			Summary:     "List attachments",
			Description: "Needs the `templates:read` permission.",
			PathParams:  []apidoc.Param{{Name: "id"}},
			Responses:   []apidoc.Response{apidoc.OK("The result.", AttachmentListResponse{})},
		},
		{
			Method:      "POST",
			Path:        "/templates/:id/attachments",
			Summary:     "Upload attachment",
			Description: "Needs the `templates:write` permission.",
			PathParams:  []apidoc.Param{{Name: "id"}},
			Request:     attachmentInput{},
			Responses:   []apidoc.Response{apidoc.Created("The result.", AttachmentResponse{})},
		},
		{
			Method:      "DELETE",
			Path:        "/templates/:id/attachments/:attId",
			Summary:     "Delete attachment",
			Description: "Needs the `templates:delete` permission.",
			PathParams:  []apidoc.Param{{Name: "id"}, {Name: "attId"}},
			Responses:   []apidoc.Response{apidoc.NoContent},
		},
		{
			Method:      "GET",
			Path:        "/templates/:id/attachments/:attId/download",
			Summary:     "Download attachment",
			Description: "Needs the `templates:read` permission.",
			PathParams:  []apidoc.Param{{Name: "id"}, {Name: "attId"}},
			Responses:   []apidoc.Response{apidoc.OctetStream("The decoded attachment bytes.")},
		},
		{
			Method:      "GET",
			Path:        "/templates/:id/export",
			Summary:     "Export",
			Description: "Needs the `templates:read` permission.",
			PathParams:  []apidoc.Param{{Name: "id"}},
			Responses:   []apidoc.Response{apidoc.OK("The result.", ExportResponse{})},
		},
		{
			Method:      "DELETE",
			Path:        "/templates/:id/localizations/:localizationId",
			Summary:     "Delete localization",
			Description: "Needs the `templates:delete` permission. The last localization of the active version is refused with 409.",
			PathParams:  []apidoc.Param{{Name: "id"}, {Name: "localizationId"}},
			Responses:   []apidoc.Response{apidoc.NoContent, apidoc.NotFound, apidoc.Conflict},
		},
		{
			Method:      "GET",
			Path:        "/templates/:id/versions",
			Summary:     "List versions",
			Description: "Needs the `templates:read` permission.",
			PathParams:  []apidoc.Param{{Name: "id"}},
			Responses:   []apidoc.Response{apidoc.OK("The result.", VersionListResponse{})},
		},
		{
			Method:      "POST",
			Path:        "/templates/:id/versions",
			Summary:     "Create version",
			Description: "Needs the `templates:write` permission.",
			PathParams:  []apidoc.Param{{Name: "id"}},
			Request:     versionInput{},
			Responses:   []apidoc.Response{apidoc.Created("The result.", VersionResponse{})},
		},
		{
			Method:      "DELETE",
			Path:        "/templates/:id/versions/:versionId",
			Summary:     "Delete version",
			Description: "Needs the `templates:delete` permission. The active version is refused with 409.",
			PathParams:  []apidoc.Param{{Name: "id"}, {Name: "versionId"}},
			Responses:   []apidoc.Response{apidoc.NoContent, apidoc.NotFound, apidoc.Conflict},
		},
		{
			Method:      "PATCH",
			Path:        "/templates/:id/versions/:versionId",
			Summary:     "Update version",
			Description: "Needs the `templates:write` permission.",
			PathParams:  []apidoc.Param{{Name: "id"}, {Name: "versionId"}},
			Request:     versionInput{},
			Responses:   []apidoc.Response{apidoc.OK("The result.", VersionResponse{})},
		},
		{
			Method:      "GET",
			Path:        "/templates/:id/versions/:versionId/localizations",
			Summary:     "List localizations",
			Description: "Needs the `templates:read` permission.",
			PathParams:  []apidoc.Param{{Name: "id"}, {Name: "versionId"}},
			Responses:   []apidoc.Response{apidoc.OK("The result.", LocalizationListResponse{})},
		},
		{
			Method:      "PUT",
			Path:        "/templates/:id/versions/:versionId/localizations",
			Summary:     "Put localization",
			Description: "Needs the `templates:write` permission.",
			PathParams:  []apidoc.Param{{Name: "id"}, {Name: "versionId"}},
			Request:     localizationInput{},
			Responses:   []apidoc.Response{apidoc.OK("The result.", LocalizationResponse{})},
		},
		{
			Method:      "POST",
			Path:        "/templates/:id/versions/:versionId/preview",
			Summary:     "Preview version",
			Description: "Needs the `templates:read` permission.",
			PathParams:  []apidoc.Param{{Name: "id"}, {Name: "versionId"}},
			Request:     versionPreviewInput{},
			Responses:   []apidoc.Response{apidoc.OK("The result.", RenderLanguageResponse{})},
		},
		{
			Method:      "POST",
			Path:        "/templates/import",
			Summary:     "Import",
			Description: "Needs the `templates:write` permission.",
			Request:     transferDoc{},
			Responses:   []apidoc.Response{apidoc.Created("The result.", TemplateResponse{})},
		},
		{
			Method:      "POST",
			Path:        "/templates/preview",
			Summary:     "Preview",
			Description: "Needs the `templates:read` permission.",
			Request:     previewInput{},
			Responses:   []apidoc.Response{apidoc.OK("The result.", RenderResponse{})},
		},
	}
}
