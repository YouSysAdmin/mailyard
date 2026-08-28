---
title: "Single Email"
description: "Send a single email via the API"
weight: 10
---

Send a single email with HTML/text content, optional attachments, custom headers, and scheduled delivery.

{{< callout type="warning" title="The From domain must be verified by this project" >}}
A send is refused with a `400` unless the domain in `from` has been verified under
[Domains](/docs/smtp-domains/domain-verification) **by the project making the request**. That holds for every surface —
this endpoint, templates, batches, campaigns, and the SMTP relay — and it is checked again at delivery, so a scheduled
message will not go out on a domain that was unverified while it waited.

The project comparison is the point. Domain names are globally unique across an install, so without it any project could
put a neighbour's domain in `from`. A domain nobody has verified and a domain another project owns give the same
message, which is deliberate: the answer must not reveal which.

A verified domain covers its **subdomains**: verify `example.com` and
`news@mail.example.com` sends. Whoever controls a zone controls every name under it. Matching is by whole labels, so
`evilexample.com` is not covered.

Subdomain mail is DKIM-signed with the verified ancestor's key, meaning
`d=example.com`. That aligns under DMARC's default relaxed policy. If you publish
`adkim=s` or `aspf=s`, verify the subdomain separately so it gets a key of its own.
{{< /callout >}}

{{< callout type="tip" title="Machine API schema" >}}
The `/api/v1` surface describes itself: run
[`mailyard export-api-spec`](/docs/security/api-keys#openapi-description) and feed the document to Swagger UI, Postman,
or a client generator.
{{< /callout >}}

## Example

```bash
curl -X POST http://localhost:3000/api/v1/emails/send \
  -H "Authorization: Bearer myk_..." \
  -H "Content-Type: application/json" \
  -d '{
    "from": "billing@example.com",
    "to": ["jane@customer.example"],
    "subject": "Receipt for order 4471",
    "html": "<p>Thanks, Jane. Your receipt is below.</p>",
    "text": "Thanks, Jane. Your receipt is below.",
    "headers": {
      "X-Order-Ref": "4471"
    },
    "list_unsubscribe_url": "https://example.com/mail/stop",
    "list_unsubscribe_post": true
  }'
```

## What is required

Four things, and the request is refused with a `400` naming the one that is wrong:

- **`from`**, a parseable address whose domain this project has verified
- **`to`**, at least one parseable address, within the installation's recipient ceiling
- **`subject`**, non-empty
- **a body** — `html` or `text`, or both

Sending both parts is worth the extra field. Clients that cannot render HTML fall back to the text part, and a message
with no text alternative scores worse with spam filters than one that has it.

## Optional fields

| Field | Does |
|---|---|
| `reply_to` | Where a reply lands when it should not go back to `from`. Any parseable address, verified or not |
| `headers` | Up to 20 custom headers |
| `attachments` | Base64 files — see [Attachments](/docs/email-sending/attachments) |
| `send_at` | Hold until an RFC 3339 time — see [Scheduled Email](/docs/email-sending/scheduled-email) |
| `dry_run` | Run every validation and persist nothing |
| `disable_tracking` | Opt this message out of open and click tracking |
| `unsubscribe_list_id` | Send under a transactional [opt-out scope](/docs/contacts/unsubscribe-lists) |
| `list_unsubscribe_url`, `list_unsubscribe_mailto`, `list_unsubscribe_post` | Carry your own opt-out targets |
| `smtp_group`, `smtp_server_id` | Pin the [route out](/docs/smtp-domains/server-groups) |
| `sandbox`, `sandbox_retention_days` | Capture instead of delivering — see [Sandbox](/docs/email-sending/sandbox) |

`dry_run` is the cheapest way to check an integration: it validates the sender, the recipients, the headers, the
attachment sizes and the routing, then returns without writing a row or spending quota.

{{< callout type="warning" title="Sixteen headers are reserved" >}}
Anything the message builder owns is refused rather than merged, so a caller cannot forge the envelope or break the
MIME structure: `From`, `To`, `Cc`, `Bcc`, `Reply-To`, `Subject`, `Date`, `MIME-Version`, `Content-Type`,
`Content-Transfer-Encoding`, `List-Unsubscribe`, `List-Unsubscribe-Post`, `Return-Path`, `Message-ID`, `Received` and
`DKIM-Signature`.

The refusal names the header and, where there is one, the field to use instead — `List-Unsubscribe` points you at
`list_unsubscribe_url`, `Reply-To` at `reply_to`. Matching is case-insensitive, and a header name or value containing a newline is refused
outright, which is what stops header injection through a value you interpolated.
{{< /callout >}}

## What comes back

`201`, with the stored record and anything that was dropped:

```json
{
  "email": {
    "id": "0198f6a1-3c7e-7b21-9f4d-2a5c8e0b1d33",
    "status": "queued",
    "recipients": ["jane@customer.example"],
    "subject": "Receipt for order 4471"
  },
  "suppressed_recipients": []
}
```

**Read `suppressed_recipients`.** Addresses on the project's
[suppression list](/docs/contacts/suppression-list) are removed before sending and named here. The call still succeeds —
suppression is a standing instruction, not an error — so an address silently missing from a delivery is usually here.

If **every** recipient was suppressed, the row is still created, with status `suppressed` and an error message saying
so. Nothing leaves the building.

## After it is accepted

The message is queued and a worker picks it up, normally within a second. Follow it with
[the status route](/docs/email-sending/email-status), which also explains what each state means and why `sent` is not
the same as delivered.

