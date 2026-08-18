---
title: "Overview"
description: "Multi-tenant projects, project-defined roles, and the project context header"
weight: 10
---

Projects provide multi-tenant isolation within Mailyard. They work like GitHub Organizations: a user is a member of one
or more **projects**, and everything they create belongs to exactly one of them.

Nothing is created for a new account. A user who belongs to no project sees the projects page and waits to be invited -
belonging nowhere is an ordinary state, not an error. Whether they can instead make one themselves is the
`user_project_creation` [platform setting](/docs/admin/platform-settings), which is off by default.

## Concepts

### Projects

A project is an isolated environment where team members collaborate. Resources created within a project are only visible
to members of that project. Each project has:

- A unique **name** and **slug** (a URL-friendly identifier)
- An **owner** (the creator)
- **Members**, each carrying one of the project's roles
- A **default language**
- Isolated operational resources (templates, SMTP servers, domains, API keys, contacts, subscribers, campaigns, emails,
  webhooks, etc.)

### Roles

A project writes its own roles. There are no built-in ones, and that is the design rather than an omission - see the
note below.

A **role** is a named list of **permissions**, and a permission is a resource plus an action: `emails:read`,
`templates:delete`. Every route checks a permission, so what a role can do is exactly the list it holds.

The catalogue is closed - 21 resources - and each one declares the actions it actually has:

| Resource                          | read | write | delete |
|-----------------------------------|:----:|:-----:|:------:|
| Emails                            | yes  |  yes  |   -    |
| Campaigns                         | yes  |  yes  |  yes   |
| Templates, stylesheets, languages | yes  |  yes  |  yes   |
| Contacts                          | yes  |   -   |   -    |
| Subscribers and lists             | yes  |  yes  |  yes   |
| Suppressions, unsubscribe lists   | yes  |  yes  |  yes   |
| Bounces                           | yes  |  yes  |   -    |
| Inbound mail                      | yes  |  yes  |  yes   |
| Analytics                         | yes  |   -   |   -    |
| Notifications                     | yes  |  yes  |  yes   |
| Email sandbox                     | yes  |  yes  |  yes   |
| Domains                           | yes  |  yes  |  yes   |
| Sender addresses                  | yes  |  yes  |  yes   |
| SMTP servers, groups, relay nodes | yes  |  yes  |  yes   |
| API keys, SMTP credentials        | yes  |  yes  |  yes   |
| Webhooks                          | yes  |  yes  |  yes   |
| Relay node enrolment              |  -   |  yes  |   -    |
| Members, roles and invitations    | yes  |  yes  |  yes   |
| Project settings                  | yes  |  yes  |   -    |
| Audit log                         | yes  |   -   |   -    |
| Data export and erasure           | yes  |   -   |  yes   |

The gaps are not oversights. Contacts are written by the delivery worker, not by an operator, so there is nothing to
grant. A project's data is exported or erased and there is nothing in between, so the data resource has no write. Relay
enrolment is enrolment only. A grid where a third of the boxes do nothing is a grid people stop reading, so the boxes
that do nothing are not drawn.

`delete` is separate from `write` because "may edit but not remove" is the most requested shape of a role, and two
actions could not say it. The mapping is mechanical: the `DELETE` method needs `delete`, and the handful of erasing
POSTs (`/sandbox/clear`, the two data erasures) say so explicitly.

The catalogue itself is served by the running binary at
`GET /api/v1/permissions`, so the console checkboxes cannot offer a permission the server does not enforce.

{{< callout type="note" title="There used to be five built-in roles" >}}
Owner, admin, editor, viewer and developer, defined in the product. They were measured against the router and opened
153, 153, 118, 59 and 10 of its 153 gated routes - nesting exactly one inside the next, with owner and admin identical.
Five names delivering four levels along one axis, which is the single ranking the permission catalogue had been
introduced to escape.

They are gone. What each install actually means by "editor" is its own business, and a self-hosted product guessing at
it in the binary was the wrong place for that decision.
{{< /callout >}}

### Ownership

**Ownership is not a role**, it is a flag on the membership row. It grants everything in the catalogue plus the two acts
no permission can name: deleting the project and rewriting its single sign-on policy.

- A project may have **several owners**. Only an existing owner may grant or revoke ownership, even though
  `members:write` hands out roles.
- A project must keep **at least one**. Demoting or removing the last owner answers `409` - a project with no owner
  could never be deleted or handed on, and nothing in the product can put one back.
