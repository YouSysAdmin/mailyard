---
title: "Managing Inbound Email"
description: "List, fetch, retry, export, and stream inbound email from your project"
weight: 30
---

Mail that arrived on the [MX listener](/docs/inbound/overview) is stored per project and read through these routes.
They are ordinary `/api/v1` endpoints under the `inbound` permission — an API key carries its project, a session needs
the `X-Mailyard-Project-Id` header.

## Listing

```
GET /api/v1/inbound-emails?limit=50&status=received
```

| Param | Notes |
|---|---|
| `status` | One of the three below |
| `limit` | Default 50, maximum 200 |
| `before` | Cursor: RFC 3339 `received_at` of the last row you saw |
| `before_id` | The id of that row. Send it with `before` |

There are three statuses, and no others:

| Status | Means |
|---|---|
| `received` | Stored and parsed |
| `rejected` | Refused at ingest — a suppressed sender, or a DMARC failure on a `p=reject` domain |
| `failed` | The MIME tree could not be parsed. The raw bytes are still there |

Paging is a **keyset cursor**, like the outbound log, and for the same reason: this list grows on its own. Send
`before_id` alongside `before` or two messages sharing a `received_at` across a page boundary will appear on neither
page. There is no total and no offset.

```bash
curl -G http://localhost:3000/api/v1/inbound-emails \
  -H "Authorization: Bearer myk_..." \
  --data-urlencode "limit=50" \
  --data-urlencode "status=received"
```

## Counts

```
GET /api/v1/inbound-emails/stats
```

```json
{ "counts": { "received": 812, "rejected": 40, "failed": 3 } }
```

## Get an Inbound Email

```
GET /api/v1/inbound-emails/{id}
```

`{id}` is the inbound email UUID. Returns the full record:

```json
{
    "inbound_email": {
        "id": "550e8400-e29b-41d4-a716-446655440000",
        "project_id": "81af718e-f0ae-4780-a0d7-9f05b34dabcc",
        "domain_id": "3f2504e0-4f89-11d3-9a0c-0305e82c3301",
        "message_id": "abc123@example.com",
        "sender": "sender@example.com",
        "recipients": [
            "inbox@yourdomain.com"
        ],
        "subject": "Hello",
        "text_body": "Plain text body",
        "html_body": "<p>HTML body</p>",
        "headers": {
            "Received": "from mail.example.com ..."
        },
        "attachments": [
            {
                "filename": "invoice.pdf",
                "content_type": "application/pdf",
                "size": 18244
            }
        ],
        "auth": {
            "spf": "pass",
            "dkim": "pass",
            "dmarc": "pass",
            "aligned": true,
            "client_ip": "203.0.113.10"
        },
        "has_raw": true,
        "size": 2048,
        "status": "received",
        "received_at": "2026-01-01T00:00:00Z",
        "created_at": "2026-01-01T00:00:00Z"
    }
}
```

`auth` carries the SPF, DKIM and DMARC verdicts stamped at ingest. `aligned` is the field worth acting on - a valid
signature from some other domain is not authentication. See [Receiving](/docs/inbound/receiving).

Attachment entries carry metadata only. Fetch the bytes through the download endpoint below.

## Delete an Inbound Email

```
DELETE /api/v1/inbound-emails/{id}
```

Removes the record and best-effort deletes its blob-stored raw message and attachments. Returns `204 No Content`.

## Re-dispatch

```
POST /api/v1/inbound-emails/{id}/retry
```

```json
{ "emitted": true }
```

{{< callout type="info" title="This re-sends the webhook, it does not re-process the mail" >}}
The name says retry, and what it actually does is emit the `inbound.received`
[webhook event](/docs/webhooks/event-types) again for a message already stored. The message is not re-parsed and its
status does not change.

It works on **any** stored message regardless of status, and it is not conditional on anything having failed. So it is
the tool for "my receiver was down when that arrived" — replaying the notification — and not for recovering a `failed`
parse, which nothing re-runs.

`emitted: true` means the event was handed to the dispatcher, not that your endpoint answered. Check
[delivery tracking](/docs/webhooks/delivery-tracking) for that.
{{< /callout >}}

## Download the Raw Message

```
GET /api/v1/inbound-emails/{id}/raw
```

Streams the raw RFC 5322 message as `message/rfc822` with a `.eml` filename. Returns `404` if the raw bytes were not
stored.

## Download an Attachment

```
GET /api/v1/inbound-emails/{uuid}/attachments/{idx}
```

Streams an attachment by zero-based index for an inbound email you own. The response uses the attachment's original
`Content-Type` and filename. Where the bytes are actually stored depends on `MAILYARD_STORAGE_BACKEND`, but this
endpoint reads them the same way either way.

## Live updates

The console shows inbound arrivals as they land, over its own event feed. To be notified machine-side, subscribe to
`inbound.received` with a [webhook](/docs/webhooks/overview). It carries metadata only - no bodies or attachments -
so fetch those with the endpoints above. See [Event Types](/docs/webhooks/event-types) for the payload.

