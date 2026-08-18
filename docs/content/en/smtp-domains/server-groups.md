---
title: "Server Groups"
description: "Named pools of SMTP servers, how a send picks one, and how failover works"
weight: 20
---

A group is a named pool of a project's SMTP servers. It does two things:

- **A send names the group, not a server.** Callers say `"smtp_group": "bulk"` and never learn a server id, so you can
  replace what is behind the name without touching a single integration.
- **It is the unit failover happens within.** A transient failure moves to the next server *in the same group*, in
  priority order.

Every project has exactly one group flagged default, created with the project. A send that names no group uses it, and
every server joins it unless you say otherwise - so an installation that never opens this page behaves exactly as it did
before groups existed.

Manage them under **Infrastructure -> Server Groups**, or at `/api/v1/smtp-server-groups`.

## How a send picks a server

In order, first match wins:

| # | Condition                         | Result                                                 |
|---|-----------------------------------|--------------------------------------------------------|
| 1 | the send sets `smtp_server_id`    | exactly that server, **no fallback**                   |
| 2 | the send sets `smtp_group`        | that group's servers                                   |
| 3 | neither                           | the project's default group                            |
| 4 | the project owns no server at all | the [shared platform pool](/docs/admin/shared-servers) |

Within a group, servers are ordered by `priority` (lowest first, then oldest) and filtered to those that are `enabled`
**and** whose `allowed_emails` admits the sender.

`allowed_emails` always wins. A server that does not accept the From address is simply not a candidate, and the send
goes out through the next one in the group - it is a property of the server, so it outranks the routing you asked for.

{{< callout type="warning" title="A pinned server never falls back" >}}
`smtp_server_id` means that server or nothing. If it is disabled, or refuses the sender, the send fails rather than
quietly leaving through a different one - a different server means a different IP and a different SPF record, which is
not a substitution to make on somebody's behalf. Use it for testing a specific server end to end, not for routing.
{{< /callout >}}

## Failover

When a send fails **transiently** - the server would not answer, timed out, or replied
`4xx` - the next server in the group is tried immediately. The whole walk costs the message **one** delivery attempt, so
a group of three does not burn three of the five retries in `worker.max_attempts`.

A **permanent** `5xx` stops the walk. That is a verdict on the message, not the server:
offering a rejected recipient to every server in turn would earn the same refusal from each and record the bounce
several times over for what is one bounce.

If every server fails transiently, the message is re-queued with the usual backoff and the whole group is tried again on
the next attempt.

`skip_dkim` is honoured **per server** during a walk. Failing over from a signing server to one that re-signs (Amazon
SES) drops the local signature for that leg, which is what the flag is for.

## Choosing a group

| Surface                                      | How                                                                      |
|----------------------------------------------|--------------------------------------------------------------------------|
| `POST /api/v1/emails/send`, `/send-template` | `"smtp_group": "<slug>"` in the body                                     |
| Console **Emails -> Send**                   | the **Server group** field, shown once a project has more than one group |
| Campaigns                                    | the **Server group** field, or `"smtp_group"` on create                  |
| SMTP relay                                   | bound to the credential - see below                                      |
| Anything else                                | the project's default group                                              |

An unknown slug is rejected at accept time with a 400 naming the group, rather than being accepted and then failing
delivery for a reason nobody can see from outside.

### The relay

An SMTP client has nowhere to put a routing field, so the group is bound to the **credential** it authenticates with.
Set **Server group** when creating a relay credential and everything submitted with it goes to that pool.

API-key relay logins have no such binding and use the default group.

## Managing groups

```
GET    /api/v1/smtp-server-groups          list groups, each with its servers
POST   /api/v1/smtp-server-groups          create   {name, slug?, description?}
PATCH  /api/v1/smtp-server-groups/{id}     rename, re-slug, or {make_default: true}
DELETE /api/v1/smtp-server-groups/{id}     delete, moving its servers to the default
```

Reads are open to any project member, writes require the project `admin` role - a group decides where a project's mail
physically leaves from, which is infrastructure rather than content.

Notes worth knowing:

- **The default group cannot be deleted.** Promote another group first with
  `make_default`, which demotes the current one in the same request. There is no way to leave a project with no default.
- **Deleting a group keeps its servers**, moving them into the default group. A group is a routing label, and losing a
  label must not lose credentials.
- **A message already queued against a deleted group** follows its servers into the default group rather than failing.
  That is where they went.
- **Changing a slug breaks callers using the old one.** The id is stable, the slug is the public handle.

## Assigning servers

A server carries `group_id` and `priority`, both settable on create and update:

```
PATCH /api/v1/smtp-servers/{id}
{ "group_id": "3f1c...", "priority": 10 }
```

Omitting `group_id` on create puts the server in the default group. A server always belongs to exactly one group - one
belonging to none would be invisible to every resolution path and would silently stop being used.