- Nothing automatic can mint an owner. Signing in never grants anything at all - membership comes from an invitation,
  and an invitation carries a role, which ownership is not.

### Writing roles

Roles live under Project - Roles: a name and read/write/delete checkboxes per resource, with the same set as editable
JSON underneath.

Rules worth knowing:

- **Edits apply on the member's next request.** There is no cache to wait out.
- **A role assigned to members cannot be deleted.** Everyone silently dropping to the project default would be a bulk
  change of access nobody asked for. Reassign the members first - the error names how many hold it.
- **The project's default role cannot be deleted either**, and that one fails worse: the project would keep naming a row
  that is gone, which reads as "no role", so everybody who never had one of their own loses everything at once. Name a
  different default first.
- An **empty role is a lockdown**, not an absence: the member keeps membership and reaches nothing until the role is
  edited or replaced.
- The **wildcard is refused** in a role. A role is an explicit list, and
  "may do everything here" is said by making somebody an owner, where it is visible for what it is.

### The default role

`PUT /api/v1/projects/{id}/default-role` names the role members carry when their own membership names none - which is
every member an invitation or a plain add creates without saying otherwise.

**With no default named, those members reach nothing.** They belong to the project, can see its name and can leave it,
and every tenant route refuses them. That is deliberate: a project that has not said what its members may do admits them
to no resource, which is visible in the console and one click to fix. A baseline granted because nobody chose one is
neither.

A member's own role always wins over the default. The console says which of the two is in force, because "everyone here
is a Support" and "this person was made a Support" survive a change to the default differently.

The console menu is built from the permissions the server grants, so a page that is not offered is one the API would
refuse. `GET /api/v1/projects/{id}` returns the caller's resolved set alongside whether they own the project.

## API usage

These are console endpoints: they authenticate with the session cookie set at sign-in and resolve the project from the
`X-Mailyard-Project-Id` header. The same session JWT is also accepted as `Authorization: Bearer <jwt>`, but its only
home is the `HttpOnly` cookie, so a script is easier to write with a cookie jar - see
[Authentication](/docs/security/authentication). For anything non-interactive, prefer
an [API key](/docs/security/api-keys) against `/api/v1`.

### Project context header

Routes under `/api/v1/*` operate against the **active project**. An API key names its own; a session resolves it from
the `X-Mailyard-Project-Id` header:

```
X-Mailyard-Project-Id: 81af718e-f0ae-4780-a0d7-9f05b34dabcc
```

```bash
curl -X GET http://localhost:3000/api/v1/templates \
  -H "Authorization: Bearer myk_..." \
```

The header value is the project UUID. A project you are not a member of - or one that no longer exists - resolves to no
project at all, so a route that needs one answers `400` asking you to name it.

Omitting the header falls back to `?project_id=`, and then to whichever project can be inferred:

| Your projects | Resolves to                        |
|---------------|------------------------------------|
| exactly one   | that project, with your role in it |
| several       | `400` - name one with the header   |
| none          | `400` - create one first           |

A caller with one project therefore never has to set the header. A caller with several should always set it: guessing is
how mail ends up in the wrong project.

{{< callout type="warning" >}}
The correct header is `X-Mailyard-Project-Id`. Earlier drafts referred to `X-Project-ID` — that name is wrong
and is not recognized by the API.
{{< /callout >}}

### Project-scoped API keys

API keys created inside a project context are bound to that project. When you authenticate with such a key, the active
project is implied by the key itself, so the `X-Mailyard-Project-Id` header is **not** needed:

```bash
curl -X POST http://localhost:3000/api/v1/emails/send \
  -H "Authorization: Bearer myk_..." \
  -H "Content-Type: application/json" \
  -d '{ ... }'
```

This is how transactional sending and other API-key endpoints stay scoped to the right project without a header.

## Project management endpoints

These routes address the project by **path id**, so they do not use the
`X-Mailyard-Project-Id` header - unlike the rest of `/api/v1`. Each one checks a permission in the project it names.

