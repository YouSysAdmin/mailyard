---
title: "Unsubscribe Lists"
description: "Transactional opt-out scopes for one-click unsubscribe links"
weight: 50
---

An unsubscribe list is a **transactional opt-out scope**. A send can reference one of these by ID so Mailyard can mint a
one-click unsubscribe link whose click suppresses the recipient on that list only — the recipient's other transactional
mail (receipts, password resets) keeps flowing.

Unsubscribe lists are email-keyed and carry **no membership**: a recipient is never enrolled. The list is purely a
suppression scope. This makes them distinct from [Subscriber Lists](/docs/subscribers/subscriber-lists), which are
subscriber-keyed campaign audiences.

These routes are project-scoped. Send a JWT plus the `X-Mailyard-Project-Id` header (a project-scoped API key implies
the project). Create, update, and delete require project edit permission.

## How it relates to sending

When a send names an unsubscribe list, the
`{{ mailyard_unsubscribe_url }}` [system variable](/docs/templates/system-variables) renders a one-click link bound to
that list and recipient. Clicking it writes a **list-scoped** [suppression](/docs/contacts/suppression-list)
(`kind: "list_unsubscribe"`) for that address against this list — not a global block. Future sends that reference the
same list skip the address; mail on other lists is unaffected.

## Fields

`id` is a UUID, and it travels inside the signed unsubscribe token as well as in the API, so it is worth keeping stable.

`name` is the label you address the list by. Unique within the project, and required.

`public_name` is the only field a recipient ever reads — it is the wording on the hosted unsubscribe page. Empty falls
back to `name`, which is how a scope called `product-updates-v2` ends up shown to a customer. Set it.

`description` is for whoever maintains the list, and appears nowhere outside the console.

`active` gates whether **new** links are minted. Turning a list off does not lift the opt-outs already recorded against
it — people who unsubscribed stay unsubscribed, which is the only defensible reading of a switch like this.

## Create an Unsubscribe List

```
POST /api/v1/unsubscribe-lists
```

| Field         | Required | Description                        |
|---------------|----------|------------------------------------|
| `name`        | yes      | Internal label, unique per project |
| `public_name` | no       | Name shown to recipients           |
| `description` | no       | Optional description               |

```bash
curl -X POST http://localhost:3000/api/v1/unsubscribe-lists \
  -H "Authorization: Bearer myk_..." \
  -H "Content-Type: application/json" \
  -d '{
    "name": "shipping-notices",
    "public_name": "Shipping and delivery updates",
    "description": "Dispatch and tracking mail. Not receipts."
  }'
```

New lists are created with `active: true`. Returns `201 Created`, or `409 Conflict` if a list with that name already
exists in the project.

## List Unsubscribe Lists

```
GET /api/v1/unsubscribe-lists?limit=20
```

```json
{
  "unsubscribe_lists": [
    {
      "id": "0198f6a1-3c7e-7b21-9f4d-2a5c8e0b1d33",
      "project_id": "0198f6a0-9d12-7f33-a1b8-6c4e2f8a0d57",
      "name": "shipping-notices",
      "public_name": "Shipping and delivery updates",
      "description": "Dispatch and tracking mail. Not receipts.",
      "active": true,
      "suppressed_count": 142,
      "created_at": "2026-01-01T00:00:00Z"
    }
  ]
}
```

`suppressed_count` is how many addresses have opted out of this scope. It is computed for the read and stored nowhere,
so it is always current and is not something you can filter or sort on.

## Get an Unsubscribe List

```
GET /api/v1/unsubscribe-lists/{id}
```

Returns the list, or `404` if it does not exist in the current project.

## Update an Unsubscribe List

```
PATCH /api/v1/unsubscribe-lists/{id}
```

All fields are optional; only the ones you send are changed. Set `active` to `false` to deactivate a list without
deleting it.

```json
{
    "public_name": "Product news",
    "active": false
}
```

## Delete an Unsubscribe List

```
DELETE /api/v1/unsubscribe-lists/{id}
```

Returns `204 No Content`.
