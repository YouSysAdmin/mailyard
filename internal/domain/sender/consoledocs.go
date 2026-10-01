// Mailyard, Copyright (c) 2021-2026 YouSysAdmin

package sender

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
			Path:        "/senders/",
			Tag:         "sender",
			Summary:     "List",
			Description: "Needs the `senders:read` permission.",
			Responses:   []apidoc.Response{apidoc.OK("The result.", ListResponse{})},
		},
		{
			Method:      "POST",
			Path:        "/senders/",
			Tag:         "sender",
			Summary:     "Create",
			Description: "Needs the `senders:write` permission.",
			Request:     createInput{},
			Responses:   []apidoc.Response{apidoc.Created("The result.", SenderResponse{})},
		},
		{
			Method:      "GET",
			Path:        "/senders/:id",
			Tag:         "sender",
			Summary:     "Get",
			Description: "Needs the `senders:read` permission.",
			PathParams:  []apidoc.Param{{Name: "id"}},
			Responses:   []apidoc.Response{apidoc.OK("The result.", SenderResponse{})},
		},
		{
			Method:      "PATCH",
			Path:        "/senders/:id",
			Tag:         "sender",
			Summary:     "Update",
			Description: "Needs the `senders:write` permission. The display name, which a bare From address gets on the server. An empty name clears it. The address itself cannot change: a different address is a different sender.",
			PathParams:  []apidoc.Param{{Name: "id"}},
			Request:     updateInput{},
			Responses:   []apidoc.Response{apidoc.OK("The result.", SenderResponse{})},
		},
		{
			Method:      "DELETE",
			Path:        "/senders/:id",
			Tag:         "sender",
			Summary:     "Delete",
			Description: "Needs the `senders:delete` permission.",
			PathParams:  []apidoc.Param{{Name: "id"}},
			Responses:   []apidoc.Response{apidoc.NoContent},
		},
		{
			Method:  "POST",
			Path:    "/senders/:id/signing",
			Tag:     "sender",
			Summary: "Give the address a signing key",
			Description: "Mail from the address is then signed as it, S/MIME or OpenPGP, " +
				"inside the message and under DKIM. `kind: pgp` with no `private_key` generates " +
				"an Ed25519 key, with one imports it (opened with `passphrase` when protected). " +
				"`kind: smime` takes a PEM `certificate` chain plus `private_key`, or `pkcs12` as " +
				"base64 with its `passphrase`. A certificate not issued to the address, without " +
				"the email protection usage, expired, or not matching the key is refused on the " +
				"field it concerns. 409 when the sender already has a key: remove that one first. " +
				"Needs the `senders:write` permission.",
			PathParams: []apidoc.Param{{Name: "id"}},
			Request:    signingInput{},
			Responses:  []apidoc.Response{apidoc.Created("The sender with its key described.", SenderResponse{}), apidoc.BadRequest, apidoc.NotFound, apidoc.Conflict},
		},
		{
			Method:  "PATCH",
			Path:    "/senders/:id/signing",
			Tag:     "sender",
			Summary: "Switch signing or key attachment on or off",
			Description: "`sign` off keeps the key and stops using it. `attach_key` is PGP only: " +
				"the Autocrypt header and the key file on every message. A field left out keeps " +
				"its value. Needs the `senders:write` permission.",
			PathParams: []apidoc.Param{{Name: "id"}},
			Request:    signingFlagsInput{},
			Responses:  []apidoc.Response{apidoc.OK("The sender with its key described.", SenderResponse{}), apidoc.NotFound},
		},
		{
			Method:      "DELETE",
			Path:        "/senders/:id/signing",
			Tag:         "sender",
			Summary:     "Remove the signing key",
			Description: "Mail from the address goes out unsigned from here on. Needs the `senders:delete` permission.",
			PathParams:  []apidoc.Param{{Name: "id"}},
			Responses:   []apidoc.Response{apidoc.NoContent, apidoc.NotFound},
		},
		{
			Method:  "GET",
			Path:    "/senders/:id/signing/public-key",
			Tag:     "sender",
			Summary: "The public half of the signing key",
			Description: "The armored OpenPGP key or the PEM certificate chain, leaf first, for " +
				"publishing wherever recipients look it up. Needs the `senders:read` permission.",
			PathParams: []apidoc.Param{{Name: "id"}},
			Responses:  []apidoc.Response{apidoc.OK("The key.", PublicKeyResponse{}), apidoc.NotFound},
		},
	}
}
