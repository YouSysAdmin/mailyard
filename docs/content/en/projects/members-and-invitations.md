---
title: "Members and Invitations"
description: "Manage project members, roles, and invitations"
weight: 20
---

Projects are collaborative. Anybody holding `members:write` adds people by sending email invitations, and once accepted
the invitee becomes a member carrying the [role](/docs/projects/overview#roles) named at invitation time - or the
project's default role, when none was named.

These routes address the project by path id, so the `X-Mailyard-Project-Id` header is not used here.

{{< callout type="note" title="These take a session, not an API key" >}}
The routes on this page address a project by path id and read the caller's membership, which an API key does not have -
it is refused on all of them. Do this work in the console, or with a signed-in session.
{{< /callout >}}

## Members

### List members

```
GET /api/v1/projects/{id}/members
```

Response:

```json
{
    "members": [
        {
            "id": "550e8400-e29b-41d4-a716-446655440000",
            "user_id": "b8493407-52ae-4d51-8396-9c8864977976",
            "name": "Ada Lovelace",
            "email": "ada@example.com",
            "owner": true,
            "role_id": "0f7c1c62-2b3e-4a55-9f2e-6d0e2c9b1a44",
            "role_name": "Support",
            "inherited_role": false,
            "created_at": "2026-05-31T10:00:00Z"
        }
    ]
}
```

`inherited_role` says the member carries the project's default rather than a role of their own - the difference matters,
because only one of the two survives a change to the default.

### Update a member's role

```
PATCH /api/v1/projects/{id}/members/{user_id}
```

`{id}` is the project and `{user_id}` is the member's user id (the `user_id` field returned by the list endpoint).

```json
{
    "role_id": "0f7c1c62-2b3e-4a55-9f2e-6d0e2c9b1a44"
}
```

`role_id` names one of the project's own roles. Send `"role_id": ""` to clear it, which drops the member back to the
project's default role - and to nothing at all if the project has named none. A role id that does not exist in this
project answers `404`, the same answer a role belonging to another project gets.

The same endpoint grants and revokes **ownership**:

```json
{
    "owner": true
}
```

Ownership is not a role and is gated harder than one. `members:write` hands out roles, and roles are bounded by the
permission catalogue - ownership is the ability to delete the project, so only an existing owner may grant or revoke it
(`403` otherwise). A project must keep at least one owner: demoting the last one answers `409`.

Roles themselves are managed at `GET/POST /api/v1/projects/{id}/roles` and
`PATCH/DELETE /api/v1/projects/{id}/roles/{roleId}`, and the project's default role at
`PUT /api/v1/projects/{id}/default-role`. The permission catalogue the checkboxes render from is
`GET /api/v1/permissions`.

### Remove a member

```
DELETE /api/v1/projects/{id}/members/{user_id}
```

Removes the member and returns `204`. Any member may remove themself. Removing somebody else needs `members:delete`. A
project's last owner cannot be removed, by themself or by anybody (`409`) - a project with no owner could never be
deleted or handed on.

## Invitations (project side)

Outgoing invitations live under `/api/v1/projects/{id}/invitations` and need `members:write`.

### Invite a member

```
POST /api/v1/projects/{id}/invitations
```

```json
{
    "email": "newuser@example.com",
    "role_id": "0f7c1c62-2b3e-4a55-9f2e-6d0e2c9b1a44"
}
```

Only `email` is required. Leaving `role_id` out offers the project's default role, which is how most people are meant to
be invited. There is no way to invite somebody as an owner - ownership is granted afterwards, deliberately, by an owner.
If the email already belongs to a member, the request returns `409`. The inviter's email must be verified.

A pending invitation is created with a unique token, valid for **7 days**, and an invitation email is sent to the
address with an accept link of the form `/<app>/invitations?token=<token>`.

Response (`201`):

```json
{
    "invitation": {
        "id": "550e8400-e29b-41d4-a716-446655440000",
        "project_id": "81af718e-f0ae-4780-a0d7-9f05b34dabcc",
        "project": "",
        "email": "newuser@example.com",
        "role_id": "0f7c1c62-2b3e-4a55-9f2e-6d0e2c9b1a44",
        "role_name": "Support",
        "status": "pending",
        "expires_at": "2026-06-07T10:00:00Z",
        "created_at": "2026-05-31T10:00:00Z"
    }
}
```

### List pending invitations

```
GET /api/v1/projects/{id}/invitations
```

Returns the project's pending invitations as an array of the object shown above.

### Cancel an invitation

```
DELETE /api/v1/projects/{id}/invitations/{invId}
```

Deletes the pending invitation by its ID and returns `204`.

## Invitations (invitee side)

The recipient redeems an invitation with the **token** from the invitation mail. Both routes need a signed-in session
and no project header - the invitee is not a member of anything yet.

```
POST /api/v1/invitations/{token}/accept
POST /api/v1/invitations/{token}/decline
```

Neither takes a body: the token in the path is the whole claim.

On accept the invitee joins the project carrying the invited role - or the project default, if the invitation named none
or named a role that has since been deleted - and the invitation is marked accepted. Somebody who is already a member
keeps the role they have: an invitation is an offer to join, not the moment to overwrite a role somebody set
deliberately. The invitation's address must match the signed-in account, otherwise
`403`. A non-pending or expired invitation is `400`. If the account is already a member the invitation is marked
accepted and the call answers `409`.

{{< callout type="note" title="There is no list-my-invitations endpoint" >}}
An invitation is redeemed from the link in its mail, or from the copyable link the project admin sees after creating it.
Nothing enumerates the invitations addressed to you, and there is no accept-by-id route - the token is the only way in.
{{< /callout >}}
