---
title: "Delivery Tracking"
description: "Track webhook delivery history"
weight: 30
---

Every attempt against a webhook is recorded, successes included. That log is the only place a failure shows up —
webhooks fail quietly by design, since a receiver being down is not a reason to fail the send that triggered the event.

```
GET /api/v1/webhooks/{id}/deliveries?limit=50
```

The log belongs to one webhook, so its id is in the path.

```bash
curl "http://localhost:3000/api/v1/webhooks/0198f6a1-3c7e-7b21-9f4d-2a5c8e0b1d33/deliveries?limit=50" \
  -H "Authorization: Bearer myk_..."
```

```json
{
  "deliveries": [
    {
      "id": "0198f6b2-1a44-7c90-8e2f-3d5a7b9c1e04",
      "webhook_id": "0198f6a1-3c7e-7b21-9f4d-2a5c8e0b1d33",
      "project_id": "0198f6a0-9d12-7f33-a1b8-6c4e2f8a0d57",
      "event": "email.failed",
      "status": "failed",
      "http_status": 500,
      "error_message": "HTTP 500",
      "attempt": 3,
      "created_at": "2026-01-01T00:01:00Z"
    }
  ],
  "next_cursor": "MjAyNi0wMS0wMVQwMDowMTowMFowMTk4ZjZiMi0xYTQ0"
}
```

## Reading a row

| Field | Notes |
|---|---|
| `event` | Which event triggered this delivery |
| `status` | `success` or `failed` — nothing else |
| `http_status` | What your endpoint answered. Absent when the request never got a response |
| `error_message` | Why it failed: an HTTP status, a timeout, a dial error |
| `attempt` | Which attempt this row is, counting from 1 |

**One row per attempt.** A message that took three tries leaves three rows, so a webhook that eventually succeeded looks
like two failures followed by a success — read `attempt` alongside `status` before concluding anything is broken.

`http_status` being absent is the interesting case: it means no HTTP response came back at all. A timeout, a refused
connection, a TLS failure, or a target the SSRF guard would not dial. `error_message` says which.

## Paging

Keyset, like every log that grows on its own. Pass `next_cursor` straight back as `cursor` and keep going until it comes
back empty:

```bash
curl -G "http://localhost:3000/api/v1/webhooks/$ID/deliveries" \
  -H "Authorization: Bearer myk_..." \
  --data-urlencode "limit=50" \
  --data-urlencode "cursor=$NEXT_CURSOR"
```

Treat the cursor as opaque. It is base64 and it encodes a timestamp and a row id, but nothing promises that shape — and
a cursor it cannot parse is answered with the **first page** rather than an error, on the grounds that a stale browser
tab should not fail a list request.

There is no total. The cursor coming back empty is how you know you have reached the end.

## Retry policy

| Setting | Default | Does |
|---|---|---|
| `webhook.max_attempts` | 3 | Total attempts **including the first**, so the default is one delivery and two retries |
| `webhook.retry_delay` | 10s | A **fixed** wait between attempts, not exponential backoff |
| `webhook.timeout` | 10s | Bounds each individual request |

Any `2xx` counts as success. Everything else is retried until the attempts run out, at which point the event is dropped
and logged — there is no dead-letter queue to drain later.

{{< callout type="warning" title="The defaults give you thirty seconds, total" >}}
Three attempts, ten seconds apart, each bounded at ten seconds. A receiver that is down for a minute loses the event
permanently.

If your endpoint can be briefly unavailable, answer `2xx` as soon as you have **durably accepted** the payload and do
the work afterwards. Treat the webhook as a nudge rather than as delivery of the data itself, and reconcile against the
[email log](/docs/email-sending/email-log) for anything that must not be missed.
{{< /callout >}}

## What is not attempted

A delivery is skipped without a log row when the webhook is not subscribed to the event, or when the sender does not
pass the webhook's [filters](/docs/webhooks/overview#narrowing-by-sender). Those are not failures, so nothing is
recorded — an empty log for a webhook that should be firing usually means one of the two, not a broken receiver.

## Retention

Delivery rows are removed by the `webhook_retention_days` [platform setting](/docs/admin/platform-settings). Zero keeps
them forever, which on a busy install is a table that grows faster than the email log — one row per attempt per
subscribed webhook per event.

{{< callout type="info" title="This list may be served by a read replica" >}}
Where replicas are configured, delivery history is one of the query groups allowed to run on one
(`database.replica_reads.webhook_deliveries`, on by default). A delivery that happened a moment ago can be missing for
as long as replication lag lasts.
{{< /callout >}}
