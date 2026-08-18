---
title: "Shared SMTP Pool"
description: "Platform-owned SMTP servers used by projects that have configured none of their own"
weight: 40
---

A platform admin can register SMTP servers that belong to the **platform** rather than to any project. A project that
has configured no server of its own delivers through this pool, so an installation can be usable the moment somebody is
invited, without every project repeating the same SMTP credentials.

Manage them under **Admin -> Shared SMTP**, or through `/api/v1/admin/shared-smtp-servers`.

{{< callout type="info" title="Projects never see these servers" >}}
No project-scoped endpoint returns a shared server, and passwords are never serialized anywhere. A project gets
*delivery* through the pool, not sight of it. `GET /api/v1/smtp-servers`
lists only the project's own.
{{< /callout >}}

## When the pool is used

The rule is deliberately blunt:

| The project                | Delivers through        |
|----------------------------|-------------------------|
| owns no SMTP server at all | the shared pool         |
| owns one or more           | its own servers, always |

**Owning a server means owning delivery.** A project whose only server is disabled, or whose server rejects the sender,
gets a plain "no enabled smtp server accepts this sender" failure - it does **not** silently fall back to the pool.
Quietly rerouting a project's mail through platform credentials would send it from a different IP, under a different SPF
record, than the operator configured, and they would discover that from a deliverability report weeks later rather than
from an error now.

Within the pool, servers are tried in `priority` order (lowest first, then oldest), and the first one that admits the
sender wins.

## Restricting who may use a server

Two filters apply, and both must pass:

| Field             | Effect                                                             |
|-------------------|--------------------------------------------------------------------|
| `allowed_emails`  | Exact addresses or `*@domain` wildcards. Empty allows any address. |
| `allowed_domains` | Bare sender domains. Empty allows any domain.                      |

On top of those, `security_mode` decides whether ownership matters:

| Mode                   | Meaning                                                                                                        |
|------------------------|----------------------------------------------------------------------------------------------------------------|
| `permissive` (default) | Any sender the two filters admit may relay.                                                                    |
| `strict`               | The sending **project** must also have [verified](/docs/smtp-domains/domain-verification) the sender's domain. |

Strict mode is what stops one project relaying as another project's domain through platform credentials. Domain names
are globally unique in Mailyard, so the check is that the verified domain belongs to *the project that is sending* - a
domain verified by somebody else does not count. Use strict on any pool server whose reputation you care about.

## API

All routes require the platform `admin` role.

### List

```
GET /api/v1/admin/shared-smtp-servers
```

```json
{
    "shared_smtp_servers": [
        {
            "id": "3f1c...",
            "name": "Company Outbound",
            "host": "smtp.company.com",
            "port": 587,
            "username": "mailyard@company.com",
            "encryption": "starttls",
            "skip_dkim": false,
            "allowed_emails": [],
            "allowed_domains": [
                "company.com"
            ],
            "security_mode": "strict",
            "priority": 0,
            "status": "enabled",
            "created_at": "2026-08-07T09:00:00Z"
        }
    ]
}
```

### Create

```
POST /api/v1/admin/shared-smtp-servers
```

```json
{
    "name": "Company Outbound",
    "host": "smtp.company.com",
    "port": 587,
    "username": "mailyard@company.com",
    "password": "secret",
    "encryption": "starttls",
    "allowed_domains": [
        "company.com"
    ],
    "security_mode": "strict",
    "priority": 0
}
```

| Field             | Required | Description                                                                 |
|-------------------|----------|-----------------------------------------------------------------------------|
| `name`            | Yes      | Display name                                                                |
| `host`            | Yes      | SMTP hostname                                                               |
| `port`            | Yes      | SMTP port, typically 25, 465 or 587                                         |
| `username`        | No       | AUTH username, omit for an IP-authenticated relay                           |
| `password`        | No       | AUTH password, stored encrypted and never returned                          |
| `encryption`      | No       | `none`, `starttls` or `ssl` (default `none`)                                |
| `skip_dkim`       | No       | Suppress Mailyard's DKIM signature, for providers that re-sign (Amazon SES) |
| `allowed_emails`  | No       | Exact addresses or `*@domain` wildcards                                     |
| `allowed_domains` | No       | Bare sender domains                                                         |
| `security_mode`   | No       | `permissive` (default) or `strict`                                          |
| `priority`        | No       | Lowest first, ties broken by age                                            |

Returns `201`. The connection is **not** dialled on create - run the test below when you are ready, so a create against
a firewalled host does not fail for the wrong reason.

### Update

```
PATCH /api/v1/admin/shared-smtp-servers/{id}
```

Every field is optional. An omitted `password` leaves the stored one alone, so a server can be re-pointed without
re-entering credentials. Include `"status": "enabled"` or
`"disabled"` to change availability.

### Test the connection

```
POST /api/v1/admin/shared-smtp-servers/{id}/test
```

```json
{
    "ok": true,
    "status": "enabled",
    "validated_at": "2026-08-07T09:05:00Z"
}
```

A failure records `status: invalid` with the error on the row, and the delivery worker stops considering that server.
This matters more here than on a project server: the pool is the fallback for every project that owns nothing, so a dead
entry in it is a dead entry for all of them. A later successful test clears the verdict.

### Delete

```
DELETE /api/v1/admin/shared-smtp-servers/{id}
```

Returns `204`. Projects relying on the pool lose delivery if this was the only server that admitted them, so check what
else is enabled first.

## See also

- [SMTP Servers](/docs/smtp-domains/smtp-servers) - the per-project servers a project owns and controls
- [Server Groups](/docs/smtp-domains/server-groups) - how a project routes between its own servers, and the full
  resolution order
- [Domain Verification](/docs/smtp-domains/domain-verification) - what `security_mode: strict` requires
