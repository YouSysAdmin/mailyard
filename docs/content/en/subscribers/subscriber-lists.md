---
title: "Subscriber Lists"
description: "Organize subscribers into static and dynamic lists"
weight: 30
---

A list is what a [campaign](/docs/campaigns/overview) sends to. There are two kinds, and the difference is where
membership comes from:

- **Static** — a set of rows you manage. Somebody is a member because you put them there.
- **Dynamic** — a set of rules. Somebody is a member because they match, evaluated fresh every time the list is
  resolved.

{{< callout type="tip" title="Machine API schema" >}}
The `/api/v1` surface describes itself: run
[`mailyard export-api-spec`](/docs/security/api-keys#openapi-description) and feed the document to Swagger UI, Postman,
or a client generator.
{{< /callout >}}

## Create

```
POST /api/v1/subscriber-lists
```

```json
{
  "name": "Newsletter",
  "description": "Opted in to the weekly digest",
  "type": "static"
}
```

`name` is required. `type` is `static` or `dynamic` — nothing else is accepted.

```json
{
  "name": "Pro plan, Americas",
  "type": "dynamic",
  "filter_rules": [
    { "field": "custom_fields.plan", "operator": "eq",       "value": "pro" },
    { "field": "timezone",           "operator": "contains", "value": "America" }
  ]
}
```

At most 20 rules per list.

## Filter rules

A rule is a field, an operator and a value. **All rules must match** — they are combined with AND, and there is no OR.

### Fields

`email`, `name`, `status`, `timezone`, `language`, or `custom_fields.<key>` for anything you store yourself. A field
name outside that set never matches, so a typo produces a segment of nobody rather than an error.

### Operators

| Operator | Matches when |
|---|---|
| `eq` | Values are equal. Numeric if both parse as numbers, otherwise a case-insensitive string compare |
| `neq` | The opposite — and **true when the field is absent**, which is how you select subscribers missing a field |
| `contains` | The value appears anywhere in the field, case-insensitively |
| `starts_with` | Case-insensitive prefix |
| `ends_with` | Case-insensitive suffix |
| `gt` | Numerically greater. Both sides must parse as numbers, or the rule is false |
| `lt` | Numerically less, same condition |
| `exists` | The field is present and not null |

Anything else is refused with `unknown operator <name>`.

{{< callout type="info" title="Rules run in Go, not in SQL" >}}
Membership is computed by walking the project's subscribers and testing each one, rather than by translating rules into
a query. That is why the operator set is small and exact, and why `eq` can be loose about `"5"` versus `5` — JSON does
not distinguish them reliably and rule authors should not have to.

It also means resolving a dynamic list is proportional to the audience, not to the match count. That is fine at the
sizes a campaign runs at, and it is worth knowing before writing a segment you intend to poll.
{{< /callout >}}

### Two filters you do not have to write

Resolution always drops subscribers whose status is not `subscribed`, and always drops anyone with a per-list opt-out.
So a rule on `status` is redundant at best, and `status eq unsubscribed` selects nobody.

## Preview a segment

Check rules before committing them to a list:

```
POST /api/v1/subscriber-lists/preview-segment
```

```json
{
  "filter_rules": [
    { "field": "language", "operator": "eq", "value": "en" },
    { "field": "custom_fields.seats", "operator": "gt", "value": 5 }
  ]
}
```

Between 1 and 20 rules. The response counts every match and returns the **first ten** as a sample, because the point is
to check the rules rather than page the audience:

```json
{ "matched": 1284, "sample": [ { "id": "...", "email": "..." } ] }
```

`matched` counts subscribed members only, but it cannot subtract per-list opt-outs — there is no list yet. Expect a
campaign on the finished list to send to the same number or slightly fewer.

## List and read

```
GET /api/v1/subscriber-lists?limit=20
GET /api/v1/subscriber-lists/{id}
```

`member_count` comes back on the **single-list** route, and only for a static list. A dynamic list has no membership to
count without resolving the segment, and reporting `0` there would be a wrong answer rather than an empty one — so the
field is absent instead.

## Update and delete

```
PATCH  /api/v1/subscriber-lists/{id}
DELETE /api/v1/subscriber-lists/{id}
```

`name` is required on the update. Deleting takes the membership rows and per-list opt-outs with it — including the
opt-outs, so recreating a list under the same name does not restore who had left it.

## Members of a static list

```
GET    /api/v1/subscriber-lists/{id}/members?limit=20
POST   /api/v1/subscriber-lists/{id}/members
DELETE /api/v1/subscriber-lists/{id}/members/{subscriberId}
```

The add call takes either identifier — whichever your caller has to hand:

```json
{ "subscriber_id": "0198f6a1-3c7e-7b21-9f4d-2a5c8e0b1d33" }
```

```json
{ "email": "jane@example.com" }
```

Adding somebody twice is not an error. A dynamic list refuses membership calls: its members are a query result, and
there is nothing to insert into.

## Sign-up flows

These three take an address rather than a subscriber id, which is what a sign-up form or an automation tool actually
has. They are ordinary `/api/v1` routes — an API key carries the project, so no extra header is involved.

### Subscribe

```
POST /api/v1/subscriber-lists/subscribe
```

```json
{
  "list_id": "0198f6a1-3c7e-7b21-9f4d-2a5c8e0b1d33",
  "email": "jane@example.com",
  "name": "Jane Doe",
  "language": "en",
  "custom_fields": {"source": "footer form"}
}
```

`list_id` and `email` are required. The list must already exist — this route does not create one — and it must be
static, or the call is refused with `dynamic lists have no explicit members`.

The subscriber is created if new and updated if not: any of `name`, `custom_fields`, `timezone` and `language` you
supply are written, and the ones you omit are left alone.

{{< callout type="warning" title="Subscribing asserts fresh consent" >}}
The call sets status back to `subscribed`, clears `unsubscribed_at`, and lifts any per-list opt-out. So it will
**re-activate somebody who previously unsubscribed**, globally as well as for this list.

That is right for a form somebody just filled in and wrong for a bulk sync. Use [import](/docs/subscribers/bulk-import)
for the second — it leaves an existing status alone.
{{< /callout >}}

Answers `201`:

```json
{
  "list_id": "0198f6a1-3c7e-7b21-9f4d-2a5c8e0b1d33",
  "subscriber": { "id": "...", "email": "jane@example.com", "status": "subscribed" }
}
```

### Unsubscribe

```
POST /api/v1/subscriber-lists/{id}/unsubscribe
```

```json
{ "email": "jane@example.com", "reason": "footer link" }
```

Records an opt-out **scoped to this list**. The subscriber's global status is untouched, so they keep receiving your
other campaigns. `reason` is optional and stored as written.

### Resubscribe

```
POST /api/v1/subscriber-lists/{id}/resubscribe
```

```json
{ "email": "jane@example.com" }
```

Lifts the opt-out. Note what this does **not** do: it does not add the subscriber to a static list they were never a
member of. On a static list, resubscribing somebody who was removed as well as opted out needs the members call too.
Idempotent either way.
