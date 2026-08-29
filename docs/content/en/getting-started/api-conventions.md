---
title: "API Conventions"
description: "The two API surfaces, how each authenticates, and the response shape"
weight: 50
---

Read this before any other API page. Two surfaces exist, they authenticate differently, and the response shape is the
same everywhere.

## Two Surfaces

| Surface           | Prefix         | Authentication                                            | Project                                                     |
|-------------------|----------------|-----------------------------------------------------------|-------------------------------------------------------------|
| **Product**       | `/api/v1/...`  | `Authorization: Bearer myk_...` **or** the session cookie | Implied by the key, else the `X-Mailyard-Project-Id` header |
| **Console's own** | `/app/api/...` | Session cookie `mailyard_session`                         | `X-Mailyard-Project-Id` header                              |

They are split by what an operation **is**, not by who calls it.

`/api/v1` carries the product: sending, templates, campaigns, domains, everything an integration could plausibly want.
The console calls it too, with its cookie - which is why there is no second copy of these routes for the dashboard.

`/api/v1/admin` is the same surface one segment deeper: users, plans, identity providers, the shared SMTP pool, platform
settings. Standing an installation up is exactly the kind of thing an operator wants to script, so it is not hidden
behind the browser.

`/app/api` carries what cannot be used remotely: signing in, passkey and 2FA ceremonies, the OIDC round-trip, session
management, the live event stream. An API key is not accepted there, and would have nothing to do with it.

Accepting the cookie on `/api/v1` costs nothing in safety: the session cookie is
`SameSite=Strict`, so a browser never sends it cross-site, and a mutating request carrying it from an origin that is
not Mailyard's own is refused. It also means cookie auth works only from Mailyard's own origin - a third-party browser
application still needs a key.

{{< callout type="warning" title="Project is a header, not a path segment" >}}
There is no `/projects/current/` path prefix. A session addresses the active project through the `X-Mailyard-Project-Id`
header, falling back to `?project_id=` and then, only if you are a member of exactly one project, to that one. Belonging
to no project is an ordinary state and nothing is created for a new account, so a route that needs a project answers
`400` naming the header rather than inventing one.

An API key is bound to exactly one project, so the header is unnecessary with a key, and a mismatching one is rejected
with `403` rather than ignored.
{{< /callout >}}

```bash
# An API key: no header needed, the key names its project
curl http://localhost:3000/api/v1/emails \
  -H "Authorization: Bearer myk_..."

# The same surface with a session: cookie plus project header
curl http://localhost:3000/api/v1/emails \
  -b cookies.txt \
  -H "X-Mailyard-Project-Id: 81af718e-f0ae-4780-a0d7-9f05b34dabcc"

# The console's own: signing in is not something a key can do
curl -c cookies.txt -X POST http://localhost:3000/app/api/auth/login \
  -H "Content-Type: application/json" \
  -d '{"email":"admin@example.com","password":"your-password"}'
```

### Getting `cookies.txt`

Every console example on these pages passes `-b cookies.txt`. That file is a curl cookie jar, and you create it by
logging in with `-c`:

```bash
curl -c cookies.txt -X POST http://localhost:3000/app/api/auth/login \
  -H "Content-Type: application/json" \
  -d '{"email":"admin@example.com","password":"your-password"}'
```

`-c` **writes** the jar, `-b` **reads** it. After that one call, every later `-b
cookies.txt` request is authenticated until the session expires.

{{< callout type="warning" title="The login response contains no token" >}}
The reply body is `{"user": {...}}` and nothing else. The session JWT is delivered **only** as the `mailyard_session`
cookie, which is `HttpOnly` - there is no token field to copy out of the JSON. This is why the console examples use a
cookie jar rather than an
`Authorization` header.

A bearer header does work, but the token has to come from the cookie itself:

```bash
JWT=$(awk '/mailyard_session/{print $7}' cookies.txt)
curl http://localhost:3000/api/v1/templates -H "Authorization: Bearer $JWT"
```

Prefer the jar. For anything non-interactive, prefer an [API key](/docs/security/api-keys)
on `/api/v1` instead - it is the surface built for it.
{{< /callout >}}

If the account has two-factor authentication on, login returns `401` with
`{"requires_2fa": true}` until you resend the same request with a `totp_code` field.

## Response Shape

Responses are **bare keyed JSON**. There is no `success` flag and no `data` envelope:

```json
{
    "templates": [
        {
            "id": "...",
            "name": "welcome"
        }
    ]
}
```

Single objects use a singular key:

```json
{
    "template": {
        "id": "...",
        "name": "welcome"
    }
}
```

Errors carry an `error` string, and validation failures add a `fields` array:

```json
{
    "error": "Name is required",
    "fields": [
        {
            "field": "name",
            "rule": "required",
            "message": "Name is required"
        }
    ]
}
```

The HTTP status is the authority: `200`/`201` success, `400` invalid input, `401`
unauthenticated, `403` the credential lacks the permission, `404` missing (or in another project),
`409` conflict, `429` rate limit or quota, `503` maintenance mode.

## Identifiers

Every id is a **string UUID** generated by the application, not an auto-incrementing integer. Examples in these pages
that show `"id": 1` are stale - treat ids as opaque strings.

## Cross-Project Access

A resource belonging to another project is reported as **missing**, not forbidden.
`GET /api/v1/templates/{id}` for a template in a project you cannot see returns `404`, never `403`, so the API does not
confirm that the id exists.

## HTTP Methods

Partial updates use `PATCH`. `PUT` is reserved for full replacement and is used only where a resource genuinely has no
partial form (stylesheets, languages, template localizations).

## OpenAPI Description

The machine surface describes itself:

```bash
mailyard export-api-spec --out openapi.yaml
```

The document is generated from the types the binary was built with. Feed the file to Postman or a client generator,
or read it rendered at [API Reference](/docs/reference/api), which the same instance serves behind the docs sign-in.

`--surface` selects which one it describes: `api` for `/api/v1` (the default, and the one a client generator wants),
`app` for the console's own routes, `all` for both.

The document is generated from the response types the handlers return, so its field descriptions cannot drift from the
code. A route nobody described, or a description whose route was renamed, fails the build.
