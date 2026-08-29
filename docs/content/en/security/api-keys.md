---
title: "API Keys"
description: "Programmatic access to the machine API, permissions, and key lifecycle"
weight: 20
---

API keys are the credential for the **machine API** at `/api/v1`. A key is bound to exactly one project and carries a
set of permissions. Manage them in the console under **Developers - API Keys**, or through `/api/v1/api-keys`.

Use a key for anything non-interactive. A browser session works on the same surface, but driving one from a script means
holding a cookie jar and re-authenticating.

## OpenAPI Description

The binary writes its own description, generated from the Go types this build returns, so it changes only when the
binary does. Browse it rendered at [API Reference](/docs/reference/api), served from the same instance and always
describing the one you are signed in to. As a file, for a generator:

```bash
mailyard export-api-spec --out openapi.yaml
```

Two surfaces are available. `--surface api` is the default and covers `/api/v1` - the product, and what you build
against. `--surface app` covers `/app/api`, the console's own sign-in ceremonies and event stream, which belong to the
web UI and move with it.
`--surface all` writes both, suffixing the filenames.

Run the binary you are actually deploying and the document matches it exactly.

Three clients ship in the repository, generated from that same metadata: `sdk/go`,
`sdk/python` and `sdk/ruby`. None has a dependency, all three name a route the same way, and a test fails the build if
any falls out of step. For any other language, generate from the document above.

**Go** (`go get github.com/yousysadmin/mailyard/sdk/go`) - typed, with a hand-written half for the calls most
integrations make:

```go
c := mailyard.New("https://mail.example.com", "myk_...")

res, err := c.Send(ctx, mailyard.SendRequest{
From: "billing@example.com", To: []string{"customer@example.net"},
Subject: "Your invoice", HTML: "<p>Attached.</p>",
})

tpl, err := c.API().CreateTemplate(ctx, api.TemplateCreateInput{
Name: "welcome", Subject: "Hello", HTML: "<p>Hi</p>",
})
```

Refusals arrive as a typed error carrying the HTTP status (`mailyard.IsOverQuota`,
`IsNotFound`, `IsUnauthorized`).

**Python** (`pip install ./sdk/python`) - bodies are dicts, results are parsed JSON:

```python
from mailyard import Client

client = Client(api_key="myk_...", base_url="https://mail.example.com")
client.api.send_email(body={
    "from": "billing@example.com", "to": ["customer@example.net"],
    "subject": "Your invoice", "html": "<p>Attached.</p>",
})
```

**Ruby** (`sdk/ruby`) - the same, with hashes:

```ruby
client = Mailyard::Client.new(api_key: "myk_...", base_url: "https://mail.example.com")
client.api.send_email(body: {
  from: "billing@example.com", to: ["customer@example.net"],
  subject: "Your invoice", html: "<p>Attached.</p>"
})
```

Both raise on a refusal (`MailyardError`, `Mailyard::Error`) carrying the status and the field errors, and both page a
cursor list with `paginate`.

The document is **generated from the code that serves the requests** - every field comes from the Go type the handler
returns, so it cannot describe a body the API does not send. A route with no description, or a description whose route
was renamed, fails the build. Secrets are kept out by a separate test, since the generator would otherwise publish
whatever a response type carries.

## Permissions

