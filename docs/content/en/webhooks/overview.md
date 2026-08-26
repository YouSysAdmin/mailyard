---
title: "Overview"
description: "Real-time webhooks for email events"
weight: 10
---

A webhook is a URL of yours that Mailyard POSTs to when something happens to your mail. It is how you learn that a
message was delivered or failed without polling for it.

## Create one

```
POST /api/v1/webhooks
```

```bash
curl -X POST http://localhost:3000/api/v1/webhooks \
  -H "Authorization: Bearer myk_..." \
  -H "Content-Type: application/json" \
  -d '{
    "url": "https://your-app.example/hooks/mailyard",
    "events": ["email.sent", "email.failed"],
    "filters": ["*@billing.example.com"]
  }'
```

| Field | Notes |
|---|---|
| `url` | Required. Must be an `http` or `https` URL, up to 2048 characters |
| `events` | Required. Between 1 and 10 [event types](/docs/webhooks/event-types), or `*` for all |
| `filters` | Optional. Up to 20 sender addresses or domain patterns |

{{< callout type="warning" title="The signing secret is returned once" >}}
Mailyard generates it and puts it in the **create response only**. Every later read omits it, and there is no route that
will show it to you again — losing it means deleting the webhook and creating another.

It is stored sealed rather than hashed. A hash would leave nothing to sign with: the dispatcher needs the same value
your receiver verifies against.
{{< /callout >}}

## Narrowing by sender

`filters` restricts deliveries to messages from particular senders. An empty list means everything.

| Pattern | Matches |
|---|---|
| `billing@example.com` | That address exactly |
| `*@example.com` | Any address at that domain |

Matching is case-insensitive and runs against the bare envelope address, so a `From` of
`Billing <billing@Example.com>` matches both patterns above.

This is how one project runs separate receivers for separate concerns — transactional mail to one endpoint, campaign
mail to another — without either having to filter the other's traffic on arrival.

## The payload

Every event uses the same envelope: `event`, `timestamp`, and the per-event fields under `data`.

```json
{
  "event": "email.failed",
  "timestamp": "2026-01-01T00:00:01Z",
  "data": {
    "id": "0198f6a1-3c7e-7b21-9f4d-2a5c8e0b1d33",
    "project_id": "0198f6a0-9d12-7f33-a1b8-6c4e2f8a0d57",
    "sender": "noreply@example.com",
    "recipients": ["recipient@customer.example"],
    "subject": "Your receipt",
    "status": "failed",
    "error_message": "smtp dial failed: connection refused",
    "attempts": 3
  }
}
```

Read the recipient from `data.recipients`. `data.id` identifies the message inside Mailyard and means nothing to your
application — if you need to correlate, put your own identifier in a custom header on the send and read it back from the
[email log](/docs/email-sending/email-log).

See [Event Types](/docs/webhooks/event-types) for the fields each event carries.

## Verifying the signature

Every delivery carries a timestamp and an HMAC-SHA256 over the timestamp and the **raw request body**:

```
X-Mailyard-Timestamp: 1756230000
X-Mailyard-Signature: sha256=<hex>
```

The signed string is `<timestamp> + "." + <body>`. Note the `sha256=` prefix — it is part of the header value, not a
description of it.

```go
func valid(secret string, timestamp string, body []byte, header string) bool {
    ts, err := strconv.ParseInt(timestamp, 10, 64)
    if err != nil || time.Since(time.Unix(ts, 0)).Abs() > 5*time.Minute {
        return false
    }

    mac := hmac.New(sha256.New, []byte(secret))
    mac.Write([]byte(timestamp))
    mac.Write([]byte("."))
    mac.Write(body)
    want := "sha256=" + hex.EncodeToString(mac.Sum(nil))

    return hmac.Equal([]byte(want), []byte(header))
}
```

Three things that matter more than they look:

- **Check the timestamp.** It is inside the signed string, so refusing one older than a few minutes refuses a replayed
  delivery with it. Five minutes leaves room for clock drift; Mailyard's retries send a fresh timestamp each time.
- **Compare in constant time.** `hmac.Equal`, or your language's equivalent — a plain `==` returns as soon as two bytes
  differ, which leaks how much of a forged signature was right.
- **Sign the bytes you received**, before any JSON parsing and re-encoding. Round-tripping through a decoder reorders
  keys and changes whitespace, and the signature will not match.

Reject anything that fails. A request without a valid signature is not from Mailyard.

### Rotating the secret

```
POST /api/v1/webhooks/{id}/rotate-secret
```

Returns the webhook with a fresh `secret`, shown once like the original. Deliveries are signed with the new secret from
the next one on, so have the receiver verify against both for the length of the changeover, then drop the old one.

## Retries

Delivery is attempted `webhook.max_attempts` times (3 by default, **including** the first), with a fixed
`webhook.retry_delay` between attempts (10s) and each request bounded by `webhook.timeout` (10s). Any `2xx` is success.
Redirects are not followed.

Every attempt is recorded — see [Delivery Tracking](/docs/webhooks/delivery-tracking), which also covers what happens
when the attempts run out.

## Manage

```
GET    /api/v1/webhooks
DELETE /api/v1/webhooks/{id}
```

There is no update route. Change a URL or an event list by creating a new webhook and deleting the old one, so the
secret does not stay valid for an endpoint that has moved. A secret on its own is rotated in place - see above.

{{< callout type="warning" title="Private network targets are refused" >}}
By default a webhook URL cannot resolve to loopback, RFC 1918, or other reserved address space. URLs are chosen by
project members, and without that guard one of them could aim a delivery at your cloud metadata service and use this
process as a proxy into your private network.

`webhook.allow_private_targets` lifts it, for receivers that genuinely are on the same network. Turning it on reopens
that path to anyone who can create a webhook.
{{< /callout >}}
