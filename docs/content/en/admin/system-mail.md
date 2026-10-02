---
title: "Platform Mail"
description: "The platform's own outbound mail for invitations, password resets and alerts"
weight: 80
---

Platform mail is how Mailyard sends its **own** messages: project invitations, password reset links, signup
confirmations and alert mail. It is sent as a message of a project you choose - the one named by the
`platform_mail_project` [platform setting](/docs/admin/platform-settings) - and goes out through whatever that project
sends with: its SMTP servers, SES, [relay nodes](/docs/admin/relay-nodes), or the
[shared pool](/docs/admin/shared-servers) when it owns no server of its own. It is signed with the project's DKIM key.

{{< callout type="info" title="A project's servers, not a project's mail" >}}
A password reset is marked as system mail. The project lends its servers, failover and signature and nothing else: the
message is never charged to the project's [plan quota](/docs/admin/plans), filtered by its suppressions, tracked, given
its default headers, sent to its webhooks, or shown in its email log and dashboard.
{{< /callout >}}

Most installations already have such a project, because the platform sends its own marketing from the same domain.
That is why there is no separate platform DKIM key to publish: the project's domain verification covers both.

## Setup

Everything is at runtime - no restart, nothing in the config file.

1. **Create the project** the platform will send as, if it does not exist yet. On a fresh install the first
   administrator lands on the projects page and makes one.
2. **Verify the domain** of the address platform mail will come from, under that project's
   [Domains](/docs/email-sending/domains), and publish its DKIM record.
3. **Give the project a way to send** - an SMTP server, SES, a relay node, or leave it to the shared pool.
4. **Set the two settings**: the address and the project.

```bash
curl -X PUT http://localhost:3000/api/v1/admin/settings \
  -H "Authorization: Bearer mya_..." \
  -H "Content-Type: application/json" \
  -d '{"settings":[
        {"key":"platform_mail_from","value":"mailyard@example.com"},
        {"key":"platform_mail_from_name","value":"Mailyard"},
        {"key":"platform_mail_project","value":"<project id>"}
      ]}'
```

In the console both live on **Admin → Settings**, where the project is picked from a list of names.

The write is refused when the project does not exist, when `platform_mail_from` is on a domain that project has not
verified, or when the project has no server that can carry the address - so a setting that saves is a setting that
sends. `server.public_url` must be set as well: invitation and reset links have to be absolute.

Platform mail is off until **both** the address and the project are set.

## How a Message Travels

The message is **queued** like any message of the project and delivered by a worker, with the same failover between the
project's servers. Its body is cleared the moment it is sent or given up on: a reset link is a credential and nothing
reads it back. A delivery failure is in the server log, as for any message, and never in the project's email log.

A hard bounce on a system message is still recorded against the project, so the address is suppressed for the project's
own mail. It does not stop later system mail to that address.

## What Runs Without It

Platform mail being off never breaks a flow, it only changes the hand-off:

| Feature                                                       | With platform mail                                                                             | Without                                                                                                                         |
|---------------------------------------------------------------|------------------------------------------------------------------------------------------------|---------------------------------------------------------------------------------------------------------------------------------|
| [Project invitations](/docs/projects/members-and-invitations) | The invitee is emailed the accept link. The link is still returned to the inviter as a backup. | The link is returned to the inviter, who passes it along out of band.                                                           |
| Password reset                                                | Users can reset their own password from the sign-in page.                                      | The endpoint reports that the feature is unavailable and the sign-in page hides the link. An admin resets the password instead. |

The create-invitation response carries `emailed: true|false` so a client can tell which happened.

## Checking It

```
GET /api/v1/admin/system-mail
```

Reports the address and the project platform mail is sent as. No credential is echoed - the project's servers are
configured on their own pages.

```json
{
    "system_mail": {
        "enabled": true,
        "from": "mailyard@example.com",
        "from_name": "Mailyard",
        "project": "Platform",
        "project_id": "0195d1a2-7c3e-7f00-8000-0000000000aa"
    }
}
```

When something stops the mail, `problem` says what: a setting that is not set, a project that no longer exists, a domain
the project has not verified, or no server that can carry the address.

And test it. With no body the check stops at whether the project can carry the address - nothing is dialled - and with
a recipient it queues a real message:

```
POST /api/v1/admin/system-mail/test
```

```bash
curl -X POST http://localhost:3000/api/v1/admin/system-mail/test \
  -H "Content-Type: application/json" \
  -d '{ "to": "ops@example.com" }'
```

Both routes require a platform admin credential.
