// Mailyard, Copyright (c) 2021-2026 YouSysAdmin

package certificate

import "github.com/yousysadmin/mailyard/internal/core/apidoc"

// APIDocs describes the certificate surface. Platform admin
// throughout: a listener is not a tenant resource, and no route here
// returns a private key.
func APIDocs() []apidoc.Route {
	return []apidoc.Route{
		{
			Method:  "GET",
			Path:    "/admin/certificates",
			Tag:     "admin",
			Summary: "List managed certificates and listener assignments",
			Description: "What an administrator uploaded or generated, with what each " +
				"certificate says about itself - subject, names, expiry, SHA-256 " +
				"fingerprint - and which listener currently serves it. Private keys " +
				"are never returned.\n\n" +
				"`used_by` names the listeners actually presenting a certificate. " +
				"`dormant` names listeners it is assigned to that do not terminate " +
				"TLS, where the assignment is recorded and nothing serves it - set " +
				"`<listener>.tls.enabled` to make one take effect.",
			Responses: []apidoc.Response{apidoc.OK("The set.", ListResponse{})},
		},
		{
			Method:  "GET",
			Path:    "/admin/certificates/system",
			Tag:     "admin",
			Summary: "List the certificates the installation holds for itself",
			Description: "The ACME cache, the self-signed pair and the relay authority. " +
				"Read-only: these are maintained by the code that needs them, and " +
				"deleting the relay authority would take every node offline.",
			Responses: []apidoc.Response{apidoc.OK("The set.", SystemResponse{})},
		},
		{
			Method:  "POST",
			Path:    "/admin/certificates",
			Tag:     "admin",
			Summary: "Upload a certificate and its private key",
			Description: "The certificate may carry a chain, leaf first. The key is checked " +
				"against it before anything is stored - a mismatch brings the listener " +
				"up and then fails every handshake, which is not something to discover " +
				"later. Uploading an existing name replaces it.",
			Request: uploadInput{},
			Responses: []apidoc.Response{
				apidoc.Created("Stored.", ManagedResponse{}),
				apidoc.BadRequest,
			},
		},
		{
			Method:  "POST",
			Path:    "/admin/certificates/generate",
			Tag:     "admin",
			Summary: "Generate a certificate for a listener",
			Description: "For an internal listener or a test instance. Hosts go in the SAN " +
				"list and at least one is required - a certificate with no SAN matches no " +
				"name anywhere. The algorithm is rsa, ecdsa or ed25519.\n\n" +
				"Self-signed unless `issuer` names one of this installation's own " +
				"authorities, in which case it is signed by that one and verifies for any " +
				"client that trusts it.\n\n" +
				"`validity_days` is capped at 398. Chrome and Apple refuse any server " +
				"certificate with a longer lifetime, including one signed by a root you " +
				"installed yourself. A certificate is also never issued to outlive its " +
				"issuer: ask for longer and it is shortened to match.\n\n" +
				"Every subject field is optional. `common_name` defaults to the first host.",
			Request: generateInput{},
			Responses: []apidoc.Response{
				apidoc.Created("Generated and stored.", ManagedResponse{}),
				apidoc.BadRequest,
			},
		},
		{
			Method:  "POST",
			Path:    "/admin/certificates/generate-ca",
			Tag:     "admin",
			Summary: "Generate a certificate authority",
			Description: "An authority signs the certificates your listeners serve, so a " +
				"client trusts ONE certificate instead of one per listener. Install the " +
				"public half - `GET /admin/certificates/{name}/pem` - wherever it has to " +
				"be trusted.\n\n" +
				"It takes no hosts: an authority is a trust anchor and serves no name, " +
				"which is also why assigning one to a listener is refused - that listener " +
				"would refuse every client.\n\n" +
				"The algorithm is rsa or ecdsa. Not ed25519: several trust stores will not " +
				"install such a root.\n\n" +
				"`validity_days` defaults to 3650. `common_name` defaults to the name, and " +
				"is the string you will recognise it by in a trust store listing.\n\n" +
				"Answers 409 if the name is taken. Replacing an authority would invalidate " +
				"everything it signed with no other symptom.",
			Request: generateCAInput{},
			Responses: []apidoc.Response{
				apidoc.Created("Generated and stored.", ManagedResponse{}),
				apidoc.BadRequest,
				apidoc.Conflict,
			},
		},
		{
			Method:  "GET",
			Path:    "/admin/certificates/:name/pem",
			Tag:     "admin",
			Summary: "Read a certificate's public half",
			Description: "The certificate, PEM encoded, with no private key - this is how " +
				"an authority gets into the trust stores that have to trust it.\n\n" +
				"JSON rather than a file download, so the generated clients can read it " +
				"like every other response here.",
			PathParams: []apidoc.Param{{Name: "name"}},
			Responses: []apidoc.Response{
				apidoc.OK("The certificate.", PEMResponse{}),
				apidoc.NotFound,
			},
		},
		{
			Method:  "GET",
			Path:    "/admin/certificates/acme",
			Tag:     "admin",
			Summary: "What ACME is configured to do, and what it holds",
			Description: "Whether ACME is on, the account contact, the directory, and each " +
				"configured host with the certificate cached for it - absent until the " +
				"first issuance succeeds.\n\n" +
				"All of this is [platform settings](#tag/admin), not configuration, so it " +
				"is changed with `PUT /admin/settings` and takes effect without a " +
				"restart: `acme_enabled`, `acme_hosts` (a JSON array), `acme_email` and " +
				"`acme_directory_url`.\n\n" +
				"`tls_terminated_here` is the one thing worth reading before ordering. " +
				"When it is true the CA validates over tls-alpn-01 against the listener " +
				"that is already up, and nothing else is needed. When it is false - a " +
				"proxy in front terminates TLS - that handshake never arrives, and " +
				"`challenge_addr` has to be set so http-01 can answer on port 80.",
			Responses: []apidoc.Response{apidoc.OK("The configuration and what is cached.", ACMEResponse{})},
		},
		{
			Method:  "POST",
			Path:    "/admin/certificates/acme/order",
			Tag:     "admin",
			Summary: "Obtain a certificate for one configured host",
			Description: "Synchronous: it is an ACME round trip including a challenge, and " +
				"answering before it finishes would mean not being able to say whether " +
				"it worked. Over tls-alpn-01 that is seconds.\n\n" +
				"The host must already be in `acme_hosts` - this endpoint issues, it does " +
				"not configure. A refusal carries the CA's own words, which is the useful " +
				"part: \"DNS problem: NXDOMAIN looking up A for mail.example.com\" says " +
				"what to fix in a way \"could not issue\" does not.\n\n" +
				"Satisfied by whatever is already cached. Use renew to force a fresh one.",
			Request: renewInput{},
			Responses: []apidoc.Response{
				apidoc.OK("Issued.", MessageResponse{}),
				apidoc.BadRequest,
			},
		},
		{
			Method:  "POST",
			Path:    "/admin/certificates/acme/renew",
			Tag:     "admin",
			Summary: "Discard what is cached for a host and order again",
			Description: "Distinct from order, which is satisfied by whatever is already " +
				"cached. There is no renew-now in the ACME client: the renewal timer " +
				"lives inside it and only runs on a handshake, so dropping the cached " +
				"entry is what turns the next ask into a real order.\n\n" +
				"Both cache variants go, the ECDSA one and the RSA one - clearing only " +
				"the variant this process would ask for leaves the other stale, and " +
				"which one a client gets depends on the client.",
			Request: renewInput{},
			Responses: []apidoc.Response{
				apidoc.OK("Renewed.", MessageResponse{}),
				apidoc.BadRequest,
			},
		},
		{
			Method:     "DELETE",
			Path:       "/admin/certificates/:name",
			Tag:        "admin",
			Summary:    "Delete a managed certificate",
			PathParams: []apidoc.Param{{Name: "name"}},
			Description: "Refused while a listener is SERVING it. Allowing it would drop that " +
				"listener quietly to the next step of the chain, with only a log line " +
				"to say so.\n\n" +
				"A listener that does not terminate TLS is serving nothing, so a " +
				"dormant assignment does not block the delete - it is cleared as part " +
				"of it, rather than left naming a certificate that is gone.",
			Responses: []apidoc.Response{
				apidoc.NoContent,
				apidoc.BadRequest,
			},
		},
	}
}
