---
title: "Contact Management"
description: "Track email recipients"
weight: 10
---

Mailyard tracks every address this project delivers to, with the tallies for it. Contacts are **read-only**: the
delivery worker writes them, and the API exposes list and detail only. There is no create, update, or delete - a tally
an operator can edit is a number nobody can trust.

{{< callout type="note" >}}
Contacts are not an audience. To build lists you can target with campaigns, use
[Subscriber Lists](/docs/subscribers/subscriber-lists). To let people opt out of one category of mail,
use [Unsubscribe Lists](/docs/contacts/unsubscribe-lists). See
[Contact Lists](/docs/contacts/contact-lists) for why there is no manual grouping here.
{{< /callout >}}

Available on both surfaces: `/api/v1/contacts` with a session, and `/api/v1/contacts` with an API key holding
`contacts:read`.

## List Contacts

```
GET /api/v1/contacts?search=alice&limit=25&offset=0
```

| Parameter | Default | Description                                                 |
|-----------|---------|-------------------------------------------------------------|
| `search`  | -       | Matches the address or the display name, case-insensitively |
| `limit`   | `20`    | Page size, capped at `200`                                  |
| `offset`  | `0`     | Rows to skip                                                |
| `page`    | -       | Zero-based page number, honored when `offset` is absent     |

```bash
curl "http://localhost:3000/api/v1/contacts?search=alice" \
  -H "Authorization: Bearer myk_..." \
```

```json
{
    "contacts": [
        {
            "id": "5b3c7eef-5c39-406d-bacc-3f6b5bedb9ca",
            "project_id": "81af718e-f0ae-4780-a0d7-9f05b34dabcc",
            "email": "alice@example.com",
            "name": "Alice Smith",
            "sent_count": 2,
            "fail_count": 0,
            "suppressed": false,
            "last_sent_at": "2026-08-06T04:12:02Z",
            "created_at": "2026-08-06T04:11:58Z",
            "updated_at": "2026-08-06T04:12:02Z"
        }
    ],
    "total": 3,
    "limit": 25,
    "offset": 0
}
```

Results are ordered by most recent activity. A contact that has only ever failed sorts by that failure rather than
dropping to the bottom for having no successful send.

## Contact Details

```
GET /api/v1/contacts/{id}
```

Returns `{ "contact": { ... } }` with the same fields. A contact belonging to another project reads as `404`, never
`403`.

## How Tracking Works

A contact row is written when a message to that address reaches a **terminal state** -
`sent` or `failed`. Queued and scheduled mail does not create one, because a message that has not been attempted says
nothing about the address.

| Field                             | Meaning                                                                                                                                                   |
|-----------------------------------|-----------------------------------------------------------------------------------------------------------------------------------------------------------|
| `sent_count`                      | Messages successfully delivered to this address                                                                                                           |
| `fail_count`                      | Messages that permanently failed                                                                                                                          |
| `name`                            | Display name last seen on a recipient header (`Alice Smith <alice@example.com>`). A later send with a bare address does not erase a name learned earlier. |
| `last_sent_at` / `last_failed_at` | Most recent outcome of each kind                                                                                                                          |
| `suppressed`                      | Whether the address is currently on the [suppression list](/docs/contacts/suppression-list)                                                               |

Addresses are normalized to lowercase and trimmed, so `Alice@Example.com` and
`alice@example.com` are one contact.

{{< callout type="info" title="Suppression is computed, not stored" >}}
`suppressed` is resolved at read time from the suppression list rather than kept on the contact row. That way it can
never disagree with the list that actually governs sending - removing a suppression is reflected on the very next read.
{{< /callout >}}

Messages skipped because the recipient was already suppressed are **not** counted as failures. That was our decision
about the address, not a delivery problem with it.
