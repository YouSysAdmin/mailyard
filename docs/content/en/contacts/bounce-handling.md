---
title: "Bounce Handling"
description: "Track and manage email bounces"
weight: 30
---

A bounce is a recipient a message could not be delivered to. Mailyard keeps two separate things about one: a **bounce
record**, which is history, and a **suppression**, which is what actually stops future mail. Confusing them is the usual
reason an address stays blocked after somebody thought they had unblocked it.

## Types

| Type | Means | Suppresses |
|---|---|---|
| `hard` | A permanent failure — the mailbox does not exist | Yes |
| `soft` | A temporary failure — mailbox full, server busy | No |
| `complaint` | The recipient marked it as spam | Yes |

A soft bounce is recorded and nothing more. It may resolve on its own, and suppressing an address because a mailbox was
briefly full would lose you a real customer.

## Where bounces come from

Three routes, and they do not behave alike:

1. **The delivery path.** A permanent SMTP rejection during sending is recorded by the worker directly.
2. **A return path.** A DSN arriving at the address messages were sent from, or an
   [SES notification](/docs/smtp-domains/ses-notifications). These are attributed strictly: the report must name a
   message this project sent, via the `X-Mailyard-Email-Id` header, **and** a recipient that message actually went to.
   Anything that cannot be attributed is logged and dropped rather than filed against the wrong tenant.
3. **The ingest endpoint below**, for a feedback loop you run yourself.

## Recording one yourself

```
POST /api/v1/webhooks/bounce
```

Needs an API key with `bounces:write`. The key decides the project, so there is no header to set.

| Field | Required | Notes |
|---|---|---|
| `recipient` | **Yes** | The bounced address. Trimmed and lowercased |
| `email_id` | No | The message this relates to, if you know it |
| `type` | No | `hard`, `soft` or `complaint`. **Defaults to `hard`** |
| `reason` | No | Free text, up to 1000 characters — usually the SMTP diagnostic |

```bash
curl -X POST http://localhost:3000/api/v1/webhooks/bounce \
  -H "Authorization: Bearer myk_..." \
  -H "Content-Type: application/json" \
  -d '{
    "recipient": "j.okafor@acme-industrial.example",
    "email_id": "0198f6a1-3c7e-7b21-9f4d-2a5c8e0b1d33",
    "type": "hard",
    "reason": "550 5.1.1 recipient address rejected: user unknown"
  }'
```

Answers `201` with the record and whether it caused a block:

```json
{
  "bounce": { "id": "...", "recipient": "j.okafor@acme-industrial.example", "type": "hard" },
  "suppressed": true
}
```

{{< callout type="warning" title="`type` defaults to `hard`, which suppresses" >}}
Omitting the field is not "unknown" — it is a permanent failure, and the address is added to the suppression list on
the spot. If you are forwarding reports whose classification you are unsure of, send `"type": "soft"` explicitly.

This endpoint takes your word for it. Unlike the return-path route, it does **not** check that `email_id` names a real
message or that the address was ever mailed — the API key established which project this belongs to, and everything
after that is trusted. Treat the credential accordingly.
{{< /callout >}}

## Listing

```
GET /api/v1/bounces?limit=50&type=hard&search=acme-industrial.example
```

Keyset paged, newest first: follow `next_cursor` until it comes back empty. `type` filters to one class and `search`
matches the recipient.

Each row carries `id`, `email_id`, `recipient`, `type`, `reason` and `created_at`.

## Clearing history

```
DELETE /api/v1/bounces?email=j.okafor@acme-industrial.example
```

Removes every bounce record for that address in this project.

{{< callout type="warning" title="This does not unblock the address" >}}
Delivery is stopped by the **suppression**, not by these rows. Clearing the history and then sending will still produce
`suppressed` — you have to remove the suppression too, through
`DELETE /api/v1/suppressions`.

The two are separate on purpose. The case this exists for is a customer list where some mailboxes have not been created
yet: they bounce, they are created a week later, and the address needs to stop reading as previously-bounced to
[verification](/docs/email-sending/email-verification) without anyone silently lifting a block that a complaint might
have put there.
{{< /callout >}}

## What a suppression does

Once an address is suppressed, every send to it is dropped before delivery. The message is still recorded — with the
address in `suppressed_recipients`, or with status `suppressed` if it was the only recipient — so it is visible rather
than silently missing. See [Suppression List](/docs/contacts/suppression-list).
