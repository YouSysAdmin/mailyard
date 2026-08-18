// Mailyard, Copyright (c) 2021-2026 YouSysAdmin

package data

import "github.com/yousysadmin/mailyard/internal/core/apidoc"

// APIDocs describes the export. Erasure is console-only on purpose: a
// bulk delete should need a person, not a key.
func APIDocs() []apidoc.Route {
	return []apidoc.Route{
		{
			Method:     "GET",
			Path:       "/data/export",
			Tag:        "meta",
			Permission: "data:read",
			Summary:    "Export every record of the key's project",
			Description: "On its own `data` resource rather than under any other read " +
				"permission, because one call returns every tenant record at once - a " +
				"broader grant than any other read route hands out.\n\n" +
				"No secret is included: SMTP passwords, key hashes, TOTP seeds and " +
				"signing secrets are omitted because the models carry `json:\"-\"`, " +
				"which a test verifies against the real bytes.",
			Responses: []apidoc.Response{apidoc.OK("The snapshot.", ExportResponse{})},
		},
	}
}
