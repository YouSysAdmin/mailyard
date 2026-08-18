// Mailyard, Copyright (c) 2021-2026 YouSysAdmin

package apikey

import "github.com/yousysadmin/mailyard/internal/core/apidoc"

// APIDocs describes the platform credential surface.
//
// Handwritten rather than generated, because these four routes are
// the ones most likely to be read by somebody deciding whether to
// mint a credential that can rewrite an installation - and the
// generated one sentence per route would not tell them what they are
// holding.
//
// The project key surface (/api/v1/api-keys) is generated like the
// rest of the product.
func APIDocs() []apidoc.Route {
	const adminOnly = "Platform administration. A project API key is refused here " +
		"however wide its permissions are: admin is not a permission, it is a " +
		"different credential."

	return []apidoc.Route{
		{
			Method:      "GET",
			Path:        "/admin/api-keys",
			Tag:         "admin",
			Summary:     "List platform credentials",
			Description: adminOnly + "\n\nOnly the prefix of each key is stored, so a list can never hand back a usable credential.",
			Responses:   []apidoc.Response{apidoc.OK("The credentials.", AdminListResponse{})},
		},
		{
			Method:  "POST",
			Path:    "/admin/api-keys",
			Tag:     "admin",
			Summary: "Mint a platform credential",
			Description: adminOnly + "\n\nThe token is returned ONCE and never again - only " +
				"`hex(sha256(token))` is kept, so nobody, including an operator with " +
				"database access, can recover it.\n\n" +
				"There is no permission list. A key on this surface can create users, " +
				"edit plans and rewrite installation settings, which is why minting one " +
				"is a deliberate act rather than a convenience. Narrow it with " +
				"`allowed_ips` and `expires_at`.",
			Request:   adminCreateInput{},
			Responses: []apidoc.Response{apidoc.Created("The credential, plus its token.", AdminCreatedResponse{})},
		},
		{
			Method:      "POST",
			Path:        "/admin/api-keys/:id/revoke",
			Tag:         "admin",
			Summary:     "Revoke a platform credential",
			Description: adminOnly + "\n\nTakes effect on the next request. The record stays, so the trail of what existed is preserved.",
			PathParams:  []apidoc.Param{{Name: "id", Description: "The credential id."}},
			Responses:   []apidoc.Response{apidoc.OK("The revoked credential.", AdminAPIKeyResponse{})},
		},
		{
			Method:      "DELETE",
			Path:        "/admin/api-keys/:id",
			Tag:         "admin",
			Summary:     "Delete a platform credential",
			Description: adminOnly + "\n\nRemoves the record entirely. Prefer revoking if you may later need to explain what a credential was.",
			PathParams:  []apidoc.Param{{Name: "id", Description: "The credential id."}},
			Responses:   []apidoc.Response{apidoc.NoContent},
		},
	}
}