| Method   | Path                                        | Needs                                            |
|----------|---------------------------------------------|--------------------------------------------------|
| `POST`   | `/api/v1/projects`                          | any signed-in account                            |
| `GET`    | `/api/v1/projects`                          | any signed-in account                            |
| `GET`    | `/api/v1/projects/{id}`                     | membership                                       |
| `PATCH`  | `/api/v1/projects/{id}`                     | `settings:write`                                 |
| `DELETE` | `/api/v1/projects/{id}`                     | ownership                                        |
| `GET`    | `/api/v1/projects/{id}/members`             | `members:read`                                   |
| `POST`   | `/api/v1/projects/{id}/members`             | `members:write`                                  |
| `PATCH`  | `/api/v1/projects/{id}/members/{user_id}`   | `members:write`, and ownership to change `owner` |
| `DELETE` | `/api/v1/projects/{id}/members/{user_id}`   | `members:delete`, or yourself                    |
| `GET`    | `/api/v1/projects/{id}/roles`               | `members:read`                                   |
| `POST`   | `/api/v1/projects/{id}/roles`               | `members:write`                                  |
| `PATCH`  | `/api/v1/projects/{id}/roles/{roleId}`      | `members:write`                                  |
| `DELETE` | `/api/v1/projects/{id}/roles/{roleId}`      | `members:delete`                                 |
| `PUT`    | `/api/v1/projects/{id}/default-role`        | `members:write`                                  |
| `GET`    | `/api/v1/projects/{id}/invitations`         | `members:write`                                  |
| `POST`   | `/api/v1/projects/{id}/invitations`         | `members:write`                                  |
| `DELETE` | `/api/v1/projects/{id}/invitations/{invId}` | `members:delete`                                 |
| `POST`   | `/api/v1/invitations/{token}/accept`        | any signed-in account                            |
| `POST`   | `/api/v1/invitations/{token}/decline`       | any signed-in account                            |
| `POST`   | `/api/v1/projects/invitations/{id}/accept`  | No                                               | Accept an invitation by ID |
| `POST`   | `/api/v1/projects/invitations/{id}/decline` | No                                               | Decline an invitation by ID |
| `GET`    | `/api/v1/plan`                              | Yes                                              | Get the effective plan and limits |
| `GET`    | `/api/v1/admin/settings`                    | Yes                                              | Get operational settings |
| `PUT`    | `/api/v1/admin/settings`                    | Yes                                              | Update operational settings (admin+) |
| `GET`    | `/api/v1/audit-log`                         | Yes                                              | Project audit trail (admin+) |

Member and invitation details are documented in [Members and Invitations](./members-and-invitations). Operational
settings, plan, and the audit log are documented in [Settings, Plan, and Audit Log](./settings).

## Creating a project

```
POST /api/v1/projects
```

```json
{
  "name": "Acme Industrial",
  "slug": "acme-industrial",
  "description": "Order confirmations, dispatch notices and the monthly dispatch",
  "default_language": "en"
}
```

Only `name` is required, and the caller becomes the project **owner**.

`slug` is derived from the name when you leave it out, and must be unique across the installation — lowercase letters,
digits and hyphens. It is **immutable** once set, because it ends up in operator bookmarks and scripts, so it is worth
a moment's thought rather than accepting whatever the name produces.

`default_language` falls back to `en`.

{{< callout type="warning" title="Closed to ordinary accounts by default" >}}
This endpoint answers `403` unless the caller is a platform administrator, or the `user_project_creation`
[platform setting](/docs/admin/platform-settings) is on. It ships off, so a fresh installation is one where an
administrator makes the projects and everybody else arrives by invitation.

`GET /api/v1/projects` reports the same answer as `can_create`, which is what the console reads to decide whether to
offer the button.
{{< /callout >}}

```bash
curl -X POST http://localhost:3000/api/v1/projects \
  -H "Authorization: Bearer myk_..." \
  -H "Content-Type: application/json" \
  -d '{"name": "Acme Inc", "slug": "acme"}'
```

Response (`201`):

```json
{
    "project": {
        "id": "550e8400-e29b-41d4-a716-446655440000",
        "name": "Acme Inc",
        "slug": "acme",
        "description": "",
        "owner_id": "b8493407-52ae-4d51-8396-9c8864977976",
        "default_role_id": "",
        "created_at": "2026-05-31T10:00:00Z"
    }
}
```

Creating a duplicate slug returns `409`. A slug is derived from the name when you do not supply one.

## Listing projects

```
GET /api/v1/projects
```

Returns every project the current user is a member of. Each entry carries the caller's `role` in that project.

## Get, update, and delete the current project

```
GET    /api/v1/projects/{id}
PATCH  /api/v1/projects/{id}
DELETE /api/v1/projects/{id}
```

The project is addressed by id in the path, not by the active-project header.
`PATCH` accepts any subset of the following and leaves absent fields unchanged:

```json
{
    "name": "Acme Corporation",
    "description": "Updated description",
    "default_language": "fr"
}
```

`DELETE` removes the project and everything in it, and returns `204`. Owner only.
