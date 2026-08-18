---
title: "Batch Email"
description: "Send many messages in one call"
weight: 30
---

One request, up to **100 messages**, each with its own recipients and its own data. This is the shape for a run of
notifications that differ per person — a digest, a set of receipts — where a hundred separate HTTP calls would be the
only other option.

```
POST /api/v1/emails/batch
```

Each item becomes an independent email with its own id, its own row in the log and its own delivery. A batch is a way
of submitting them together, not a message with many recipients.

{{< callout type="tip" title="Machine API schema" >}}
The `/api/v1` surface describes itself: run
[`mailyard export-api-spec`](/docs/security/api-keys#openapi-description) and feed the document to Swagger UI, Postman,
or a client generator.
{{< /callout >}}

## Template mode

Name a template once, and let each item supply the data it renders against:

```bash
curl -X POST http://localhost:3000/api/v1/emails/batch \
  -H "Authorization: Bearer myk_..." \
  -H "Content-Type: application/json" \
  -d '{
    "from": "news@example.com",
    "template_name": "monthly-digest",
    "language": "en",
    "items": [
      { "to": ["bob@example.com"],   "data": {"name": "Bob",   "plan": "Pro"} },
      { "to": ["carol@example.fr"],  "language": "fr", "data": {"name": "Carol", "plan": "Enterprise"} },
      { "to": ["dave@example.com"],  "data": {"name": "Dave",  "plan": "Free"} }
    ]
  }'
```

The top-level `language` is the default. An item naming its own overrides it, and each item resolves through the
[usual four steps](/docs/email-sending/template-email#which-language-goes-out) independently.

## Raw mode

Leave the template ref out and each item carries its own content:

```json
{
  "from": "alerts@example.com",
  "items": [
    { "to": ["ops@example.com"], "subject": "Disk 91% on db-2", "text": "Threshold crossed at 14:02 UTC." },
    { "to": ["oncall@example.com"], "subject": "Disk 91% on db-2", "html": "<p>Threshold crossed at 14:02 UTC.</p>" }
  ]
}
```

## What an item may carry

| Field | Notes |
|---|---|
| `to` | Required, one or more addresses |
| `data` | Template mode — the render values |
| `language` | Overrides the batch default |
| `subject`, `html`, `text` | Raw mode |
| `list_unsubscribe_url`, `list_unsubscribe_mailto`, `list_unsubscribe_post` | Per item, because an opt-out link identifies a recipient |

The opt-out fields are per item deliberately. A batch is where an application sends its bulk mail, and one link shared
across a hundred items would unsubscribe whoever clicked it from nothing in particular.

`from` belongs to the batch, not the item. `headers`, `attachments` and `send_at` are not available here — use
individual sends when you need them.

## What comes back

`200`, always, with the outcome of every item. One bad item does not sink the rest, so a failure is reported in the body
rather than as a status code:

```json
{
  "total": 3,
  "accepted": 2,
  "results": [
    { "index": 0, "email_id": "0198f6a1-3c7e-7b21-9f4d-2a5c8e0b1d33", "status": "queued" },
    { "index": 1, "email_id": "0198f6a1-3c80-7c44-b6e1-9d2f7a0c5188", "status": "queued",
      "suppressed_recipients": ["carol@example.fr"] },
    { "index": 2, "error": "template render failed: ... map has no entry for key \"plan\"" }
  ]
}
```

- `index` is the item's position in what you sent, counting from zero.
- `accepted` counts the items with no `error`, which is what you compare against `total`.
- `suppressed_recipients` names addresses dropped before sending. An item whose recipients were **all** suppressed still
  gets a row and an id, with status `suppressed`.
- An item that could not be accepted at all has `error` and no `email_id`.

Read the results. A `200` here means the batch was processed, not that every message was queued.

## Limits and refusals

The whole batch counts against your [sending quota](/docs/analytics/dashboard), item by item — a batch of 100 spends
100 of the hourly allowance, and an item refused by the quota reports that in its own `error`.

{{< callout type="warning" title="A sandbox key cannot send a batch" >}}
Batch is refused outright on a [sandbox](/docs/email-sending/sandbox) credential, with a `400` telling you to send the
items individually.

The alternative would be worse: batch does not capture, so falling through would deliver for real on the one credential
an operator handed out specifically so it could not — and it would look like success while doing it.
{{< /callout >}}
