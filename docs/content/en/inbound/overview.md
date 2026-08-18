---
title: "Overview"
description: "Receive, store, and forward inbound email with Mailyard"
weight: 10
---

Inbound email lets Mailyard receive messages addressed to one of your verified domains, store them, and notify your
application. Mailyard is the MX: it runs its own SMTP listener and accepts mail directly from the sending server. There
is no upstream provider to sign up with and no ingestion webhook to POST to.

{{< callout type="note" title="Or a relay node is the MX" >}}
A [relay node](/docs/smtp-domains/relay-nodes) - enterprise edition - can run the mail exchanger instead and forward
what it receives over its own outbound HTTP. That is for a network Mailyard cannot be reached from - mail sent into a
region behind a national firewall, whose bounces cannot cross it. Everything below is unchanged: the node holds no
database and decides nothing, so the same verified-domain gate, the same authentication and the same dedup run here
either way.
{{< /callout >}}

## Enabling Inbound

Inbound email is off by default. Enable it with configuration:

| Variable                                | Default             | Description                                                                                                                                                                  |
|-----------------------------------------|---------------------|------------------------------------------------------------------------------------------------------------------------------------------------------------------------------|
| `MAILYARD_INBOUND_ENABLED`              | `false`             | Master switch. Starts the SMTP listener.                                                                                                                                     |
| `MAILYARD_INBOUND_ADDR`                 | `:25`               | Bind address. 25 is where MX delivery arrives, since an MX record has nowhere to put a port number. See [Privileged ports](/docs/security/smtp-submission#privileged-ports). |
| `MAILYARD_INBOUND_HOSTNAME`             | `mailyard`          | Hostname announced in the SMTP `EHLO` greeting.                                                                                                                              |
| `MAILYARD_INBOUND_MAX_MESSAGE_SIZE`     | `26214400` (25 MiB) | Maximum raw message size in bytes.                                                                                                                                           |
| `MAILYARD_INBOUND_RATE_PER_MINUTE`      | `120`               | Per-IP session cap per minute. `0` disables it.                                                                                                                              |
| `MAILYARD_INBOUND_REJECT_ON_DMARC_FAIL` | `false`             | Refuse mail whose From domain publishes `p=reject` and which nothing vouched for.                                                                                            |

Point your domain's MX record at the host running this listener, then verify the domain so mail for it is accepted.

{{< callout type="note" >}}
A message is only accepted if at least one recipient domain matches an
ownership-verified [domain](../smtp-domains/domain-verification.md) in your account. The check happens at `RCPT TO`, so
mail for an unclaimed domain is refused during the SMTP conversation and never enters your project. This listener has no
authentication — the verified-domain gate is what stops it being an open relay.
{{< /callout >}}

## How It Works

```
sending MTA  ──►  SMTP on :25  ──►  RCPT TO checked against verified domains
                                          │
                                          ▼
                                    stored (InboundEmail)
                                          │
                                          └─►  inbound.received webhook  ──►  your endpoint
```

1. A remote mail server connects and offers a message. Recipients are checked at `RCPT TO` against domains verified to a
   project, and anything unclaimed is refused there.
2. Mailyard authenticates the sender (SPF, DKIM, DMARC), stores the verdict on the record and stamps an
   `Authentication-Results` header.
3. The message is parsed, deduplicated by Message-ID and content hash, and persisted (raw `.eml` and attachments go to
   blob storage when configured).
4. The record is dispatched asynchronously as an `inbound.received` [webhook event](../webhooks/event-types.md) to
   subscribed project webhooks.
5. You can browse, download and delete inbound mail from the project management API.

## Statuses

| Status     | Meaning                                                             |
|------------|---------------------------------------------------------------------|
| `received` | Stored and parsed.                                                  |
| `rejected` | Refused at ingest, for instance a suppressed sender or a duplicate. |
| `failed`   | The MIME tree could not be parsed. The raw message is kept.         |

## Next Steps

- [Receiving Email](receiving.md) — the listener, authentication and attachment downloads.
- [Managing Inbound Email](managing.md) — list, fetch, retry, export, and stream from your project.

## Replying

Received mail has a **Reply** button. It opens the compose form addressed back to the sender, from the address they
wrote to, with the original quoted below the cursor and `In-Reply-To` and `References` set from the message id.

The last part is what makes the answer land in the thread they started rather than as a new conversation - which matters
most in the case that produces this mail in the first place: somebody writing to a `no-reply` address because it was the
only one they had.

The From address is prefilled, not forced. It still has to be one this project may send as -
see [Domain Verification](/docs/smtp-domains/domain-verification) - so a
`no-reply@` on a domain you have verified works, and one on a domain you have not is refused with a message saying so.
