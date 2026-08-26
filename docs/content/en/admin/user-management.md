---
title: "User Management"
description: "Manage user accounts from the admin panel"
weight: 10
---

Platform administrators create and manage the accounts that can sign in. Every route here is admin-only. Note the split:
an **account** is a person who signs in, and what they may do inside a project is a separate membership row - see
[Projects](/docs/projects/overview).

## Create a User

```
POST /api/v1/admin/users
```

```json
{
    "email": "user@example.com",
    "password": "secure-password",
    "admin": false
}
```

| Field      | Notes                                                                                                                          |
|------------|--------------------------------------------------------------------------------------------------------------------------------|
| `email`    | Required. Normalized to lower case.                                                                                            |
| `password` | Optional, minimum 12 characters. Leave it out for an account that signs in only through SSO - it then has no password to guess. |
| `admin`    | Platform administration. Defaults to false.                                                                                    |

| `admin` | What it grants                                                                                                                           |
|---------|------------------------------------------------------------------------------------------------------------------------------------------|
| `false` | An ordinary account. Everything it can do comes from project membership.                                                                 |
| `true`  | Platform administration - users, plans, platform settings, scheduled jobs, the whole security log. Also owner-equivalent in any project. |

An account created here is a LOCAL account (`account_type` 1), whether or not you give it a password, and its owner
manages their own password, two-factor authentication and passkeys from the profile page. `account_type` 2 is an account
an identity provider owns, and only the provider mints those - those credentials are managed there, not here.

Returns `409 Conflict` if the email already exists. Accounts created here are marked email-verified, so they never wait
for a confirmation link - that gate is for self-registration.

{{< callout type="note" title="No project is created here" >}}
Nothing is minted for a new account. The user either gets invited to a project or makes one from the projects page, so
an install that provisions everyone into a shared project does not collect an empty tenant per employee.
{{< /callout >}}

## List Users

```
GET /api/v1/admin/users
```

Returns every account, oldest first, as `{"users": [...]}`. The list is not paged - it is bounded by how many people
your installation has.

## Update a User

```
PATCH /api/v1/admin/users/{id}
```

```json
{
    "admin": true,
    "disabled": false,
    "email_verified": true
}
```

Every field is optional and absent means unchanged. `email`, `password`,
`admin`, `disabled` and `email_verified` can be set.

Setting `disabled` to `true` blocks sign-in. `email_verified` is the manual release valve for a self-registered account
whose confirmation mail never arrived.

{{< callout type="warning" title="You cannot change your own administrator or disabled flags" >}}
Self-edits are limited to email and password. The guard exists so the last administrator cannot demote or lock
themselves out. Another admin still can.
{{< /callout >}}

## Reset a User's Passkeys

```
DELETE /api/v1/admin/users/{id}/passkeys
```

Removes every passkey on the account, for the user who lost the device that held the only one. They sign in with their
password afterwards and enrol again. The response says how many went:

```json
{
    "removed": 2
}
```

{{< callout type="warning" title="Not on your own account" >}}
Removing a passkey from your own profile asks for your password. This endpoint does not, so allowing it on yourself
would let a hijacked admin session strip the phishing-resistant factor off the very account it hijacked. Another admin
still can, and the security log records who did.
{{< /callout >}}

## List a User's Projects

```
GET /api/v1/admin/users/{id}/projects
```

Every project the account is a member of, so you can see what it touches before disabling or deleting it.

## Delete a User

```
DELETE /api/v1/admin/users/{id}
```

Removes the account immediately and answers `204`. Deleting your own account is refused, and an id that names nobody is
`404` so a double-click reads as "already gone" rather than silent success.

{{< callout type="warning" title="There is no undo and no grace period" >}}
Deletion is immediate. If you want an account kept but locked out, set `disabled`
instead - that is reversible and preserves the audit trail.
{{< /callout >}}

## Disable 2FA for a User

For the person who lost their authenticator:

```
DELETE /api/v1/admin/users/{id}/2fa
```

Refused on your own account. The self-service path in your profile proves possession with a valid code, and this
endpoint deliberately does not - allowing it on yourself would turn any hijacked admin session into a 2FA bypass for
that admin. Returns
`400` if the account has no second factor enrolled. The reset is recorded in the security log.

## Revoke All Sessions

Force an account to sign in again everywhere:

```
POST /api/v1/admin/users/{id}/revoke-sessions
```

Answers `{"revoked": 3}`. The node handling the request is consistent immediately and other nodes converge within the
session cache TTL. Also recorded in the security log.
