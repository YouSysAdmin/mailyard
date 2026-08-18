---
title: "SMTP Submission"
description: "Submit mail from an existing SMTP client through Mailyard outbound pipeline"
weight: 30
---

SMTP Submission is a migration aid for teams that already send mail through an SMTP client or library and are not yet
ready to rewrite that integration against the HTTP API. A project issues SMTP username/password credentials from
Mailyard, the existing SMTP client points at Mailyard instead of its current outbound provider, and every message it
sends is parsed and fed into the **same outbound pipeline** used by `POST /api/v1/emails/send` — the same
domain-verification, suppression-list, rate-limit, and delivery handling, just reached over SMTP instead of HTTP. Teams
can cut over their SMTP client on day one and migrate call sites to the HTTP API gradually, at their own pace.

This is a separate, purpose-built listener from [Inbound Email](../inbound/overview.md): Inbound accepts anonymous mail
addressed to a verified domain, while submission requires SMTP AUTH and exists to accept mail *from* your applications,
independent of whether Inbound is enabled.

## Enabling submission

Submission is off by default. Enable it with configuration:

| Setting                       | Env                                    | Default             | Description                                                                                                                                                                                                    |
|-------------------------------|----------------------------------------|---------------------|----------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------|
| `submission.enabled`          | `MAILYARD_SUBMISSION_ENABLED`          | `false`             | Master switch for the submission listener.                                                                                                                                                                     |
| `submission.addr`             | `MAILYARD_SUBMISSION_ADDR`             | `:587`              | Bind address. Separate from `inbound.addr` - the two listeners never share a port or process state. 587 is the submission port from RFC 6409, and it is privileged: see [Privileged ports](#privileged-ports). |
| `submission.hostname`         | `MAILYARD_SUBMISSION_HOSTNAME`         | `mailyard`          | Hostname announced in the SMTP `EHLO` greeting.                                                                                                                                                                |
| `submission.max_message_size` | `MAILYARD_SUBMISSION_MAX_MESSAGE_SIZE` | `26214400` (25 MiB) | Maximum raw message size in bytes. Larger messages are rejected with `552`.                                                                                                                                    |
| `submission.rate_per_minute`  | `MAILYARD_SUBMISSION_RATE_PER_MINUTE`  | `60`                | Per-IP maximum SMTP sessions per minute. `0` disables the limit.                                                                                                                                               |
| `submission.tls.enabled`      | `MAILYARD_SUBMISSION_TLS_ENABLED`      | `true`              | Whether the listener offers STARTTLS. See below.                                                                                                                                                               |

The credential management routes (`/api/v1/smtp-credentials`) are always registered, so an operator can issue
credentials before switching the listener on. The console shows a banner while the listener is off.

### Privileged ports

`submission.addr` defaults to `:587` and `inbound.addr` to `:25`, because those are the ports clients and remote mail
servers actually try. Both are below 1024, so a process that is not root cannot bind them without help:

- **Docker** sets `net.ipv4.ip_unprivileged_port_start=0` inside containers, so the published image binds them as uid
  1000 with no extra configuration.
- **Kubernetes** does not. Grant `NET_BIND_SERVICE` in the container's
  `securityContext.capabilities`, or allow the same sysctl.
- **systemd** on bare metal: `AmbientCapabilities=CAP_NET_BIND_SERVICE`.
- **Anywhere else**, set `MAILYARD_SUBMISSION_ADDR=:2525` and
  `MAILYARD_INBOUND_ADDR=:2526` and forward the low ports to them.

Mailyard binds both listeners before it finishes starting, so a port it cannot take is a startup error with the address
in the message - not a warning buried in the log while the rest of the process comes up healthy.

### STARTTLS

`submission.tls.enabled` decides whether STARTTLS is offered. It is on by default, and the configuration file says
nothing about *which* certificate is served:

```yaml
submission:
    enabled: true
    addr: ":587"
    tls:
        enabled: true
```

The certificate is whatever is assigned to the submission listener under **Administration -> Certificates** in the
console. Nothing assigned falls through to the ACME certificate if [ACME](/docs/admin/certificates) is configured for
this hostname, and then to a self-signed pair that is generated on first use and shared by every node. So the listener
always has something to present, and replacing it is a change in the console rather than an edit and a restart.

{{< callout type="warning" title="Cleartext AUTH" >}}
With `enabled: false` the listener advertises no STARTTLS and accepts
`AUTH PLAIN` over a cleartext connection, so credentials and message content travel unencrypted. That is acceptable on a
private network, over a VPN, or behind a TLS-terminating TCP proxy - never bind it directly to the public internet with
STARTTLS off.
{{< /callout >}}

## How It Works

Submission is a front door onto the same pipeline the HTTP API uses, not a parallel one. Once the message is parsed it
is an ordinary send request, and every rule that applies to `POST /api/v1/emails/send` applies here — because it is
literally the same function.

1. Your SMTP client opens a connection to `submission.addr` and authenticates with `AUTH PLAIN`, using either an SMTP
   submission username and password or - with any username - an API key holding `emails:write` as the password.
2. Mailyard looks up the credential, confirms it is not revoked and that the connecting IP is allowed, and ties the rest
   of the session to the credential's project. `MAIL FROM`, `RCPT TO`, and `DATA` are all rejected until AUTH succeeds.
3. On `DATA`, Mailyard reads the raw message (bounded by `submission.max_message_size`), parses subject, HTML/text
   bodies, and attachments from the MIME body, and builds a send request using the SMTP envelope addresses
   (`MAIL FROM` / `RCPT TO`) rather than the parsed header addresses — the same behavior a normal MTA would have.
4. That request is handed to the same `email.Service.Send` used by the HTTP API, scoped to the credential's project and
   owning user. It goes through the identical checks: sender domain verification, suppression-list filtering, rate
   limits, plan quota, and attachment-size validation, and a normal `Email` record is created and queued for delivery.
5. The SMTP response code reflects the outcome of that call — see [Send Outcomes](#send-outcomes) below.

{{< callout type="info" title="A sandbox credential never reaches step 3" >}}
Capture happens **before the MIME body is parsed**, so a message submitted on a
[sandbox](/docs/email-sending/sandbox) credential is stored raw and the send pipeline is not entered at all. The client
still gets a success code, because from its point of view the message was accepted.

That ordering is the point: the decision lives on the credential, so a test suite cannot mail a real customer by getting
a flag wrong in the message.
{{< /callout >}}

## Issuing a Credential

SMTP credentials are project-scoped and always require a project - there is no personal/unscoped credential. Create one
from the dashboard under **Developers -> SMTP Submission** (`/app/smtp-submission`), or directly via the API. Creating,
revoking and deleting a credential all require the project `admin` role.

```
POST /api/v1/smtp-credentials
```

```bash
curl -X POST http://localhost:3000/api/v1/smtp-credentials/ \
  -H "Authorization: Bearer myk_..." \
  -H "Content-Type: application/json" \
  -d '{ "name": "Legacy app submission", "allowed_ips": ["203.0.113.0/24"] }'
```

Response (`201`):

```json
{
    "smtp_credential": {
        "id": "b4f9bd4a-f184-4e93-a40c-14e7f7181242",
        "project_id": "81af718e-f0ae-4780-a0d7-9f05b34dabcc",
        "name": "Legacy app submission",
        "username": "smtp_9f3c2a1b7e4d5601",
        "allowed_ips": [
            "203.0.113.0/24"
        ],
        "revoked": false,
        "created_at": "2026-07-20T00:00:00Z"
    },
    "password": "8b1c...e02f",
    "submission": {
        "enabled": true,
        "host": "mailyard",
        "port": "587",
        "starttls": true
    }
}
```

{{< callout type="warning" >}}
**Save the password immediately.** Like an API key, the plaintext password is only returned once, at creation
time. Mailyard stores only a hash of it.
{{< /callout >}}

Unlike API keys, an SMTP credential has no expiry and no permission list - it is either usable or revoked. It does
support an IP allowlist (`allowed_ips`), same as [API keys](api-keys.md#security-features): an empty list permits any
IP.

### Listing Credentials

```
GET /api/v1/smtp-credentials
```

Returns the credentials of the current project under `smtp_credentials`, plus a `submission` object describing the
listener (`enabled`, `host`, `port`, `starttls`) so a client can render connection settings. Passwords are never
returned - only credential metadata (`id`, `name`, `username`, `allowed_ips`, `revoked`, `created_at`, `last_used_at`).

### Revoking a Credential

Instantly disables a credential without deleting it, mirroring [API key revocation](api-keys.md#revoking-a-key):

```
POST /api/v1/smtp-credentials/{id}/revoke
```

A revoked credential fails `AUTH` immediately on its next connection attempt; any already-open session is not forcibly
disconnected but subsequent commands on a *new* session will be rejected.

### Deleting a Credential

```
DELETE /api/v1/smtp-credentials/{id}
```

Permanently removes the credential record.

## Connecting Your SMTP Client

Point your existing SMTP client at the submission host and port, with the generated username/password and no encryption:

| Setting             | Value                                                                                         |
|---------------------|-----------------------------------------------------------------------------------------------|
| Host                | `submission.addr` host (or wherever it's reachable from your app)                             |
| Port                | `submission.addr` port (default `587`)                                                        |
| Encryption          | STARTTLS when `submission.tls` is configured, otherwise none                                  |
| Auth mechanism      | `PLAIN`                                                                                       |
| Username / Password | From the credential creation response, or any username plus an API key holding `emails:write` |

```bash
swaks --server localhost --port 587 \
  --auth PLAIN --auth-user smtp_9f3c2a1b7e4d5601 --auth-password 8b1c...e02f \
  --from sender@yourdomain.com --to recipient@example.com \
  --header "Subject: Hello from SMTP submission" \
  --body "This message was submitted through Mailyard's outbound pipeline."
```

`AUTH PLAIN` is the only mechanism submission advertises; clients that default to `LOGIN` or `CRAM-MD5` should be
configured to use `PLAIN` explicitly. The listener accepts up to 100 recipients per message.

## Send Outcomes

Because submission feeds the same `email.Service.Send` used by `POST /api/v1/emails/send`, an accepted message ends up
in the identical set of [email statuses](../email-sending/email-status.md#status-values) (`pending`, `queued`,
`processing`, `sent`, `failed`, `suppressed`) — submission does not introduce any new ones. The SMTP response code the
client sees just reflects whether Mailyard accepted the message for that pipeline, not its eventual delivery outcome:

| SMTP response             | Meaning                                                                                                                                                                                                                                                             |
|---------------------------|---------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------|
| `250`                     | Accepted and handed to the outbound pipeline as an `Email` record. This includes the case where every recipient was suppressed — Mailyard still accepts the message, logs it with `suppressed` status, and does not attempt delivery, exactly as the HTTP API does. |
| `550 5.7.1`               | Sender domain is not [verified](../smtp-domains/domain-verification.md) for this project.                                                                                                                                                                           |
| `452 4.7.0` / `421 4.7.0` | [Rate limit](rate-limiting.md) or plan quota exceeded; retry later.                                                                                                                                                                                                 |
| `552 5.3.4`               | Message exceeds `submission.max_message_size`.                                                                                                                                                                                                                      |
| `554 5.6.0`               | Message could not be parsed (malformed MIME).                                                                                                                                                                                                                       |
| `451 4.3.0`               | Temporary failure (e.g. a transient error while queuing).                                                                                                                                                                                                           |
| `502 5.7.0`               | A mail command was sent before a successful `AUTH`.                                                                                                                                                                                                                 |
| `535 5.7.8`               | `AUTH` failed - unknown or revoked credential, wrong password, an API key without `emails:write`, or the connecting IP is not on the credential's `allowed_ips`.                                                                                                    |

Check the eventual delivery outcome of a message the same way you would for an API-sent email — via
`GET /api/v1/emails/{id}/status` using the `id` Mailyard assigned, or by browsing the project's email log in the
dashboard.

## Next Steps

- [Domain Verification](../smtp-domains/domain-verification.md) — required before a sender address can send mail.
- [API Keys](api-keys.md) — the HTTP-API equivalent of a submission credential, for when you're ready to migrate fully.
- [Rate Limiting](rate-limiting.md) — how send limits are enforced across both the HTTP API and submission.
- [Email Status](../email-sending/email-status.md) — track a submitted message after it's accepted.
