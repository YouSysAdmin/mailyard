---
title: "Event Types"
description: "Available webhook event types"
weight: 20
---

Seven events exist, and a subscription to anything else is refused with `400` when the webhook is created.

## Email Events

| Event              | Fires when                                                          |
|--------------------|---------------------------------------------------------------------|
| `email.queued`     | The message was accepted and written to the delivery queue          |
| `email.sent`       | It was handed to an SMTP server successfully                        |
| `email.failed`     | Delivery failed and no attempt remains                              |
| `email.suppressed` | Every recipient was on a suppression list, so nothing was attempted |

## Campaign Events

| Event                | Fires when                                             |
|----------------------|--------------------------------------------------------|
| `campaign.started`   | The audience was fanned out into the queue             |
| `campaign.completed` | Every message in the campaign reached a terminal state |

## Inbound Events

| Event              | Fires when                              |
|--------------------|-----------------------------------------|
| `inbound.received` | A message arrived for a verified domain |

## The Envelope

**Every** delivery has the same three top-level fields, and the per-event fields are inside `data` - never at the top
level:

```json
{
    "event": "email.sent",
    "timestamp": "2026-01-01T00:00:01Z",
    "data": {}
}
```

`timestamp` is when the event was emitted, RFC 3339 in UTC. The body is signed with the webhook's secret -
see [Signature Verification](/docs/webhooks/overview#signature-verification).

## Email Payloads

`email.queued`, `email.sent`, `email.failed` and `email.suppressed` all carry the **same** `data` shape, so one handler
can serve all four and switch on `event` or on `data.status`:

```json
{
    "event": "email.failed",
    "timestamp": "2026-01-01T00:00:01Z",
    "data": {
        "id": "019ffb19-e600-7b7d-b8dd-17cff629d5c6",
        "project_id": "019ffb18-b5d6-7488-9c5e-4373a8c062dc",
        "sender": "noreply@example.com",
        "recipients": [
            "recipient@customer.example"
        ],
        "subject": "Your receipt",
        "template_name": "receipt",
        "status": "failed",
        "error_message": "smtp dial failed: dial tcp 203.0.113.9:587: connect: connection refused",
        "attempts": 3,
        "created_at": "2026-01-01T00:00:00Z",
        "sent_at": null
    }
}
```

| Field           | Notes                                                                                                                       |
|-----------------|-----------------------------------------------------------------------------------------------------------------------------|
| `id`            | The email id. `GET /api/v1/emails/{id}` returns the full record, bodies included.                                           |
| `recipients`    | Every recipient of this message, as an array. **This is the address you want** - `id` alone means nothing outside Mailyard. |
| `sender`        | The From address as accepted, which may carry a display name.                                                               |
| `template_name` | Empty string when the message was not sent from a template.                                                                 |
| `status`        | `queued`, `sent`, `failed` or `suppressed`. Matches the event.                                                              |
| `error_message` | Why delivery failed. Empty on the non-failure events.                                                                       |
| `attempts`      | Attempts made. On `email.failed` this is the last one.                                                                      |
| `sent_at`       | `null` until the message is sent.                                                                                           |

{{< callout type="note" title="No per-recipient event" >}}
One message produces one event, whatever the recipient count - `recipients` is an array for that reason. A message that
failed for one address and succeeded for another is not representable here; use one send per recipient when you need
that distinction.
{{< /callout >}}

## Campaign Payloads

```json
{
    "event": "campaign.completed",
    "timestamp": "2026-01-01T00:00:01Z",
    "data": {
        "id": "a1f5c3d2-6b48-4e97-9c21-3d8e7f0a5b64",
        "project_id": "019ffb18-b5d6-7488-9c5e-4373a8c062dc",
        "name": "Spring Newsletter",
        "from_email": "news@example.com",
        "list_id": "7c9e6679-7425-40de-944b-e07fc1f90ae7",
        "counts": {
            "sent": 1180,
            "failed": 12,
            "suppressed": 8
        }
    }
}
```

`counts` differs between the two events, and that is the only difference in shape:

- On `campaign.started` it is `{"recipients": N}` - how many messages were queued.
- On `campaign.completed` it is the per-status totals, and a status with no messages is absent rather than zero.

## Inbound Payload

```json
{
    "event": "inbound.received",
    "timestamp": "2026-01-01T00:00:01Z",
    "data": {
        "id": "7d3f9a12-4b2c-7e81-9a03-5f6d8c1e2b47",
        "domain": "mail.example.com",
        "sender": "sender@example.com",
        "recipients": [
            "inbox@mail.example.com"
        ],
        "subject": "Hello",
        "message_id": "<unique@mail.example.com>",
        "size": 14200,
        "received_at": "2026-01-01T00:00:00Z"
    }
}
```

Metadata only: **no bodies, no headers and no attachments**. A received message can be tens of megabytes, and a webhook
body that size is a delivery that times out rather than a convenience. Fetch what you need with
`GET /api/v1/inbound-emails/{id}`, which returns the parsed message, and its attachment routes for the content.

`message_id` is the RFC 5322 header from the message itself, not one of ours, and it is empty when the sender did not
set one.

## Filters

A webhook may narrow its deliveries to matching sender addresses - exact (`billing@example.com`) or per-domain
(`*@example.com`), matched case-insensitively against the bare address with any display name stripped.

The field matched is the **sender**, and which address that is depends on the direction:

- On the email events it is the From address of the message you sent.
- On the campaign events it is the campaign's own from address.
- On `inbound.received` it is **the address that mailed you**, which is somebody else's. A filter of `*@example.com` on
  a webhook subscribed to inbound therefore means "only mail arriving from example.com", not "only mail to my
  example.com domain".

A message with no sender at all - a null return path, which is what a bounce report carries - passes every filter rather
than none.

