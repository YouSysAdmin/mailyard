---
title: "Suppression List"
description: "Manage suppressed email addresses"
weight: 40
---

The suppression list prevents emails from being sent to specific addresses. Addresses are added automatically on hard
bounces/complaints, or manually.

These routes are project-scoped. Send a JWT plus the `X-Mailyard-Project-Id` header (a project-scoped API key implies
the project).

A suppression can be **global** (blocks all mail to the address) or **list-scoped** (an opt-out of a
single [Unsubscribe List](/docs/contacts/unsubscribe-lists)). Pass `list_id` to scope a suppression to one list; omit it
for a global block.

## Add to Suppression List

```
POST /api/v1/suppressions
```

| Field     | Required | Description                                                             |
|-----------|----------|-------------------------------------------------------------------------|
| `email`   | yes      | Address to suppress                                                     |
| `reason`  | no       | Free-text note                                                          |
| `list_id` | no       | Scope the suppression to one unsubscribe list (omit for a global block) |

```bash
curl -X POST http://localhost:3000/api/v1/suppressions \
  -H "Authorization: Bearer myk_..." \
  -H "Content-Type: application/json" \
  -d '{"email": "unsubscribed@example.com", "reason": "User requested removal"}'
```

Manually created suppressions are stored with `kind: "manual"`. Returns `409 Conflict` if already suppressed.

## List Suppressed Addresses

```
GET /api/v1/suppressions?search=ada@&kind=hard&limit=50
```

| Parameter | Meaning                                                                 |
|-----------|-------------------------------------------------------------------------|
| `search`  | Matches the **start** of an address                                     |
| `kind`    | `hard`, `bounce`, `complaint` or `manual`                               |
| `limit`   | Rows per page, default 50, capped at 200                                |
| `cursor`  | Where to resume. Pass back the `next_cursor` from the previous response |

Response:

```json
{
    "suppressions": [
        {
            "id": "3f7c1a2b-...",
            "project_id": "9b2e...",
            "email": "unsubscribed@example.com",
            "kind": "manual",
            "reason": "User requested removal",
            "unsubscribe_list_id": "",
            "created_at": "2026-08-08T00:00:00Z"
        }
    ],
    "next_cursor": "MjAyNi0wOC0wOFQwMDowMDowMFp8M2Y3YzFhMmI"
}
```

`kind` records why the address is blocked:

| Kind | Written by |
|---|---|
| `hard` | A permanent SMTP rejection during delivery |
| `bounce` | A [bounce report](/docs/contacts/bounce-handling) classified hard |
| `complaint` | A spam complaint from a feedback loop |
| `manual` | You, through this API or the console |
| `list_unsubscribe` | A recipient clicking a one-click link scoped to an [unsubscribe list](/docs/contacts/unsubscribe-lists) |

Only the first four can be **created** by a caller. `list_unsubscribe` is written by the hosted unsubscribe page and
carries an `unsubscribe_list_id`, so it blocks that one scope rather than everything. All five are accepted as a
`kind` filter on this list.

### Paging

Keyset, not page numbers. Keep passing the `next_cursor` you got back until the response returns an empty one, which
means you have reached the end.

```bash
cursor=""
while :; do
  page=$(curl -sG "$API/api/v1/suppressions" \
    -H "Authorization: Bearer $KEY" \
    --data-urlencode "limit=200" --data-urlencode "cursor=$cursor")
  echo "$page" | jq -c '.suppressions[]'
  cursor=$(echo "$page" | jq -r '.next_cursor')
  [ -n "$cursor" ] || break
done
```

{{< callout type="note" title="Why there is no page number and no total" >}}
This table gains a row for every permanently rejected message and is **never pruned** - a suppression is permanent by
design. On a busy install it reaches millions of rows.

`OFFSET 900000` makes the database read and discard nine hundred thousand rows to serve one page, so the deeper you go
the slower it gets. A cursor names the last row you saw instead, and every page costs the same. `COUNT(*)` is the same
problem in reverse: a full index scan, on every page load, to produce a number no caller acts on.

The tradeoff is that you can only go forward. For finding one address, use `search`

- that is the question this list exists to answer, and no amount of paging answers it on a table this size.
{{< /callout >}}

## Remove from Suppression List

```
DELETE /api/v1/suppressions
```

```json
{
    "email": "resubscribed@example.com"
}
```

Include `list_id` to remove only the list-scoped suppression; omit it to remove the global block. Returns
`204 No Content`.

## What a suppression does to a send

Recipients are checked at **accept time**, before the message is queued — not at delivery. So a scheduled message that
was accepted before you suppressed an address will still go to it, and one accepted afterwards will not.

A suppressed address is removed from the recipient list, and the send continues with whoever is left. Nothing is
refused:

- **Some recipients suppressed** — the message is queued to the rest, and the dropped addresses come back in
  `suppressed_recipients`.
- **All recipients suppressed** — a row is still written, with status `suppressed` and an error message saying so.
  Nothing leaves the building.

```json
{
  "email": { "id": "0198f6a1-3c7e-7b21-9f4d-2a5c8e0b1d33", "status": "queued" },
  "suppressed_recipients": ["j.okafor@acme-industrial.example"]
}
```

In a [batch](/docs/email-sending/batch-email) the same field appears per item, under that item's `index`.

{{< callout type="warning" title="Nothing shouts about this" >}}
A suppressed send is a `201`. If your integration only checks the status code, an address quietly stops receiving mail
and the first anybody hears of it is a customer asking where their receipt went.

Read `suppressed_recipients` and log it.
{{< /callout >}}

A **list-scoped** suppression only applies to sends that name the same `unsubscribe_list_id`. Everything else reaches
the address normally, which is the entire point of scoping — somebody who opted out of shipping notices still gets
their password reset.
