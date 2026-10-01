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
{
    "emails": [
        {
            "id": "...",
            "sender": "...",
            "recipients": [
                "..."
            ],
            "status": "failed"
        }
    ]
}
```

## Parameters

| Param            | Notes                                                                                                                                        |
|------------------|----------------------------------------------------------------------------------------------------------------------------------------------|
| `status`         | One status or several separated by commas — see [Email Status](/docs/email-sending/email-status)                                             |
| `sender`         | One whole From address, without regard to case                                                                                               |
| `recipient`      | One whole recipient address, Cc and Bcc included, without regard to case                                                                     |
| `template`       | The name of the template the message was rendered from                                                                                       |
| `tag`            | Only messages carrying this tag                                                                                                              |
| `api_key_id`     | Only mail accepted through this API key                                                                                                      |
| `smtp_server_id` | Only mail delivered through this server                                                                                                      |
| `from`, `to`     | A `created_at` window. Each is a date (`2026-08-01`) or an RFC 3339 instant, `to` exclusive, and a bare date on `to` includes that whole day |
| `after`          | Created strictly after this instant. A poller passes the `created_at` of the newest row it has                                               |
| `search`         | A whole recipient address, or part of a subject, without regard to case                                                                      |
| `limit`          | Default 50, maximum 200                                                                                                                      |
| `cursor`         | The `next_cursor` of the previous page                                                                                                       |

Every filter is ANDed with the others. Rows come back newest first, ordered by `created_at` then `id`. That order is
fixed — there is no sort parameter. The response carries `next_cursor`, empty on the last page.

{{< callout type="info" title="What `search` actually matches" >}}
Two things, joined by OR:

- **A recipient, matched whole.** The pattern is the complete address, so `alice@example.com` finds the message and
  `alice@` finds nothing. It is matched against the recipient **as it was submitted**, so a send addressed to
  `Alice <alice@example.com>` is not found by the bare address. Case does not matter.
- **A subject, matched as a substring**, case-insensitively. `invoice` finds "Your invoice for March".

The **body is never searched.** It may be large, it may be redacted by a retention policy, and it may live in blob
storage rather than in the row — none of which makes for a predictable search.
{{< /callout >}}

## Paging is a cursor, not an offset

The log grows with every message sent, so it pages by cursor. Every page carries `next_cursor`, and the next request
passes it back:

```bash
curl -G http://localhost:3000/api/v1/emails \
  -H "Authorization: Bearer myk_..." \
  --data-urlencode "cursor=MjAyNi0wMy0yMFQwOToxNDoyMi40ODFafDAxOThmNmExLTNjN2UtN2IyMS05ZjRkLTJhNWM4ZTBiMWQzMw"
```

The cursor is opaque. Inside it is the `created_at` and the `id` of the last row together, which is what makes it
safe: two messages can share a `created_at` down to the microsecond, and a cursor made of the timestamp alone would
drop the rows tied with it across a page boundary. An empty `next_cursor` is the last page.

There is no total, deliberately. Counting a table that grows per message costs more than the page it would decorate.

## One message

```
GET /api/v1/emails/{id}
```

The full record: sender, recipients, subject, both bodies, headers, attachment metadata, the delivery state, and the
tracking counters. Content may be shortened or removed by the installation's
[retention settings](/docs/admin/platform-settings) once a message is old enough.

`recipients` is the envelope, every address the message was delivered to. Beside it, `addressing` splits that back
into the lists the sender wrote: `to` and `cc` as the headers named them, and as `bcc` every recipient the headers
did not name. A message sent with `to` alone has everybody under `to` and the other two empty.

```json
"addressing": {
    "to": [
        "jane@customer.example"
    ],
    "cc": [],
    "bcc": [
        "archive@yourapp.example"
    ]
}
```

Related routes on the same message:

| Route                                       | Answers                                                         |
|---------------------------------------------|-----------------------------------------------------------------|
| `GET /api/v1/emails/{id}/status`            | Just the delivery state — the cheap poll                        |
| `GET /api/v1/emails/{id}/attachments/{idx}` | One attachment's bytes, by position                             |
| `GET /api/v1/emails/{id}/eml`               | The message as an `.eml` file, built the way delivery builds it |
| `GET /api/v1/emails/{id}/tracked-links`     | The links rewritten for click tracking, with their tallies      |
| `POST /api/v1/emails/{id}/retry`            | Requeue a failed message                                        |
| `POST /api/v1/emails/{id}/cancel`           | Withdraw a scheduled or still queued message                    |

## Counts

```
GET /api/v1/emails/stats?from=2026-08-01&to=2026-08-31
```

Per-status totals for the project, which is what the dashboard tiles read. `from` and `to` are optional and take the
same date-or-instant bounds as the list, so one month's counts are one request:

```json
{
    "counts": {
        "sent": 18422,
        "failed": 31,
        "queued": 4,
        "suppressed": 12
    }
}
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
