---
title: "Authentication"
description: "How a machine authenticates against the API"
weight: 10
---

`/api/v1` takes a bearer token. Two kinds:

| Token     | Reaches           | Bound to         |
|-----------|-------------------|------------------|
| `myk_...` | the product API   | one project      |
| `mya_...` | `/api/v1/admin/*` | the installation |

```
Authorization: Bearer myk_...
```

A project key names its own project, so no `X-Mailyard-Project-Id` header is needed. A header naming a different project
is refused with `403`. Minting and managing keys:
[API Keys](/docs/security/api-keys).

What a key may do comes from the permission catalogue, the same one that governs project members. See [Roles](#roles)
below.

## Signing in to the console

People sign in at `/app` with a password, a passkey, or an identity provider, and the browser carries a session cookie
from there. None of that is part of the machine API and none of it is needed to call `/api/v1`.

Setting it up: [Identity Providers](/docs/admin/oauth-providers) for SSO,
[Two-Factor Auth](/docs/security/two-factor-auth), [Passkeys](/docs/security/passkeys).

{{< callout type="note" title="SSO never disables password sign-in" >}}
Adding a provider does not turn off local login. It is how you configure the first provider and how you get back in when
one breaks. Keep at least one local admin account.
{{< /callout >}}

## Routes a session still owns

A few `/api/v1` routes address a project by path id and read the caller's MEMBERSHIP, so an API key is refused on them
however wide its permissions are:

- `/api/v1/projects` and everything under `/api/v1/projects/{id}/`
- `/api/v1/invitations/{token}/accept` and `/decline`

Do those in the console, or with a signed-in session.

## Roles

Two role systems, at different levels.

**Platform administration** is one flag on the user account, `admin`:

| `admin` | Permissions                                                                                                                             |
|---------|-----------------------------------------------------------------------------------------------------------------------------------------|
| `false` | Ordinary account. Access is decided by project membership.                                                                              |
| `true`  | Platform administration: users, plans, platform settings, scheduled jobs, the whole security log. Also owner-equivalent in any project. |

No admin can change their own `admin` or `disabled` flag - that guard exists so the last administrator cannot lock
themselves out. Another admin still can.

**How an account signs in** is a separate field, `account_type`: 1 for a local account and 2 for one an identity
provider owns. It decides one thing - whether the password, two-factor authentication and passkeys are managed here or
at the provider. The two are independent, so an account can perfectly well be `account_type` 2 and
`admin` true.

**Project roles**, per membership. There are no built-in ones: a project writes the roles it needs as named lists of
permissions (`resource:action`), and every tenant route checks a permission rather than comparing roles. **Ownership**
is the one thing that is not a permission - it is a flag on the membership row carrying the two acts no permission can
name, deleting the project. See
[Projects - Roles](/docs/projects/overview#roles).

## Public Endpoints

These need no token:

- `GET /healthz` - liveness, answers as long as the process is up
- `GET /readyz` - readiness, checks the database - see
  [Health checks](/docs/analytics/health-checks)
- `/tracking/*` - open pixel, click redirects, hosted unsubscribe
- `POST /webhooks/ses` - SES feedback, authenticated by the SNS signature
- `/api/relay-nodes/*` - relay node enrolment, authenticated by its enrol token
  (enterprise edition)
