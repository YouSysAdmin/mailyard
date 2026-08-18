---
title: "Email Log"
description: "List and filter a project's sent email"
weight: 70
---

Every message a project accepts becomes a row here, whatever route submitted it — the API, a template send, a batch, a
campaign, or [SMTP submission](/docs/security/smtp-submission). The log is the record of what was sent and what
happened to it, and it backs the **Emails** page in the console.

```
GET /api/v1/emails
```

```bash
curl "http://localhost:3000/api/v1/emails?limit=50&status=failed" \
  -H "Authorization: Bearer myk_..."
```

```json
{ "emails": [ { "id": "...", "sender": "...", "recipients": ["..."], "status": "failed" } ] }
```

## Parameters

| Param | Notes |
|---|---|
| `status` | One status. Not a list — see [Email Status](/docs/email-sending/email-status) for the values |
| `search` | A whole recipient address, or part of a subject |
| `limit` | Default 50, maximum 200 |
| `before` | Cursor: RFC 3339 `created_at` of the last row you saw |
| `before_id` | The id of that same row. Send it with `before` |

Rows come back newest first, ordered by `created_at` then `id`. That order is fixed — there is no sort parameter.

{{< callout type="info" title="What `search` actually matches" >}}
Two things, joined by OR:

- **A recipient, matched whole.** The pattern is the complete address, so `alice@example.com` finds the message and
  `alice@` finds nothing. It is matched against the recipient **exactly as it was submitted**, so a send addressed to
  `Alice <alice@example.com>` is not found by the bare address, and matching is case-sensitive.
- **A subject, matched as a substring**, case-insensitively. `invoice` finds "Your invoice for March".

The **body is never searched.** It may be large, it may be redacted by a retention policy, and it may live in blob
storage rather than in the row — none of which makes for a predictable search.
{{< /callout >}}

## Paging is a cursor, not an offset

The log grows with every message sent, so it pages by cursor. Take the `created_at` and `id` of the last row on the
page and pass both:

```bash
curl -G http://localhost:3000/api/v1/emails \
  -H "Authorization: Bearer myk_..." \
  --data-urlencode "before=2026-03-20T09:14:22.481Z" \
  --data-urlencode "before_id=0198f6a1-3c7e-7b21-9f4d-2a5c8e0b1d33"
```

{{< callout type="warning" title="Send `before_id` as well as `before`" >}}
`before` on its own still works, and it will silently lose rows. Two messages can share a `created_at` down to the
microsecond, and if that tie straddles a page boundary the rows sharing the timestamp appear on **neither** page. You
get a log with a message missing and no indication that it happened.

With both, the comparison is on the pair `(created_at, id)`, which no two rows can tie on.
{{< /callout >}}

There is no total, deliberately. Counting a table that grows per message costs more than the page it would decorate.
Fetch a page, and if it comes back full there is probably another.

## One message

```
GET /api/v1/emails/{id}
```

The full record: sender, recipients, subject, both bodies, headers, attachment metadata, the delivery state, and the
tracking counters. Content may be shortened or removed by the installation's
[retention settings](/docs/admin/platform-settings) once a message is old enough.

Related routes on the same message:

| Route | Answers |
|---|---|
| `GET /api/v1/emails/{id}/status` | Just the delivery state — the cheap poll |
| `GET /api/v1/emails/{id}/attachments/{idx}` | One attachment's bytes, by position |
| `GET /api/v1/emails/{id}/tracked-links` | The links rewritten for click tracking, with their tallies |
| `POST /api/v1/emails/{id}/retry` | Requeue a failed message |

## Counts

```
GET /api/v1/emails/stats
```

Per-status totals for the project, which is what the dashboard tiles read:

```json
{ "counts": { "sent": 18422, "failed": 31, "queued": 4, "suppressed": 12 } }
```

## In the console

The **Emails** page offers exactly what the API does: a status dropdown and one search box, labelled for what it takes —
a recipient address or part of a subject. Paging is the same cursor, so **Load more** appends rather than jumping to a
page number.

The page refreshes itself on a timer, quietly, and pauses that refresh once you have paged back through history — a
refresh that yanked you forward every ten seconds while you were reading would be worse than slightly stale rows.

{{< callout type="info" title="This list may be served by a read replica" >}}
Where replicas are configured, the log listing and the status counts are among the queries allowed to run on one. That
makes them cheap and makes them **eventually** consistent: a message accepted a moment ago can be missing from the list
for as long as replication lag lasts.

Fetching one message by id always reads the primary, so a send followed by a lookup of its own id is never affected.
{{< /callout >}}
