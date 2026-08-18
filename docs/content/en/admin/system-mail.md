---
title: "Platform Mail"
description: "The platform's own outbound mail for invitations and password resets"
weight: 80
---

Platform mail is how Mailyard sends its **own** messages: project invitations, password reset links and signup
confirmations. It is deliberately separate from the tenant send pipeline that `POST /api/v1/emails/send` and
the [SMTP Submission](/docs/security/smtp-submission) feed.

{{< callout type="info" title="Why it is separate" >}}
Platform mail belongs to the installation, not to a tenant. Routing a password reset through the tenant pipeline would
charge some project's [plan quota](/docs/admin/plans), file the message in that project's email log, fire its webhooks,
and require it to have configured an SMTP server first.
{{< /callout >}}

It leaves through the [shared SMTP pool](/docs/admin/shared-servers), so there is nothing to configure twice.

## Setup

Two things, both at runtime - no restart, nothing in the config file.

1. **A server in the shared pool.** Any enabled row will carry platform mail.
2. **A from address**, the `platform_mail_from` [platform setting](/docs/admin/platform-settings). Empty means platform
   mail is off.

```bash
curl -X PUT http://localhost:3000/api/v1/admin/settings \
  -H "Authorization: Bearer mya_..." \
  -H "Content-Type: application/json" \
  -d '{"settings":[
        {"key":"platform_mail_from","value":"mailyard@example.com"},
        {"key":"platform_mail_from_name","value":"Mailyard"}
      ]}'
```

`server.public_url` must be set first - invitation and reset links have to be absolute, and the setting is refused
without it.

### Reserving a server

A pool server with `platform_only: true` carries platform mail and **no tenant traffic**. Platform mail prefers it over
every other row.

```bash
curl -X PATCH http://localhost:3000/api/v1/admin/shared-smtp-servers/{id} \
  -H "Authorization: Bearer mya_..." \
  -H "Content-Type: application/json" \
  -d '{ "platform_only": true }'
```

Leave it off on a small install: one shared server carrying both is fine. Set it when platform mail has to leave from a
different address or reputation than the tenants relaying through the pool.

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

Reports the address and which pool server would carry the mail. Credentials live on the pool server and are never echoed
here.

```json
{
    "system_mail": {
        "enabled": true,
        "from": "mailyard@example.com",
        "from_name": "Mailyard",
        "server": "Platform relay",
        "reserved": true
    }
}
```

`server` is empty with a `problem` beside it when the pool holds nothing usable.

And test it. With no body the check stops at the connection, with a recipient it delivers a real message:

```
POST /api/v1/admin/system-mail/test
```

```bash
curl -X POST http://localhost:3000/api/v1/admin/system-mail/test \
  -H "Content-Type: application/json" \
  -d '{ "to": "ops@example.com" }'
```

Both routes require a platform admin credential - the test makes an outbound connection.
