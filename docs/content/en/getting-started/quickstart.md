---
title: "Quick Start"
description: "Send your first email with Mailyard in minutes"
weight: 40
---

This guide walks you through sending your first email with Mailyard.

## 1. Start Mailyard

Follow the [Installation](/docs/getting-started/installation) guide to start Mailyard with Docker Compose.

## 2. Sign In

Sign in at `http://localhost:3000/app` with `admin@example.com` and the bootstrap password from the container log.

For the curl below, get a cookie jar. The login response carries no token — the session is an `HttpOnly` cookie and
nothing else:

```bash
curl -c cookies.txt -X POST http://localhost:3000/app/api/auth/login \
  -H "Content-Type: application/json" \
  -d '{"email":"admin@example.com","password":"<from the log>"}'
```

## 3. Create a Project

Everything in Mailyard belongs to a project, and a new account is a member of none — nothing is created for you. The
console shows a **New project** button on first sign-in, or:

```bash
curl -X POST http://localhost:3000/api/v1/projects \
  -b cookies.txt -H "Content-Type: application/json" \
  -d '{"name": "My App"}'
```

Keep the `id` from the response. It is a UUID, and every project-scoped call needs it in the `X-Mailyard-Project-Id`
header.

```bash
PROJECT=81af718e-f0ae-4780-a0d7-9f05b34dabcc
```

## 4. Create an API Key

```bash
curl -X POST http://localhost:3000/api/v1/api-keys \
  -b cookies.txt \
  -H "X-Mailyard-Project-Id: $PROJECT" \
  -H "Content-Type: application/json" \
  -d '{
    "name": "my-app",
    "permissions": ["emails:write", "emails:read"]
  }'
```

The plaintext is the `token` field of the response, and it is returned **once** — only `hex(sha256(token))` is stored,
so nobody can recover it afterwards, an operator with database access included. Keep it for the send below:

```bash
KEY=myk_...
```

{{< callout type="warning" title="A key with no permissions can do nothing" >}}
`permissions` is not optional in practice. Omit it and the key is minted holding nothing, and every call it makes
answers `403` — which is the safe reading of an unstated intent, not a bug. The catalogue is at
`GET /api/v1/permissions`, and `["*"]` grants everything in the project.
{{< /callout >}}

## 5. Configure an SMTP Server

```bash
curl -X POST http://localhost:3000/api/v1/smtp-servers \
  -b cookies.txt \
  -H "X-Mailyard-Project-Id: $PROJECT" \
  -H "Content-Type: application/json" \
  -d '{
    "name": "primary",
    "host": "smtp.relay.example.net",
    "port": 587,
    "username": "acme-industrial",
    "password": "'"$SMTP_PASSWORD"'",
    "encryption": "starttls"
  }'
```

## 6. Verify Your Sending Domain

Mailyard will not send from a domain this project has not verified, so this step comes before the first message rather
than after it. Add the domain, publish the TXT record it gives you, and verify:

```bash
curl -X POST http://localhost:3000/api/v1/domains \
  -b cookies.txt \
  -H "X-Mailyard-Project-Id: $PROJECT" \
  -H "Content-Type: application/json" \
  -d '{"domain": "yourdomain.com"}'

# publish the returned mailyard-verification=... TXT record at the apex, then
curl -X POST http://localhost:3000/api/v1/domains/<id>/verify \
  -b cookies.txt \
  -H "X-Mailyard-Project-Id: $PROJECT"
```

See [Domain Verification](/docs/smtp-domains/domain-verification) for the full flow. Verifying also mints the DKIM key,
so mail starts being signed at the same time.

## 7. Send Your First Email

Now the API key, which needs no project header — the key carries its project already:

```bash
curl -X POST http://localhost:3000/api/v1/emails/send \
  -H "Authorization: Bearer $KEY" \
  -H "Content-Type: application/json" \
  -d '{
    "from": "hello@yourdomain.com",
    "to": ["you@yourdomain.com"],
    "subject": "First message",
    "text": "If you are reading this, the install works.",
    "html": "<p>If you are reading this, the install works.</p>"
  }'
```

Send it to **yourself**, at the domain you just verified. A first message to somebody else is how you find out that
your SMTP route is wrong by way of their spam folder.

```json
{
  "email": { "id": "0198f6a1-3c7e-7b21-9f4d-2a5c8e0b1d33", "status": "queued" },
  "suppressed_recipients": []
}
```

## 8. Check Delivery Status

`queued` means accepted, not delivered. Poll the id you got back:

```bash
curl http://localhost:3000/api/v1/emails/0198f6a1-3c7e-7b21-9f4d-2a5c8e0b1d33/status \
  -H "Authorization: Bearer $KEY"
```

```json
{
  "id": "0198f6a1-3c7e-7b21-9f4d-2a5c8e0b1d33",
  "status": "sent",
  "attempts": 1,
  "error_message": "",
  "sent_at": "2026-01-01T00:00:01Z"
}
```

A `failed` status puts the reason in `error_message`, and it is almost always one of three things at this stage: the
SMTP credentials, the sending domain not being verified, or the server refusing the `from` address. Fix it and
`POST /api/v1/emails/{id}/retry`.

See [Email Status](/docs/email-sending/email-status) for what each state means, and why `sent` is not the same as
delivered.

## Generating a client

There is a Go client at `github.com/yousysadmin/mailyard/sdk/go`. For other languages, run `mailyard export-api-spec`
and generate a typed client from the document it writes.

## Next Steps

- [Template Email](/docs/email-sending/template-email) — Send emails using templates
- [Batch Email](/docs/email-sending/batch-email) — Send to multiple recipients
