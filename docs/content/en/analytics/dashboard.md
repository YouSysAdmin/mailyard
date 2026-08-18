---
title: "Dashboard Stats"
description: "Dashboard statistics and metrics"
weight: 10
---

One call for the project overview.

```
GET /api/v1/dashboard/stats
```

Also available as `GET /api/v1/dashboard/stats` with an API key holding `analytics:read`.

```json
{
    "stats": {
        "emails": {
            "queued": 12,
            "sent": 14890,
            "failed": 230,
            "suppressed": 40
        },
        "total_emails": 15172,
        "failure_rate": 1.52,
        "inbound": {
            "received": 300,
            "rejected": 4
        },
        "resources": {
            "domains": 3,
            "verified_domains": 2,
            "smtp_servers": 2,
            "api_keys": 5,
            "active_api_keys": 4,
            "smtp_credentials": 1,
            "senders": 4,
            "templates": 9,
            "contacts": 8200,
            "subscribers": 1200,
            "suppressions": 40,
            "bounces": 120,
            "webhooks": 2,
            "campaigns": 6,
            "unsubscribe_lists": 3
        }
    }
}
```

`emails` and `inbound` are keyed by status, and only statuses with rows appear - a project that has never had a failure
has no `failed` key rather than `"failed": 0`.

{{< callout type="info" title="How failure_rate is calculated" >}}
`failed / (sent + failed)`, as a percentage. Queued and scheduled mail is **excluded**. Counting messages that have not
been attempted yet as successes would flatter the number, and counting them as failures would slander it. A project with
nothing finalized reports
`0`.
{{< /callout >}}
