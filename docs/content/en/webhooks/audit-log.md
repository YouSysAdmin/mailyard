---
title: "Audit Log"
description: "Track project operational and personal security events"
weight: 40
---

Mailyard keeps two trails. Both live in one table and are read through different endpoints with different gates.

## Project Activity

Configuration changes inside one project: credentials minted or revoked, templates changed, SMTP servers added, webhooks
edited. Requires the project `admin` or `owner`
role.

```
GET /api/v1/audit-log?limit=50&offset=0
```

```bash
curl "http://localhost:3000/api/v1/audit-log?limit=50" \
  -H "Authorization: Bearer myk_..." \
```

```json
{
    "events": [
        {
            "id": "0c1f...",
            "category": "project",
            "type": "apikey.created",
            "project_id": "81af718e-f0ae-4780-a0d7-9f05b34dabcc",
            "actor_id": "c223c373-d501-4643-8cdb-35abb984ed7e",
            "actor_email": "admin@example.com",
            "client_ip": "203.0.113.42",
            "method": "POST",
            "path": "/api/v1/api-keys/",
            "status": 201,
            "created_at": "2026-08-06T02:11:00Z"
        }
    ],
    "limit": 50,
    "offset": 0
}
```

## Account Security

Sign-ins, failed sign-ins, sign-outs, two-factor changes, and completed password resets. Carries no project.

Read it in the console under **Developers - Audit Log - Security**. Each person sees their own events. A platform admin
sees every account, which is what spots a burst of failed sign-ins across addresses.

Types recorded here:

| Type                                     | Meaning                                   |
|------------------------------------------|-------------------------------------------|
| `auth.login.succeeded`                   | Password (and 2FA where enabled) accepted |
| `auth.login.failed`                      | Rejected. `detail` says which leg failed  |
| `auth.logout`                            | Session cookie cleared                    |
| `auth.oidc.login`                        | SSO sign-in completed                     |
| `auth.oidc.denied`                       | SSO identity refused by the allowlist     |
| `auth.2fa.enabled` / `auth.2fa.disabled` | Two-factor changed                        |
| `auth.password_reset.completed`          | A reset link was redeemed                 |

## What Gets Recorded

Project events are captured by middleware on every **successful mutating** request, so a new endpoint is covered the day
it is added rather than the day somebody remembers to add a log line. Event types are derived from the route:
`POST /api/v1/api-keys` becomes
`apikey.created`, `POST /api/v1/smtp-servers/{id}/test` becomes `smtpserver.test`. Every row also carries the raw method
and path, so a derived type that reads oddly is still traceable.

Two consequences worth knowing:

- **Rejected requests are not recorded.** A validation error changed nothing, and logging every 400 would bury the
  configuration changes an auditor is looking for. Failed sign-ins are the deliberate exception, because there the
  failure is the event.
- **Reads are not recorded.** The trail answers "what changed", not "who looked".

Writes with no project context - platform settings, user management, plan changes - are filed on the acting user's
security trail rather than inventing a project for them.

## Durability

Audit writes are queued and flushed by a background writer, so recording never adds latency to the request that
triggered it and never fails that request. If the queue fills because the database is wedged, events are dropped and
each drop is logged as an error with a running total. An install that needs guaranteed-durable audit before the action
completes needs a different design - this one favors the action succeeding.

## Retention

Controlled by `audit_log_retention_days` in
[Platform Settings](/docs/admin/platform-settings), default 90 days. `0` keeps entries forever.
The [retention job](/docs/admin/scheduled-jobs) does the trimming.
