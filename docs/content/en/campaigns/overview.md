---
title: "Overview"
description: "Create and send email campaigns to subscriber lists"
weight: 10
---

A campaign renders one [template](/docs/templates/overview) against every member of a
[subscriber list](/docs/subscribers/subscriber-lists) and delivers the results. It is bulk mail, and it differs from a
[batch send](/docs/email-sending/batch-email) in every way that matters: the audience is resolved for you, the send is
throttled, it can be paused, and it tracks engagement.

{{< callout type="tip" title="Machine API schema" >}}
The `/api/v1` surface describes itself: run
[`mailyard export-api-spec`](/docs/security/api-keys#openapi-description) and feed the document to Swagger UI, Postman,
or a client generator.
{{< /callout >}}

## Create

```
POST /api/v1/campaigns
```

```bash
curl -X POST http://localhost:3000/api/v1/campaigns \
  -H "Authorization: Bearer myk_..." \
  -H "Content-Type: application/json" \
  -d '{
    "name": "April dispatch",
    "from_email": "newsletter@example.com",
    "from_name": "Acme Industrial",
    "template_id": "0198f6a1-3c7e-7b21-9f4d-2a5c8e0b1d33",
    "list_id": "0198f6a2-7b19-7d02-9c31-4e8f1a6b3c22",
    "language": "en",
    "template_data": {"month": "April", "featured_url": "https://example.com/april"},
    "send_rate": 600
  }'
```

Four fields are required — **`name`**, **`from_email`**, **`template_id`** and **`list_id`** — and the one most people
expect to see among them is not there.

{{< callout type="info" title="`subject` is a fallback, not the subject line" >}}
The subject comes from the template's localization, like everything else about the content. The campaign's `subject`
field is used **only** when that localization renders an empty one.

So a campaign with a carefully worded `subject` and a template that has its own will send the template's. Change the
copy where the copy lives.
{{< /callout >}}

### The rest

| Field | Default | Does |
|---|---|---|
| `from_name` | — | Display name on the From header |
| `subject` | — | Fallback subject, as above |
| `language` | — | Localization to render. Falls through the [usual four steps](/docs/templates/localization#choosing-one-at-send-time) per subscriber |
| `template_data` | — | Campaign-wide render values, up to 100 keys |
| `smtp_group` | project default | Slug of the [server pool](/docs/smtp-domains/server-groups) to send through |
| `send_rate` | `0` | Emails per minute. `0` is unthrottled |
| `send_at_local_time` | `false` | Deliver at the scheduled wall-clock time in each subscriber's own timezone |
| `ab_test_enabled` | `false` | Turn on [A/B testing](/docs/campaigns/ab-testing) |
| `ab_variants` | — | Up to 5 variants |

`smtp_group` is worth setting. Bulk mail on its own pool is the usual arrangement, so a campaign that burns an IP's
reputation does not take your transactional mail down with it.

`template_data` is merged **under** each subscriber's own custom fields, and `email` and `name` are written last from
the subscriber record — so a subscriber field beats a campaign-wide one, and neither can override the recipient's own
address.

Scheduling is not a property of the campaign. Pass `scheduled_at` to the send call instead — see
[Sending](/docs/campaigns/sending). A campaign always renders the template's **active** version, and there is no way to
pin an older one.

## Statuses

| Status | Means | Editable |
|---|---|---|
| `draft` | Created, never started | Yes |
| `scheduled` | A send was requested for a future time | No |
| `sending` | The runner is working through the audience | No |
| `paused` | Stopped part-way, resumable | No |
| `sent` | Every message reached a terminal state | No |
| `cancelled` | Stopped permanently | No |

Only `draft` can be edited, and only `draft` or `scheduled` can be sent. Anything else answers `409` with "campaign is
already running or finished".

## List

```
GET /api/v1/campaigns
```

Every campaign in the project, newest first.

{{< callout type="warning" title="This route takes no parameters" >}}
No `limit`, no `offset`, no `status` filter — the whole list comes back in one response, and each entry is the campaign
record alone. **Per-campaign statistics are not included**: those come from the single-campaign route below.

Campaigns are a list somebody made by hand, so it stays small in practice. Filter client-side.
{{< /callout >}}

## Read one

```
GET /api/v1/campaigns/{id}
```

This is the route with the numbers on it:

```json
{
  "campaign": { "id": "...", "name": "April dispatch", "status": "sending" },
  "stats": { "pending": 2100, "queued": 500, "sent": 2300, "failed": 50, "skipped": 50 },
  "stats_by_variant": { "A": { "sent": 1150 }, "B": { "sent": 1150 } },
  "engagement": { "opened": 890, "clicked": 214 }
}
```

`stats` counts **messages**, one per recipient:

- `pending` — waiting for a batch
- `queued` — an email exists in the delivery queue
- `sent`, `failed` — mirroring that email's fate
- `skipped` — the recipient was suppressed, or the campaign was cancelled before their turn

`engagement` is the **unique recipient** view — how many people opened, not how many opens there were. Those counters
are aggregated as the send runs, so they survive the tracking-event retention sweep. The per-day series on
`GET /api/v1/campaigns/{id}/analytics` come from the raw event log and only reach back as far as retention keeps it.

## Manage

| Route | Does |
|---|---|
| `PATCH /api/v1/campaigns/{id}` | Edit — `draft` only |
| `POST /api/v1/campaigns/{id}/duplicate` | Copy the definition as a fresh `draft` |
| `POST /api/v1/campaigns/{id}/send` | Start, or schedule — see [Sending](/docs/campaigns/sending) |
| `POST /api/v1/campaigns/{id}/pause` | Stop between batches |
| `POST /api/v1/campaigns/{id}/resume` | Carry on |
| `POST /api/v1/campaigns/{id}/cancel` | Stop for good |
| `GET /api/v1/campaigns/{id}/messages` | The per-recipient rows, with addresses |
| `GET /api/v1/campaigns/{id}/analytics` | Per-link click tallies and daily series |
| `DELETE /api/v1/campaigns/{id}` | Remove the campaign and its messages |

Duplicate is the way to iterate: a `sent` campaign cannot be edited or re-run, so the second attempt is a copy with the
audience or the wording changed.
