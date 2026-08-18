---
title: "Receiving Email"
description: "How the MX listener accepts mail, authenticates it, and stores attachments"
weight: 20
---

Mailyard receives mail directly over SMTP. There is no ingestion webhook and no upstream provider in the path — a
sending mail server connects to Mailyard's listener the same way it would connect to any other MX.

## Pointing an MX at Mailyard

1. **Enable the listener.** `MAILYARD_INBOUND_ENABLED=true`. It binds
   `MAILYARD_INBOUND_ADDR`, which defaults to `:25`.
2. **Verify the domain** you want to receive for, under *Domains*. Mail for an unverified domain is refused.
3. **Publish the MX record**, pointing at the host running Mailyard:

   ```
   example.com.   IN  MX  10  mail.example.com.
   mail.example.com.  IN  A   198.51.100.10
   ```

An MX record carries a hostname and a priority, never a port — remote servers always connect on 25. If Mailyard cannot
bind a privileged port in your environment, bind a high one and forward 25 to it, see
[Privileged ports](/docs/security/smtp-submission#privileged-ports).

Set `MAILYARD_INBOUND_HOSTNAME` to the name in the MX record so the `EHLO`
greeting matches. Some receivers care, and it is what appears in the `Received`
headers of anything you later forward.

## What happens during the SMTP conversation

The listener does not authenticate its clients — that is what an MX is. What protects it is the recipient gate:

- **`RCPT TO` is checked against verified domains.** A recipient on a domain no project has claimed is refused with a
  `550`, during the conversation, before the message body is transferred. This is what stops the listener being an open
  relay.
- **The sender is checked against the project suppression list.** A suppressed sender is refused.
- **`MAILYARD_INBOUND_MAX_MESSAGE_SIZE`** is advertised in the `SIZE` extension and enforced, so an oversized message is
  rejected rather than buffered.
- **`MAILYARD_INBOUND_RATE_PER_MINUTE`** caps sessions per client IP.

## Authentication results

Every accepted message is checked for SPF, DKIM and DMARC. The verdict is stored on the record and stamped into an
`Authentication-Results` header.

The field worth acting on is `aligned`. A message can carry a perfectly valid DKIM signature belonging to some unrelated
domain — that is not authentication of the `From` address, and only alignment says the signing domain and the visible
sender agree.

Mailyard refuses a message on authentication failure only when **both** conditions hold: the sender's domain publishes
`p=reject`, and
`MAILYARD_INBOUND_REJECT_ON_DMARC_FAIL=true`. The default is to accept and record, because silently dropping mail is the
most damaging thing a receiver can do, and ordinary forwarding breaks SPF as a matter of course. Look at what your real
traffic scores before turning refusal on.

## Deduplication

A message is deduplicated per project by `Message-ID`. When a message carries none, a content hash stands in. A
redelivery — which is normal SMTP behaviour after a timeout — is stored once.

## Reading received mail

Received messages are available through the console API:

```
GET    /api/v1/inbound-emails                     list, paginated
GET    /api/v1/inbound-emails/:id                 one message with parsed bodies
GET    /api/v1/inbound-emails/:id/raw             the original .eml
GET    /api/v1/inbound-emails/:id/attachments/:idx  stream one attachment
POST   /api/v1/inbound-emails/:id/retry           re-emit the webhook (editor)
DELETE /api/v1/inbound-emails/:id                 delete, with its blobs (editor)
```

The machine API exposes the read subset at `/api/v1/inbound-emails`,
`/api/v1/inbound-emails/stats` and `/api/v1/inbound-emails/:id` for API keys holding `inbound:read`.

Attachments stream with their original `Content-Type` and a
`Content-Disposition` filename. Where the bytes actually live depends on
`MAILYARD_STORAGE_BACKEND` — inline in the database by default, or in the filesystem or S3 — but the download endpoint
is the same either way.

## Being told about new mail

Rather than polling the list endpoint, subscribe a webhook to the
`inbound.received` [event](../webhooks/event-types.md). That is the only push notification for received mail — the
console's own live feed carries outbound delivery results, not arrivals.

See [Managing Inbound Email](managing.md).
