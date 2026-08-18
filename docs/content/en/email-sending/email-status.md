---
title: "Email Status"
description: "Track email delivery status"
weight: 60
---

Every send answers with an id. This route turns that id back into delivery state, without the bodies, headers or
attachments the full record carries — it is the cheap call to poll.

```
GET /api/v1/emails/{id}/status
```

```bash
curl http://localhost:3000/api/v1/emails/0198f6a1-3c7e-7b21-9f4d-2a5c8e0b1d33/status \
  -H "Authorization: Bearer myk_..."
```

```json
{
  "id": "0198f6a1-3c7e-7b21-9f4d-2a5c8e0b1d33",
  "status": "sent",
  "attempts": 1,
  "error_message": "",
  "sent_at": "2026-01-01T00:00:01Z"
}
```

Five fields, and that is the whole response. Use `GET /api/v1/emails/{id}` when you need the message itself.

{{< callout type="tip" title="Machine API schema" >}}
The `/api/v1` surface describes itself: run
[`mailyard export-api-spec`](/docs/security/api-keys#openapi-description) and feed the document to Swagger UI, Postman,
or a client generator.
{{< /callout >}}

## The states

| Status | Meaning | Moves on |
|---|---|---|
| `queued` | Accepted and waiting for a worker | When a worker claims it |
| `scheduled` | Held for a future `send_at` | At that time |
| `processing` | Claimed and being handed to SMTP | Within one attempt |
| `sent` | An SMTP server accepted it | Terminal |
| `failed` | Every attempt was spent | Terminal, unless retried |
| `suppressed` | Every recipient was blocked before sending | Terminal |

A seventh value, `pending`, is accepted as a filter on the [email log](/docs/email-sending/email-log) for historical
reasons and is never written. Filtering on it always returns nothing.

{{< callout type="warning" title="`sent` means accepted, not delivered" >}}
It records that a receiving SMTP server took the message. What happens after that — a mailbox that is full, a spam
folder, a delayed bounce — arrives separately, as a [bounce](/docs/contacts/bounce-handling) or a
[webhook](/docs/webhooks/event-types). A message can be `sent` and bounced at the same time, and both facts are true.
{{< /callout >}}

Between `queued` and `sent` a message may be tried against several SMTP servers. `attempts` counts attempts, not
servers — a single attempt can walk a whole [failover chain](/docs/smtp-domains/server-groups) before it gives up.

## Retry

```
POST /api/v1/emails/{id}/retry
```

Only a `failed` message can be retried. Anything else is refused, naming the state it is actually in:

```
only failed emails can be retried (status is "sent")
```

{{< callout type="info" title="Retry resets the attempt counter" >}}
The message goes back to `queued` with `attempts` set to **zero** and the error message cleared, so it gets a full set
of tries again rather than resuming where it left off.

That is what you want after fixing the cause — new credentials, a corrected DNS record. It is not a way to nudge a
message that failed for a permanent reason, which will simply spend the whole budget again.
{{< /callout >}}

The response is the full email record, not the status summary. The worker is woken immediately, so a healthy queue
picks the message up in the same second rather than at the next poll.

## Polling

For a transactional send the interesting window is short — most messages leave `queued` within seconds. Poll this route
every few seconds while the status is `pending`, `queued` or `processing`, and stop once it is not.

Do not poll a `scheduled` message. It is waiting on purpose, possibly for days, and the answer is the `send_at` you
already know. The console makes the same distinction for the same reason.

For anything longer-lived, [webhooks](/docs/webhooks/overview) push the transitions instead of you asking for them.
