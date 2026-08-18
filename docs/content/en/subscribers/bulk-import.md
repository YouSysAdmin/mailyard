---
title: "Bulk Import"
description: "Import subscribers from JSON and CSV"
weight: 20
---

Two routes take a batch of subscribers. They differ only in how you hand over the rows — the behaviour after that is
identical, down to the response.

{{< callout type="warning" title="Import means upsert, not insert" >}}
A row whose address already exists in the project **updates** that subscriber rather than being skipped. The stored id,
`created_at` and `subscribed_at` are kept, and a row that leaves `status` empty keeps the status the subscriber already
had — so re-importing a list does not resurrect people who unsubscribed since the last run.

That is the behaviour you want for a nightly sync and the behaviour to be careful with for a one-off file: a blank
`name` column overwrites the name you have.
{{< /callout >}}

Both routes are project-scoped and need the `subscribers:write` permission.

## JSON

```
POST /api/v1/subscribers/import
```

Between 1 and 10,000 subscribers per call.

```bash
curl -X POST http://localhost:3000/api/v1/subscribers/import \
  -H "Authorization: Bearer myk_..." \
  -H "Content-Type: application/json" \
  -d '{
    "subscribers": [
      {
        "email": "alice@example.com",
        "name": "Alice",
        "timezone": "Europe/London",
        "language": "en",
        "custom_fields": {"company": "Acme", "plan": "pro"}
      },
      { "email": "bob@example.com", "name": "Bob", "language": "fr" }
    ]
  }'
```

Only `email` is required per row. `status` must be one of `subscribed`, `unsubscribed`, `bounced` or `complained`, and
`custom_fields` holds at most 50 keys. Addresses are trimmed and lowercased on the way in, so `Alice@Example.com ` and
`alice@example.com` are one subscriber rather than two.

## CSV

```
POST /api/v1/subscribers/import/csv
```

{{< callout type="warning" title="Send the CSV as the raw request body" >}}
This is not a file upload. There is no multipart form, no `file` field, and no column-mapping parameter — post the CSV
text itself and let the **header row** name the columns.
{{< /callout >}}

```bash
curl -X POST http://localhost:3000/api/v1/subscribers/import/csv \
  -H "Authorization: Bearer myk_..." \
  -H "Content-Type: text/csv" \
  --data-binary @subscribers.csv
```

```csv
email,name,language,company,plan
alice@example.com,Alice,en,Acme,pro
bob@example.com,Bob,fr,Globex,free
```

Five header names are recognised, matched case-insensitively after trimming:

| Header | Goes to |
|---|---|
| `email` | The address. **Required** — a file without this column is refused |
| `name` | The display name |
| `status` | One of the four statuses, lowercased |
| `timezone` | IANA zone name |
| `language` | Locale code, lowercased |

**Every other column becomes a custom field**, keyed by its header. That is the whole mapping mechanism: rename the
column and you rename the field. An empty cell is skipped rather than stored as an empty string, so a sparse column does
not litter every subscriber with blanks.

The same 10,000-row ceiling applies, counted over data rows.

A malformed file is refused whole, before anything is written:

| Response | Cause |
|---|---|
| `csv is empty or unreadable` | No header row could be read |
| `csv header must include an email column` | No column named `email` |
| `csv parse error at line N` | Unbalanced quoting or a ragged row |
| `csv has no data rows` | Header only |
| `csv exceeds the 10000 row import limit` | Too many rows |

## What comes back

Both routes answer `200` with the same report:

```json
{
  "created": 1240,
  "updated": 61,
  "skipped": 2,
  "errors": [
    { "index": 88,  "email": "not-an-address", "error": "invalid email" },
    { "index": 903, "email": "dana@example.com", "error": "unknown status pending" }
  ]
}
```

A bad row is reported and passed over — it does not sink the rest of the file, because one typo in a ten thousand row
export should not cost you the other nine thousand nine hundred and ninety nine. `skipped` is exactly the length of
`errors`, and `index` is the row's position in what you sent, counting from zero.

{{< callout type="info" title="The plan limit is checked once, up front" >}}
The whole batch is measured against the project's subscriber cap before any row is written. Over it, the call is refused
with `429` and **nothing** is imported — you do not get a partially applied file to reconcile.
{{< /callout >}}
