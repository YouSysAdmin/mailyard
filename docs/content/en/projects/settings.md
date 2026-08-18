---
title: "Settings, Plan, and Audit Log"
description: "Operational project settings, plan limits, and the project audit trail"
weight: 30
---

Each project carries its own settings, an effective plan with sending limits, and an audit trail.

{{< callout type="note" title="Two different things are called settings" >}}
The settings here belong to a **project**. `/api/v1/admin/settings` is installation-wide
[platform settings](/docs/admin/platform-settings), platform administrators only.
{{< /callout >}}

{{< callout type="note" title="Project routes take a session" >}}
Anything addressing a project by **path id** reads the caller's membership, which an API key does not have - it is
refused there, so do that work in the console. `/api/v1/usage`
and `/api/v1/audit-log` act on the ACTIVE project, named by the
`X-Mailyard-Project-Id` header, and take a key normally.
{{< /callout >}}

## Project settings

A project's settings are fields on the project itself, read and written through the project route rather than a separate
settings object:

```
GET   /api/v1/projects/{id}
PATCH /api/v1/projects/{id}
```

Reading needs membership. Writing needs `settings:write`, which the owner and admin role presets carry.

The settings that shape behaviour:

| Field              | Type   | Default | What it does                                                                                                        |
|--------------------|--------|---------|---------------------------------------------------------------------------------------------------------------------|
| `name`             | string | —       | Display name                                                                                                        |
| `slug`             | string | derived | URL-safe identifier, unique across the install                                                                      |
| `description`      | string | —       | Free text                                                                                                           |
| `default_language` | string | `en`    | Language used when a template send names none                                                                       |
| `strict_senders`   | bool   | `false` | Refuse any From address not registered under [sender addresses](/docs/smtp-domains/sender-addresses)                |
| `track_opens`      | bool   | `false` | Add the open pixel to non-campaign mail. See [Tracking](/docs/tracking/overview)                                    |
| `track_clicks`     | bool   | `false` | Rewrite links in non-campaign mail                                                                                  |
| `bounce_address`   | string | —       | Envelope return path for this project's own SMTP servers. See [Bounce handling](/docs/smtp-domains/bounce-handling) |

`PATCH` takes any subset and leaves absent fields unchanged:

The updated project is returned as `{"project": {...}}`.

{{< callout type="note" title="The plan is not set here" >}}
`plan_id` is on the project but only a platform administrator may change it, through
`PATCH /api/v1/projects/{id}/plan`. See [Plans and Quotas](/docs/admin/plans).
{{< /callout >}}

## Plan and consumption

```
GET /api/v1/usage
```

Any member of the active project. It answers with the effective plan and what the project has actually used:

```json
{
    "plan": {
        "id": "3f2504e0-4f89-11d3-9a0c-0305e82c3301",
        "name": "Pro",
        "is_default": false,
        "hourly_email_limit": 5000,
        "daily_email_limit": 50000,
        "max_api_keys": 50,
        "max_smtp_servers": 10,
        "max_domains": 25,
        "max_subscribers": 100000
    },
    "usage": {
        "emails_last_hour": 120,
        "emails_last_day": 4300,
        "api_keys": 3,
        "smtp_servers": 1,
        "domains": 2,
        "subscribers": 812
    }
}
```

A limit of `0` means unlimited. `plan` is absent when the install has no plans at all. The counts come from the primary
tables on every call rather than from stored counters, so they cannot drift. Plans themselves are managed by platform
administrators - see [Plans and Quotas](/docs/admin/plans).

## Project audit log

```
GET /api/v1/audit-log
GET /api/v1/audit-log/{id}
```

The configuration trail for the active project, needing `audit:read`. Every successful mutating request is recorded by
middleware, so new routes are covered automatically.

| Parameter | Type | Default | Description                                                                           |
|-----------|------|---------|---------------------------------------------------------------------------------------|
| `limit`   | int  | `20`    | Page size. Over-asking is clamped to the ceiling, never refused.                      |
| `offset`  | int  | `0`     | Rows to skip. `page` is still honoured as a zero-based alias when `offset` is absent. |

```bash
curl "http://localhost:3000/api/v1/audit-log?limit=20" \
  -H "Authorization: Bearer myk_..." \
```

```json
{
    "events": [
        {
            "id": "9b1deb4d-3b7d-4bad-9bdd-2b0d7b3dcb6d",
            "category": "audit",
            "type": "project.member.updated",
            "project_id": "81af718e-f0ae-4780-a0d7-9f05b34dabcc",
            "actor_id": "b8493407-52ae-4d51-8396-9c8864977976",
            "actor_email": "ada@example.com",
            "client_ip": "203.0.113.10",
            "method": "PATCH",
            "path": "/api/v1/projects/81af718e-f0ae-4780-a0d7-9f05b34dabcc/members/b8493407-52ae-4d51-8396-9c8864977976",
            "status": 200,
            "created_at": "2026-05-31T10:05:00Z"
        }
    ],
    "limit": 20,
    "offset": 0
}
```

Account security events - sign-ins, two-factor changes, password resets - are a separate trail at
`GET /api/v1/security-log`, which is scoped to your own account. A platform administrator can pass `?all=true` to see
everyone's.

## Single sign-on

A project has no sign-in settings, and that is deliberate: authentication belongs to the platform, so a project decides
who reaches its own data and never how somebody signed in.

Identity providers are configured once for the installation, and who may sign in is decided there by email and domain
allowlists. People reach a project by
[invitation](/docs/projects/members-and-invitations), which is also where their role comes from. To refuse passwords
entirely, set `auth.local.enabled: false` for the installation.

See [Identity Providers](/docs/admin/oauth-providers) for the whole picture.
