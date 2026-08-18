---
title: "Platform Metrics"
description: "Platform-wide metrics and analytics"
weight: 30
---

What an administrator can watch across the whole installation, and where the line falls between platform-wide and
per-project figures.

## Where platform-wide numbers come from

There is no cross-project aggregation API. Installation-wide observability is the
[Prometheus endpoint](/docs/analytics/prometheus-metrics):

```
GET /metrics
```

It sits at the root rather than under `/api`, is off unless `metrics.enabled` is set, and can be gated with a bearer
token (`metrics.token`). It carries counters for accepted and finalized emails, inbound mail and webhook deliveries,
plus a scrape-time gauge of emails by status. Anything you want charted across tenants is built there, in your own
monitoring stack, rather than in this console.

## Per-project analytics

The delivery figures are scoped to the **active project**, resolved from the usual
`X-Mailyard-Project-Id` header, and need `analytics:read`:

```
GET /api/v1/dashboard/stats
GET /api/v1/analytics?from=2026-01-01&to=2026-01-31
```

`from` and `to` are plain `YYYY-MM-DD` dates. The window defaults to the trailing 30 days and may not exceed 366.
`daily_counts` fills days with no traffic, so a quiet week cannot silently rescale a chart's axis. Both routes exist on
the machine API too (`/api/v1/...`) with an API key holding the relevant `:read` permissions. Details in
[Email Analytics](/docs/analytics/email-analytics).

An administrator holds owner-equivalent rights in every project, so reading another project's figures means switching to
it rather than calling a different endpoint.

## Real-Time Monitoring

### Live activity stream

The console updates its notification badge from a server-sent events feed rather than polling. It is a browser feature
carried on the session cookie, so there is nothing to call from a script - for machine-side notification of the same
events, register a
[webhook](/docs/webhooks/overview).

### Stream statistics

The console reports the live subscriber count and how many events were dropped because a client stopped reading. A
dropped count that climbs steadily means some browser is not keeping up.

```json
{
    "subscribers": 3,
    "projects": 2,
    "dropped": 0
}
```

## Notifications

In-app alerts, addressed to a **project** rather than a person: what they report is a fact about the project, and read
state is shared, so one member clearing an alert clears it for everyone.

```
GET    /api/v1/notifications
GET    /api/v1/notifications/unread
POST   /api/v1/notifications/{id}/read
POST   /api/v1/notifications/read-all
DELETE /api/v1/notifications/{id}
```

Any project member. `?unread=true` narrows the list. The unread count is a separate endpoint because the console polls
it far more often than it opens the list.

### Bounce rate alerts

A scheduled job (`bounce-alert`, every 15 minutes) measures each project's bounce rate over the last hour and raises a
warning when it crosses the threshold.

| Setting                       | Default | Meaning                                                                                                                   |
|-------------------------------|---------|---------------------------------------------------------------------------------------------------------------------------|
| `bounce_alert_percent`        | `10`    | Rate that raises an alert. **0 turns it off.**                                                                            |
| `bounce_alert_min_volume`     | `20`    | Messages that must finish in the hour before the rate is judged. Two bounces out of three sends is 66% and means nothing. |
| `notification_retention_days` | `30`    | How long **read** notifications are kept. Unread ones are never purged by age.                                            |

Only terminal outcomes count toward the rate - queued and processing messages have not decided yet, and including them
would dilute the number and hide a real problem behind a backlog.

The alert is deduped per hour, so a rate that stays high produces one notification an hour rather than one every fifteen
minutes. Marking it read does not make it fire again.

{{< callout type="note" title="Why a job and not the send path" >}}
A rate is a property of a window, so it cannot be evaluated at the moment one message fails. Hooking the failure path
would also make the alert fire hardest exactly when the system is already struggling.
{{< /callout >}}
