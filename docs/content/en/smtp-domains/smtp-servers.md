---
title: "SMTP Servers"
description: "Configure SMTP servers for email delivery"
weight: 10
---

An SMTP server is a route out. A project can hold several, arranged into
[groups](/docs/smtp-domains/server-groups) that decide which one a given message tries and what it falls back to.

## Adding one

```
POST /api/v1/smtp-servers
```

`name` is required and is how you refer to the server everywhere else. `host` and `port` are required for an ordinary
SMTP relay — a [provider](/docs/smtp-domains/aws-ses) reached over an API has neither.

```bash
curl -X POST http://localhost:3000/api/v1/smtp-servers \
  -H "Authorization: Bearer myk_..." \
  -H "Content-Type: application/json" \
  -d '{
    "name": "primary-relay",
    "host": "smtp.relay.example.net",
    "port": 587,
    "username": "acme-industrial",
    "password": "'"$SMTP_PASSWORD"'",
    "encryption": "starttls",
    "allowed_emails": ["noreply@yourdomain.com", "alerts@yourdomain.com"]
  }'
```

`username` and `password` are both optional — a relay authenticated by IP address, or an internal one that requires
nothing, is a legitimate configuration. The password is sealed at rest and never returned by any read.

### Encryption Options

| Value      | Port | Description                     |
|------------|------|---------------------------------|
| `none`     | 25   | No encryption (not recommended) |
| `starttls` | 587  | Upgrade to TLS after connecting |
| `ssl`      | 465  | TLS from the start              |

## Testing Connections

Verify SMTP credentials and connectivity before sending via `POST /api/v1/smtp-servers/{id}/test`. This validates the
hostname, port, credentials, and encryption.

## Sender Restrictions

Use `allowed_emails` to restrict which sender addresses can use a specific SMTP server. This is useful when different
servers are configured for different brands or departments.

{{< callout type="note" >}}
Passwords are never returned in API responses.
{{< /callout >}}

## Common SMTP Providers

| Provider        | Host                                 | Port | Encryption |
|-----------------|--------------------------------------|------|------------|
| Gmail           | `smtp.gmail.com`                     | 587  | `starttls` |
| Outlook         | `smtp.office365.com`                 | 587  | `starttls` |
| Amazon SES      | `email-smtp.us-east-1.amazonaws.com` | 587  | `starttls` |
| Mailgun         | `smtp.mailgun.org`                   | 587  | `starttls` |
| Postfix (local) | `localhost`                          | 25   | `none`     |

## Grouping and order

Servers belong to [groups](/docs/smtp-domains/server-groups), which is how a send picks one and how failover decides
what to try next. A project that never creates a group has exactly one, the default, holding every server - which is the
behaviour that existed before groups.

## If a project has no server

A project that has configured **no** SMTP server delivers through the platform's
[shared pool](/docs/admin/shared-servers), if an administrator has set one up. Adding a server here takes delivery over
completely: from that point the pool is not consulted, even if the server you added is disabled or refuses the sender.

## Providers

A server row says how it is **reached**, not only where. Two providers today:

| Provider | How                                              | Fields it asks for                                      |
|----------|--------------------------------------------------|---------------------------------------------------------|
| `smtp`   | A dial                                           | host, port, encryption, optional username and password  |
| `ses`    | The [Amazon SES](/docs/smtp-domains/aws-ses) API | region, optional configuration set, optional access key |

Everything else about a row is the same whichever it is: it sits in a
[group](/docs/smtp-domains/server-groups), takes a priority, honours its allowed senders, and takes part in failover. A
group can hold both, and failover walks it in the same order - which is what lets an SES row be the primary and a plain
relay the fallback.

`provider` is set when the server is created and cannot be patched afterwards. The credentials mean something different
to each one - an SMTP login on one, an access key on the other - so switching in place would leave the wrong ones in the
fields a PATCH does not touch. Delete and recreate instead.

A row reached over an API shows no host and no encryption, and the console says so rather than leaving the cells blank:
"none" under encryption would read as cleartext when the call is HTTPS.