A key grants only what its permissions allow, drawn from the **same catalogue** a project member is judged by -
`resource:action`, where the action is `read`, `write` or `delete`
and each resource declares which of the three it actually has. The full list is at
[Projects - Roles](/docs/projects/overview#roles), and `GET /api/v1/permissions` returns it from the binary that
enforces it.

Every `/api/v1` route names the resource and action it needs. A request without it gets
`403` and `{"error": "permission <resource>:<action> required"}`, or - when the key holds nothing at all on that
resource - `{"error": "no access to <resource> in this project"}`.

```json
[
    "emails:read",
    "emails:write",
    "suppressions:write"
]
```

That key can send mail, read the delivery log and block an address. It cannot see the domains, the SMTP configuration,
the audit trail or the export.

| Common shape                                | Permissions                                     |
|---------------------------------------------|-------------------------------------------------|
| Transactional sender                        | `emails:write`, `emails:read`                   |
| Sender that also honours opt-outs           | add `suppressions:read`, `suppressions:write`   |
| Reporting / dashboard integration           | `analytics:read`, `emails:read`, `bounces:read` |
| Webhook manager                             | `webhooks:read`, `webhooks:write`               |
| Backup / compliance job                     | `data:read`                                     |
| Relay node enrolment                        | `relay:write`                                   |
| Everything, including resources added later | `*`                                             |

{{< callout type="warning" title="An empty permission list grants nothing" >}}
A key created with no `permissions` field can do **nothing**. This is a change in direction: an empty list used to mean
"send", a sensible default back when sending was all a key could do. Now that a key can reach the whole project surface,
the unstated intent has to fail closed.
{{< /callout >}}

{{< callout type="note" title="Why a key may hold `*` when a custom group may not" >}}
A [custom group](/docs/projects/members-and-invitations) refuses the wildcard, because a person who should have
everything is given the admin **role**, where it is visible for what it is. A key has no role behind it, so the wildcard
is the only way to say "this is the project's own deployment key" - and it keeps covering the product as resources are
added, rather than quietly stopping at whatever existed on the day it was minted.
{{< /callout >}}

### What a key still cannot do

Permissions decide the resource. Two things sit above them and are refused to every project key regardless of what it
holds:

- **Deleting the project** and **changing the SSO policy**. These are owner-tier acts.
- **Platform administration** - users, plans, installation settings. See below: that is a different credential, not a
  wider permission list.

Data export (`data:read`) returns every tenant record in one call, which is a wider grant than any other read. It has
its own resource for that reason rather than riding along with the rest.

## Platform Credentials

`/api/v1/admin` - users, plans, identity providers, the shared SMTP pool, installation settings - takes a **different
key**, minted under **Admin - Platform Credentials**. Its tokens start `mya_` rather than `myk_`.

```bash
curl http://localhost:3000/api/v1/admin/users \
  -H "Authorization: Bearer mya_..."
```

A project API key holding the wildcard is still refused there:

```json
{
    "error": "admin privileges required"
}
```

That is the point. **Admin is not a permission**, it is a different credential, on a separate table, with no permission
list at all - the catalogue describes what a member may do inside a project, and none of its resources mean "may create
a user".

A platform credential is also owner-equivalent in any project it names with
`X-Mailyard-Project-Id`, exactly as a platform-admin session is. Without that header it has no project, which is correct
for the admin routes and a `400` on everything else.

{{< callout type="warning" title="Narrow it, or do not mint it" >}}
There is nothing to restrict by resource, so the two controls that matter are
`allowed_ips` and `expires_at`. Prefer a project key with a narrow permission list whenever the job is actually about
one project - most jobs are.
{{< /callout >}}

Revoking takes effect on the next request, and every write made with one is recorded in the security log naming the
credential (`admin api key <name>`) rather than a blank actor.

## Creating a Key

```
POST /api/v1/api-keys
```

Needs `apikeys:write`, and that is the whole rule - a key holding it mints other keys just as a member does. Nothing on
this route asks how you authenticated.

```bash
curl -X POST http://localhost:3000/api/v1/api-keys \
  -H "Authorization: Bearer myk_phlLKeNu_yUEzc8..." \
  -H "Content-Type: application/json" \
  -d '{
    "name": "Production sender",
    "permissions": ["emails:write", "emails:read"],
    "allowed_ips": ["203.0.113.0/24"]
  }'
```

No `X-Mailyard-Project-Id`: the key names its own project, and the new key is minted in that one.
See [Using a Key](#using-a-key).

The first key has to come from somewhere else, since there is no key yet to present. That one is made from the console,
or with a session and the header naming the project:

```bash
curl -X POST http://localhost:3000/api/v1/api-keys \
  -H "Authorization: Bearer myk_..." \
  -H "Content-Type: application/json" \
  -d '{
    "name": "Bootstrap",
    "permissions": ["apikeys:write", "apikeys:read"]
  }'
```

{{< callout type="warning" title="A key that can mint keys is owner-equivalent in its project" >}}
`apikeys:write` lets the holder mint a key carrying any permission in the catalogue,
`*` included, and then use it. Treat it as the whole project, not as one more permission - give it to a credential that
provisions an installation, never to the key an application sends mail with.

It stops at the project. A key minted this way cannot reach `/api/v1/admin`, however wide its permissions are: platform
administration is a different credential, not a permission. See [Platform credentials](#platform-credentials).
{{< /callout >}}

| Field         | Type              | Description                                                                                                                                                                                                                                                                                                                                        |
|---------------|-------------------|----------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------|
| `name`        | string (required) | Human-readable label                                                                                                                                                                                                                                                                                                                               |
| `permissions` | string[]          | Catalogue strings, or the single wildcard `*`. Omitted grants nothing.                                                                                                                                                                                                                                                                             |
| `allowed_ips` | string[]          | Restrict usage to specific IPs or CIDR ranges                                                                                                                                                                                                                                                                                                      |
| `expires_at`  | string            | RFC 3339 timestamp. Omitted means never expires.                                                                                                                                                                                                                                                                                                   |
| `sandbox`     | boolean           | Everything this key sends is captured into the [sandbox](/docs/email-sending/sandbox) instead of delivered. Fixed at creation. Such a key is judged on `sandbox:write` rather than `emails:write` and needs no permission on emails at all - see [what a sandbox credential may do](/docs/email-sending/sandbox#what-a-sandbox-credential-may-do). |

An unknown permission is refused at creation rather than accepted and ignored, so a typo is a `400` naming the entry
instead of a key that saves fine and is mysteriously denied.

Response:

```json
{
    "api_key": {
        "id": "5ca0d3e1-b3fe-4485-98cb-f7a33f668790",
        "project_id": "81af718e-f0ae-4780-a0d7-9f05b34dabcc",
        "name": "Production sender",
        "key_prefix": "myk_phlLKeNu",
        "permissions": [
            "emails:write",
            "emails:read"
        ],
        "allowed_ips": [
            "203.0.113.0/24"
        ],
        "revoked": false,
        "created_at": "2026-08-06T05:23:43Z"
    },
    "token": "myk_phlLKeNu_yUEzc8yYab7wPegWcbf6QtzW87F0lWkfHM"
}
```

{{< callout type="warning" title="Save the token immediately" >}}
`token` appears in this one response and never again. Only `hex(sha256(token))` is stored, so nobody - including an
operator with database access - can recover it. The
`key_prefix` exists so you can tell keys apart in a list.
{{< /callout >}}

## Using a Key

```bash
curl -X POST http://localhost:3000/api/v1/emails/send \
  -H "Authorization: Bearer myk_phlLKeNu_yUEzc8..." \
  -H "Content-Type: application/json" \
  -d '{ ... }'
```

The key implies its project, so `X-Mailyard-Project-Id` is unnecessary. Sending a header naming a **different** project
is rejected with `403` rather than ignored.

### Two credentials, one surface

`/api/v1` accepts **either** an API key or the session a browser already holds. The console uses the second, which is
why there is no second copy of these routes for it.

They differ in exactly one thing - where the project comes from. A key names its own; a session names one with
`X-Mailyard-Project-Id`, falling back to the caller's default project. Everything after that is identical: the same
permission check, the same handler, the same response. A session is judged by its **membership**, so somebody carrying a
read-only role reaches exactly what that role reaches, on this surface as on any other.

A key presented to an `/app/api` route is still not accepted - that surface is the browser's sign-in ceremonies, and a
key has no business there.

{{< callout type="note" title="Why cookies here are not a CSRF risk" >}}
The session cookie is `SameSite=Strict`, so a browser never attaches it to a request originating from another site.
Strict is a site boundary, not an origin one - a sibling subdomain is same-site - so a mutating request that carries
the cookie is also refused (403) when its `Origin` is not Mailyard's own, the host the request arrived on, or one of
`cors.allowed_origins`. Requests carrying an `Authorization` header are not subject to that check. Together the two
mean cookie auth works only from Mailyard's own origin - a third-party browser application still needs a key, which
is the intent.
{{< /callout >}}

## Listing Keys

```
GET /api/v1/api-keys
```

Any project member can list. Only the prefix is returned - see the warning above.

## Revoking a Key

Instantly disable a key without deleting it. Project admin role required.

```
POST /api/v1/api-keys/{id}/revoke
```

Revocation takes effect on the next request. A revoked key stays in the list, so an audit trail of what existed is
preserved.

## Deleting a Key

```
DELETE /api/v1/api-keys/{id}
```

Project admin role required. Deleting removes the record entirely - prefer revoking if you may later need to explain
what a key was.

## Security Features

- **Hashed storage** - only `hex(sha256(token))` is kept, compared in constant time
- **Permissions** - a key grants only the resources it names, from the same catalogue that governs people
- **Expiry** - optional `expires_at` to enforce rotation
- **IP allowlist** - restrict a key to specific IPs or CIDR ranges
- **Last used tracking** - recorded (throttled) so unused keys are identifiable
- **Revocation** - instant disable without losing the record
- **Rate limiting** - `/api/v1` is limited per key, see [Rate Limiting](/docs/security/rate-limiting)
