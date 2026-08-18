---
title: "Subscriber Management"
description: "Create and manage email subscribers"
weight: 10
---

A subscriber is one person in a project's marketing audience: an address, a lifecycle status, and whatever custom fields
your campaigns render against. There is one row per address per project — the address is the identity, and the id is
just how you reference it.

{{< callout type="info" title="Subscribers are not contacts" >}}
[Contacts](/docs/contacts/contact-management) are derived from mail you have actually sent and are read-only.
Subscribers are a list you maintain, and are what [campaigns](/docs/campaigns/overview) send to. The two never merge.
{{< /callout >}}

Every route here is project-scoped and needs a `subscribers` permission.

## Status

| Status | Meaning | Set by |
|---|---|---|
| `subscribed` | Receives campaigns | The default on create |
| `unsubscribed` | Opted out | The subscriber, or you |
| `bounced` | The address failed | Delivery feedback |
| `complained` | Reported as spam | Delivery feedback |

Only `subscribed` receives campaign mail. The last two are written by the delivery path rather than by hand, and
resetting one to `subscribed` because the person asked you to is a decision, not a correction — the address failed or
complained for a reason that is still true.

## Create

```
POST /api/v1/subscribers
```

```bash
curl -X POST http://localhost:3000/api/v1/subscribers \
  -H "Authorization: Bearer myk_..." \
  -H "Content-Type: application/json" \
  -d '{
    "email": "jane@example.com",
    "name": "Jane Doe",
    "timezone": "America/New_York",
    "language": "en",
    "custom_fields": {"company": "Acme", "plan": "pro"}
  }'
```

Only `email` is required. Status defaults to `subscribed`, and `subscribed_at` is stamped at the same moment. The
address is trimmed and lowercased, and an address the project already holds is refused with `409` — use
[import](/docs/subscribers/bulk-import) if you want an upsert instead.

The project's subscriber cap is checked first, so a create over the plan limit answers `429`.

## List

```
GET /api/v1/subscribers?limit=20&offset=0&status=subscribed&search=acme
```

| Parameter | Notes |
|---|---|
| `limit` | Default 20, clamped to the API ceiling |
| `offset` | Rows to skip. `page` is honoured as a zero-based alias when `offset` is absent |
| `status` | One of the four statuses |
| `search` | Matches email or name |

`total` counts what the **filters** match, not the project — so a search hitting one row out of five thousand reports
one, and the pager offers one page rather than fifty mostly empty ones.

## Read one

```
GET /api/v1/subscribers/{id}
```

## Update

```
PATCH /api/v1/subscribers/{id}
```

{{< callout type="warning" title="This behaves like a replace, not a patch" >}}
Two things about this route will surprise you if you treat the method literally:

- **`email` is required on every call.** Omitting it fails validation, even when you only meant to change the name.
- **Omitted fields are cleared.** `name`, `timezone` and `language` are written from the request unconditionally, so a
  body carrying only `email` and `name` blanks the timezone and language you had.

`custom_fields` is the one exception — leave it out and the stored object is kept. Send the subscriber back whole,
which is what a `GET` immediately before makes easy.
{{< /callout >}}

```json
{
  "email": "jane@example.com",
  "name": "Jane Smith",
  "timezone": "America/New_York",
  "language": "en",
  "custom_fields": {"company": "New Corp", "plan": "enterprise"}
}
```

Moving `status` to `unsubscribed` stamps `unsubscribed_at`. Changing the address to one another subscriber holds is
refused with `409`.

## Delete

```
DELETE /api/v1/subscribers/{id}
```

Returns `204`. This removes the audience row. It does **not** create a
[suppression](/docs/contacts/suppression-list), so nothing stops the address being added again by the next import or
mailed by a transactional send. If the intent is "never mail this person again", suppress the address as well.

## Custom fields

`custom_fields` is a flat JSON object, up to 50 keys.

{{< callout type="warning" title="They are flattened into the template data" >}}
A campaign merges the subscriber's custom fields into the **top level** of the render data. So a field called `company`
is written `{{ company }}` — not `{{ .custom_fields.company }}`, which resolves to nothing.

Two keys are always overwritten after the merge: `email` and `name` come from the subscriber record itself, so a custom
field of either name never reaches a template.
{{< /callout >}}

Beyond personalization, custom fields are what dynamic
[subscriber lists](/docs/subscribers/subscriber-lists) segment on.

## In bulk

See [Bulk Import](/docs/subscribers/bulk-import) for the JSON and CSV routes, which upsert rather than refusing an
address that already exists.
