---
title: "Configuration"
description: "Environment variables and configuration options"
weight: 30
---

Mailyard reads `./mailyard.yaml` (override with `--config`) and every key can also be set as an environment variable.
The variable name is the YAML path, uppercased, with dots replaced by underscores and a `MAILYARD_` prefix —
`server.public_url`
is `MAILYARD_SERVER_PUBLIC_URL`. Environment wins over the file, and the file is optional, so a deployment can be
configured entirely from the environment.

Put secrets in the environment rather than the file: `MAILYARD_AUTH_JWT_SECRET`,
`MAILYARD_DATABASE_CRYPTO_ENCRYPTION_KEY`, `MAILYARD_DATABASE_DSN` and
`MAILYARD_RELAY_NODES_AUTO_REGISTER_TOKEN`.

## Server

| Variable                          | Default | Description                                                                                                                                                                                                                                                                                     |
|-----------------------------------|---------|-------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------|
| `MAILYARD_SERVER_ADDR`            | `:3000` | Bind address for the HTTP console and API                                                                                                                                                                                                                                                       |
| `MAILYARD_SERVER_PUBLIC_URL`      | —       | Public base URL, e.g. `https://mail.example.com`. Required for anything that has to build an absolute link: invitations, password resets, OIDC redirects, tracking links and hosted unsubscribe pages. It also decides the `Secure` flag on the session cookie                                  |
| `MAILYARD_SERVER_TRUSTED_PROXIES` | —       | Proxy IPs or CIDRs whose `X-Forwarded-For` is believed. Empty means the direct peer is the client, which is correct for direct exposure. Set it to your load balancer's CIDRs when TLS terminates upstream, otherwise the rate limiter, audit log and access log all record the proxy's address. **List every hop**, not just the nearest one — see [Trusting a proxy](/docs/security/rate-limiting) |
| `MAILYARD_SERVER_MAX_CONCURRENT_REQUESTS` | `4096` | Requests served at once. A request body is read into memory before any handler runs, so this is what bounds how many large uploads can exist at the same time. `0` removes the cap |
| `MAILYARD_SERVER_TLS_ENABLED`     | `false` | Terminate TLS here rather than at a proxy in front — see [TLS](#tls)                                                                                                                                                                                                                            |

{{< callout type="warning" title="public_url must match the scheme and host you actually browse" >}}
Two ways to get locked out of a console that is working perfectly, neither of which reports anything:

- **Scheme.** `https://…` sets `Secure` on the session cookie. Open the console over plain `http://` and the login
  returns 200, the browser discards the cookie, and every request after it is unauthenticated.
- **A bare IP address.** Safari will not keep a cookie for a host like
  `http://192.168.1.10:3000` — an IP literal has no registrable domain and WebKit needs one. Chrome and Firefox are fine
  with it. Give the installation a hostname instead.

Both land on the login page with `authentication required`.
{{< /callout >}}

The console frontend and the documentation you are reading are compiled into the binary, so there is no asset path to
configure.

## Database (PostgreSQL)

PostgreSQL is the only supported engine. One connection string, no host/port/user parts - put it in
`MAILYARD_DATABASE_DSN` rather than YAML so the password stays out of the config file.

| Variable                         | Default | Description                                                                                            |
|----------------------------------|---------|--------------------------------------------------------------------------------------------------------|
| `MAILYARD_DATABASE_DSN`          | —       | Required. `postgres://user:pass@host:5432/mailyard?sslmode=require`                                    |
| `MAILYARD_DATABASE_REPLICA_DSNS` | —       | Read-only followers, comma separated. See [Read replicas](/docs/getting-started/scaling#read-replicas) |

```yaml
database:
    dsn: postgres://mailyard:secret@localhost:5432/mailyard?sslmode=disable
```

Migrations run automatically at startup (goose, embedded in the binary), so a fresh empty database is all the service
needs.

PostgreSQL is the only service to configure: it holds the delivery
queue and is how several nodes coordinate - see
[Scaling out](/docs/getting-started/scaling).

## Logging

One sink, written exactly where you point it.

| Variable                  | Default  | Description                                                                                                                        |
|---------------------------|----------|------------------------------------------------------------------------------------------------------------------------------------|
| `MAILYARD_LOGGING_LEVEL`  | `info`   | `trace`, `debug`, `info`, `warn` or `error`                                                                                        |
| `MAILYARD_LOGGING_FORMAT` | `json`   | `json` for shippers, `text` for reading                                                                                            |
| `MAILYARD_LOGGING_OUTPUT` | `stdout` | `stdout`, `stderr`, or a file path (opened for append, created when missing)                                                       |
| `MAILYARD_LOGGING_COLOR`  | `true`   | ANSI colors, `text` format only. Honored only when the output is an interactive terminal, so a file or a piped stdout stays plain. |

An invalid `format` or `level` fails at startup rather than falling back, because a silent fallback to human-readable
text produces a log your shipper cannot parse and never says why.

The first-run bootstrap password is written straight to stderr, not through the logger, so it is still visible when logs
are directed to a file.

## Authentication

| Variable                             | Default | Description                                                                                                                                                                                                                                                     |
|--------------------------------------|---------|-----------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------|
| `MAILYARD_AUTH_JWT_SECRET`           | —       | **Required.** Signs session JWTs. Generate with `openssl rand -hex 32`. Every other HMAC in the system (OIDC state cookie, tracking signer) uses a separate subkey derived from it, never the raw value                                                         |
| `MAILYARD_AUTH_LOCAL_ENABLED`        | `false` | Email and password sign-in                                                                                                                                                                                                                                      |
| `MAILYARD_AUTH_LOCAL_EMAIL`          | —       | Bootstrap admin address. On first start against an empty users table this account is created with a generated password, printed to stderr once                                                                                                                  |
| `MAILYARD_AUTH_SESSION_TTL`          | `12h`   | Session cookie lifetime, as a Go duration                                                                                                                                                                                                                       |
| `MAILYARD_AUTH_REGISTRATION_ENABLED` | `false` | Opens `POST /app/api/auth/register` for public self-signup. Off by default: this is an operator console, and an open signup on an internet-facing install means strangers with accounts. New accounts are always plain users — an admin grants roles afterwards |
| `MAILYARD_AUTH_DISABLED`             | `false` | Removes the authentication gate from every API surface. Local development only                                                                                                                                                                                  |

{{< callout type="danger" >}}
`MAILYARD_AUTH_DISABLED=true` makes every console endpoint reachable without credentials, including the platform-admin
routes. It exists so a developer can poke at the API without logging in. Never set it on anything reachable from a
network you do not control.
{{< /callout >}}

Single sign-on is configured per provider in the console under **Admin → OAuth Providers**, not through the environment,
because the settings are stored in the database and editable at runtime. See
[OAuth Providers](/docs/admin/oauth-providers).

## Encryption at rest

| Variable                                  | Default      | Description                                                                                                                                                                                                                                                                                                                                    |
|-------------------------------------------|--------------|------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------|
| `MAILYARD_DATABASE_CRYPTO_ENCRYPTION_KEY` | **required** | Keys the at-rest encryption of secrets stored in the database: tenant SMTP passwords, DKIM private keys, TOTP secrets, OAuth client secrets, certificate private keys. At least 32 characters - generate with `openssl rand -hex 32`. The AES-256 key is derived with HKDF-SHA256, which stretches a short secret without adding entropy to it |

**Required.** Mailyard refuses to start without it. It used to fall back to base64 encoding with a warning at startup,
which meant an install that missed the warning kept every one of those secrets in a reversible encoding, in columns
documented as encrypted. Refusing to boot is the honest version of that warning.

Changing it later orphans every row already sealed under the old key, so treat it like a database credential.

A stored value is `base64(nonce||ciphertext)` and nothing else. Older releases wrote an `enc:` prefix in front of it to
mark sealed values apart from the base64 fallback — with the fallback gone the prefix had nothing left to say and it was
removed. Nothing you do is affected: the bytes underneath are unchanged.

## Amazon SES notifications

SES replaces the envelope sender with its own, so no return path can collect its bounces. It reports over SNS instead.
See
[Bounce Handling](/docs/smtp-domains/bounce-handling).

**There is nothing to configure here.** `POST /webhooks/ses` always exists, and the SNS topic ARN is set on the SMTP
server that uses SES — under **SMTP Servers**, or **Shared SMTP Pool** for a platform-owned one.

That is deliberate. SES belongs to one server: a project configures its own SES account as its own server, so a
platform-wide list could only ever serve an operator who owned the account themselves, and a tenant had no way to
register a topic at all.

The endpoint is public, because SNS presents no session and no API key. Two things authenticate a notification, and it
needs both: the SNS signature, verified against a certificate fetched only from `sns.<region>.amazonaws.com`, and the
topic being configured on a server. The signature alone proves Amazon sent the message, not that **your** account did —
anyone can open an AWS account and point a topic at your URL.

With no server carrying a topic, nothing is accepted. That is why the endpoint needs no switch: it gates itself on the
data.

A notification is additionally required to be about a message that actually left through a server publishing to that
topic. Attribution still comes from the
`X-Mailyard-Email-Id` header — this is what stops one tenant's topic reporting on another tenant's mail.

## Metrics

| Variable                   | Default | Description                                                                                 |
|----------------------------|---------|---------------------------------------------------------------------------------------------|
| `MAILYARD_METRICS_ENABLED` | `false` | Serves the Prometheus scrape endpoint at `GET /metrics`                                     |
| `MAILYARD_METRICS_TOKEN`   | —       | When set, the endpoint requires it as a bearer token. Leave empty only on a trusted network |

## Rate Limiting

Fixed-window counters held in process memory, so on a multi-node deployment the effective ceiling is the value here
times the node count. Size accordingly, or terminate the limit at a shared proxy instead. Zero on an individual limit
disables just that one.

| Variable                              | Default | Description                                                       |
|---------------------------------------|---------|-------------------------------------------------------------------|
| `MAILYARD_RATELIMIT_ENABLED`          | `true`  | Master switch for all three HTTP limiters                         |
| `MAILYARD_RATELIMIT_LOGIN_PER_MINUTE` | `10`    | `POST /app/api/auth/login` per client IP                          |
| `MAILYARD_RATELIMIT_OIDC_PER_MINUTE`  | `30`    | OIDC callback per client IP                                       |
| `MAILYARD_RATELIMIT_API_PER_MINUTE`   | `120`   | `/api/v1` per API key, or per IP for callers with no usable token |

Per-project sending volume is a separate mechanism: it comes from the plan assigned to the project, not from
configuration. See [Plans](/docs/admin/plans). The SMTP listeners have their own per-IP session limits,
`MAILYARD_SUBMISSION_RATE_PER_MINUTE` and `MAILYARD_INBOUND_RATE_PER_MINUTE`.

## Worker

The delivery worker runs inside the server process, so there is nothing to enable. Running more nodes runs more
workers - every `serve` node claims from the same queue.

Exactly one node carries `--init`, which applies pending migrations. The rest start without it and skip goose entirely.
Two nodes migrating an empty database at the same time race, and a node that starts before anybody has applied the
schema says so rather than failing on the first query. See [Scaling out](/docs/getting-started/scaling).

These settings are **per node**. Three nodes at concurrency 4 give you twelve parallel deliveries, not four.

| Variable                           | Default | Description                                                                                                                                                                                                         |
|------------------------------------|---------|---------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------|
| `MAILYARD_WORKER_CONCURRENCY`      | `4`     | Parallel delivery goroutines                                                                                                                                                                                        |
| `MAILYARD_WORKER_POLL_INTERVAL`    | `2s`    | How often the queue is checked for due work. A send also wakes the worker immediately - on every node, not just the one that accepted it - so this only governs scheduled and retried mail                          |
| `MAILYARD_WORKER_MAX_ATTEMPTS`     | `5`     | Delivery attempts before an email is marked failed                                                                                                                                                                  |
| `MAILYARD_WORKER_RETRY_BASE_DELAY` | `30s`   | Seeds the exponential backoff: base × 2^(attempt−1)                                                                                                                                                                 |
| `MAILYARD_WORKER_RETRY_MAX_DELAY`  | `1h`    | Ceiling for that backoff                                                                                                                                                                                            |
| `MAILYARD_WORKER_CLAIM_TIMEOUT`    | `5m`    | Re-queues `processing` rows older than this, which is how a crashed node's in-flight mail is recovered. Keep it comfortably above your slowest SMTP delivery, or a slow send is re-queued while it is still running |

## Campaigns

| Variable                          | Default | Description                                                                                      |
|-----------------------------------|---------|--------------------------------------------------------------------------------------------------|
| `MAILYARD_CAMPAIGN_BATCH_SIZE`    | `100`   | Recipients rendered and queued per batch. The campaign's own send rate throttles between batches |
| `MAILYARD_CAMPAIGN_POLL_INTERVAL` | `5s`    | How often the runner looks for due campaigns                                                     |

## Webhooks

| Variable                                 | Default | Description                                            |
|------------------------------------------|---------|--------------------------------------------------------|
| `MAILYARD_WEBHOOK_MAX_ATTEMPTS`          | `3`     | Total delivery attempts per event, including the first |
| `MAILYARD_WEBHOOK_TIMEOUT`               | `10s`   | Per-request timeout, as a Go duration                  |
| `MAILYARD_WEBHOOK_RETRY_DELAY`           | `10s`   | Fixed wait between attempts, as a Go duration          |
| `MAILYARD_WEBHOOK_ALLOW_PRIVATE_TARGETS` | `false` | Permit deliveries to private and reserved addresses    |

{{< callout type="warning" title="Private webhook targets" >}}
Webhook URLs are chosen by project members, not operators. By default a delivery to a loopback, RFC 1918, link-local or
otherwise reserved address is refused at connection time, which stops a member from aiming one at the cloud metadata
service (`169.254.169.254`), at a neighbouring container, or at this process's own admin API and using the platform as a
proxy into your network. Redirects are not followed, for the same reason.

Set `MAILYARD_WEBHOOK_ALLOW_PRIVATE_TARGETS=true` only when your receivers really do live on the same private network as
this deployment, and understand that it re-opens that path to anyone who can create a webhook.

{{< /callout >}}

## Delivery

| Variable                                   | Default | Description                                                                                                                         |
|--------------------------------------------|---------|-------------------------------------------------------------------------------------------------------------------------------------|
| `MAILYARD_SENDING_AUTO_SUPPRESS_ON_REJECT` | `true`  | Add a recipient to the suppression list (and stop retrying) after a permanent `5xx` rejection at `RCPT TO`, e.g. `550 user unknown` |

## Email Verification

Controls the `POST /api/v1/emails/verify` endpoint: syntax, disposable and role-account checks, and an MX lookup with an
A/AAAA fallback per RFC 5321. Off by default because the MX check makes outbound DNS queries, which an operator on a
locked-down network should opt into rather than discover. Results are cached in process memory, so each node builds its
own cache and the TTLs below are per node.

| Variable                               | Default | Description                                                   |
|----------------------------------------|---------|---------------------------------------------------------------|
| `MAILYARD_EMAIL_VERIFY_ENABLED`        | `false` | Enable the endpoint                                           |
| `MAILYARD_EMAIL_VERIFY_CACHE_TTL`      | `24h`   | How long an address-level verdict is reused, as a Go duration |
| `MAILYARD_EMAIL_VERIFY_MX_CACHE_TTL`   | `1h`    | How long a domain's MX answer is reused                       |
| `MAILYARD_EMAIL_VERIFY_LOOKUP_TIMEOUT` | `5s`    | Bounds one DNS resolution                                     |

There is deliberately no SMTP probe, so `mailbox_verified` is always false. See
[Email Verification](/docs/email-sending/email-verification) for why.

## Platform mail

The platform's own outbound mail: project invitations, password resets and signup confirmations. Deliberately separate
from the tenant send pipeline, so it never consumes a project's plan quota, appears in a project email log, or needs a
tenant to have configured an SMTP server.

**There is nothing to configure here.** There used to be a `system_mail` block with its own host, port and credentials —
it is gone. It meant an operator configured platform SMTP twice, once in the file and once in the console, and the
console could show neither.

Platform mail now leaves through the **shared SMTP pool**, and the address it sends from is a platform setting rather
than config:

| Setting                   | Where                                | Description                                                               |
|---------------------------|--------------------------------------|---------------------------------------------------------------------------|
| `platform_mail_from`      | Admin → Settings                     | Envelope sender. Empty means platform mail is off                         |
| `platform_mail_from_name` | Admin → Settings                     | Optional display name                                                     |
| `platform_only`           | Admin → Shared SMTP Pool, per server | Reserves that server for platform mail, so no tenant is routed through it |

The address is a setting and not a config key on purpose: everything else about the pool is edited in the console by the
same administrator, and an address that needs a restart to correct is the one thing they cannot fix when it is wrong.

`platform_mail_from` requires `MAILYARD_SERVER_PUBLIC_URL`, since every link it sends is absolute — and that requirement
is checked at the moment somebody writes the address, which is where it is actionable.

With no address set, invitations still work — the console returns a copyable link instead — but password reset is not
offered at all. Status and a connection test live at `GET`/`POST /api/v1/admin/system-mail[/test]`. See
[System Mail](/docs/admin/system-mail).

## Relay nodes

{{< callout type="warning" title="Enterprise edition" >}}
Neither block below applies to the community edition, and a community binary
refuses to start with `relay_nodes.enabled` set.
{{< /callout >}}

A relay node is a machine somewhere else — often another provider, often another continent — that delivers straight to
recipient mail exchangers from its own address, and can run an MX of its own. Two config blocks, and they point in
opposite directions.

### The control plane, on an API node

| Variable                                   | Default             | Description                                                                                      |
|--------------------------------------------|---------------------|--------------------------------------------------------------------------------------------------|
| `MAILYARD_RELAY_NODES_ENABLED`             | `false`             | Registers the enrolment endpoints at all. Off means the routes do not exist rather than refusing |
| `MAILYARD_RELAY_NODES_AUTO_REGISTER_TOKEN` | —                   | The shared secret a platform node presents to enrol. Required when enabled                       |
| `MAILYARD_RELAY_NODES_CA_COMMON_NAME`      | `Mailyard Relay CA` | Names the authority in its own certificate                                                       |

A leaked token is not a disaster on its own: enrolment lands in **pending**, and a node carries no mail until somebody
approves it. Whether that approval is automatic is the `relay_nodes_auto_approve` platform setting, off by default.

A project's own node enrols with an API key carrying `relay:enroll` instead of this token, and is approved by an
administrator of that project.

### The node itself

Read only by `mailyard relay`. A node ships neither the database DSN nor the encryption key.

| Variable                                   | Default         | Description                                                               |
|--------------------------------------------|-----------------|---------------------------------------------------------------------------|
| `MAILYARD_RELAY_NODE_CONTROL_URL`          | —               | Where the platform lives. The node only ever connects out                 |
| `MAILYARD_RELAY_NODE_ENROLL_TOKEN`         | —               | The shared secret, or a project API key. First run only                   |
| `MAILYARD_RELAY_NODE_ADDR`                 | `:2587`         | Where delivery workers connect. Implicit TLS, client certificate required |
| `MAILYARD_RELAY_NODE_HOSTNAME`             | —               | The certificate name workers dial AND the HELO it announces               |
| `MAILYARD_RELAY_NODE_SERVER_GROUP`         | —               | Slug of the project server group to join. Ignored by a platform node      |
| `MAILYARD_RELAY_NODE_SPOOL_DIR`            | `./relay-spool` | The queue. Metadata in bbolt, message bytes as files beside it            |
| `MAILYARD_RELAY_NODE_MAX_LIFETIME`         | `72h`           | How long a message may keep failing before it is given up on              |
| `MAILYARD_RELAY_NODE_HEARTBEAT_INTERVAL`   | `2m`            | How often the node reports in                                             |
| `MAILYARD_RELAY_NODE_DELIVERY_CONCURRENCY` | `8`             | Simultaneous outbound sessions                                            |
| `MAILYARD_RELAY_NODE_SMTP_PORT`            | `25`            | Destination port. Configurable only so a test can point it elsewhere      |
| `MAILYARD_RELAY_NODE_IPV6`                 | `false`         | Allow delivery over IPv6                                                  |

IPv6 is off by default, and that default is the point: a box with a AAAA address will happily prefer v6, where receivers
want a PTR and SPF coverage a v4-only setup does not have, and the mail is refused for reasons that look nothing like
the cause.

The control channel is plain HTTPS with a nil transport, so `HTTPS_PROXY`,
`ALL_PROXY` and SOCKS5 work with no configuration — which is the whole point for a node behind a national firewall or a
VPN.

{{< callout type="warning" title="The node verifies your certificate" >}}
It uses the system trust store. If the platform serves a self-signed certificate, no node will enrol until that root is
installed on each machine.
{{< /callout >}}

### A node that also receives

| Variable                                       | Default    | Description                                                                     |
|------------------------------------------------|------------|---------------------------------------------------------------------------------|
| `MAILYARD_RELAY_NODE_INBOUND_ENABLED`          | `false`    | Take mail on port 25 and forward it over the control channel                    |
| `MAILYARD_RELAY_NODE_INBOUND_ADDR`             | `:25`      | Where the internet delivers                                                     |
| `MAILYARD_RELAY_NODE_INBOUND_MAX_MESSAGE_SIZE` | `26214400` | Bytes per received message                                                      |
| `MAILYARD_RELAY_NODE_INBOUND_RATE_PER_MINUTE`  | `120`      | New sessions per client IP per minute                                           |
| `MAILYARD_RELAY_NODE_INBOUND_TLS_CERT`         | —          | STARTTLS certificate on the node's own disk. Empty generates a self-signed pair |
| `MAILYARD_RELAY_NODE_INBOUND_TLS_KEY`          | —          | The matching key. Both or neither                                               |

Point the MX record at the node rather than at Mailyard. See
[Relay Nodes](/docs/smtp-domains/relay-nodes).

## Sending Limits

Caps applied to every send, on both the console API and the machine API.

| Variable                                     | Default    | Description                               |
|----------------------------------------------|------------|-------------------------------------------|
| `MAILYARD_SENDING_MAX_RECIPIENTS`            | `50`       | Recipients per message                    |
| `MAILYARD_SENDING_MAX_ATTACHMENT_SIZE`       | `10485760` | Bytes per attachment (10 MiB)             |
| `MAILYARD_SENDING_MAX_TOTAL_ATTACHMENT_SIZE` | `26214400` | Bytes of attachments per message (25 MiB) |

{{< callout type="warning" title="These size limits set the HTTP body limit" >}}
Attachments travel base64-encoded, which inflates them by 4/3, so the request body cap is computed from
`max_total_attachment_size` at startup rather than configured separately. Raising it therefore raises the body cap for
**every** endpoint, because Fiber has no per-route body limit. Lower these values if the deployment does not send large
files.

Two configurations are refused at startup: a `max_attachment_size` larger than
`max_total_attachment_size` (unreachable, and it reads as a promise the total overrides), and a total above 256 MiB.
{{< /callout >}}

See [Attachments](/docs/email-sending/attachments) for the request shape and the
`GET /api/v1/emails/limits` endpoint that reports the effective values.

## Sender Authentication

| Variable                                | Default | Description                                                                                                                                                                                                                                                                                   |
|-----------------------------------------|---------|-----------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------|
| `MAILYARD_SENDING_BOUNCE_ADDRESS`       | —       | Return path for mail leaving through the **shared platform pool** only, where the sending IPs are ours. A project sending through its own server sets its own under Project settings. Empty leaves `MAIL FROM` as the From address. See [Bounce Handling](/docs/smtp-domains/bounce-handling) |
| `MAILYARD_SENDING_SPF_INCLUDE`          | —       | Host tenants should name in their SPF record, e.g. `_spf.mail.example.com`. Empty means the console tells the operator to set it rather than printing a placeholder.                                                                                                                          |
| `MAILYARD_INBOUND_REJECT_ON_DMARC_FAIL` | `false` | Refuse received mail when the From domain publishes `p=reject` and nothing it vouches for passed                                                                                                                                                                                              |

Outbound mail is DKIM signed automatically for every domain a project has verified. Keys are RSA-2048, generated when
ownership verifies, and the private half is encrypted with `database.crypto.encryption_key` before it reaches the
database. There is no variable to turn signing on: a verified domain with a key gets signed mail.

Inbound mail is always authenticated and the verdict always stored.
`MAILYARD_INBOUND_REJECT_ON_DMARC_FAIL` only decides whether a failure is also a refusal.
See [Domain Verification](/docs/smtp-domains/domain-verification).

## Attachment Storage

Where attachment bytes live. The default keeps them inline as base64 in the database, which needs no configuration at
all. The `fs` and `s3` backends move the bytes out and store only metadata plus a storage key.

| Variable                             | Default            | Description                                          |
|--------------------------------------|--------------------|------------------------------------------------------|
| `MAILYARD_STORAGE_BACKEND`           | —                  | Empty for inline, or `fs` or `s3`                    |
| `MAILYARD_STORAGE_FS_PATH`           | `data/attachments` | Base directory for the `fs` backend                  |
| `MAILYARD_STORAGE_S3_ENDPOINT`       | —                  | S3-compatible endpoint (e.g. MinIO, R2)              |
| `MAILYARD_STORAGE_S3_REGION`         | —                  | S3 region                                            |
| `MAILYARD_STORAGE_S3_BUCKET`         | —                  | S3 bucket name, required for the `s3` backend        |
| `MAILYARD_STORAGE_S3_ACCESS_KEY`     | —                  | S3 access key                                        |
| `MAILYARD_STORAGE_S3_SECRET_KEY`     | —                  | S3 secret key                                        |
| `MAILYARD_STORAGE_S3_USE_PATH_STYLE` | `false`            | Path-style addressing, required by some MinIO setups |

## Inbound Email

| Variable                                | Default    | Description                                                                                                                                                          |
|-----------------------------------------|------------|----------------------------------------------------------------------------------------------------------------------------------------------------------------------|
| `MAILYARD_INBOUND_ENABLED`              | `false`    | Master toggle — enables the MX listener                                                                                                                              |
| `MAILYARD_INBOUND_ADDR`                 | `:25`      | Bind address, host and port together. 25 is where MX delivery arrives, and it is privileged: see [Privileged ports](/docs/security/smtp-submission#privileged-ports) |
| `MAILYARD_INBOUND_HOSTNAME`             | `mailyard` | Hostname announced in the SMTP `EHLO` greeting — should match the MX record                                                                                          |
| `MAILYARD_INBOUND_MAX_MESSAGE_SIZE`     | `26214400` | Max raw message size in bytes (default 25 MiB)                                                                                                                       |
| `MAILYARD_INBOUND_RATE_PER_MINUTE`      | `120`      | Per-IP max SMTP sessions per minute (`0` disables)                                                                                                                   |
| `MAILYARD_INBOUND_REJECT_ON_DMARC_FAIL` | `false`    | See [Sender Authentication](#sender-authentication)                                                                                                                  |
| `MAILYARD_INBOUND_TLS_ENABLED`          | `true`     | Offer STARTTLS                                                                                                                                                       |

Recipients are gated at `RCPT TO` on domains the project verified by DNS TXT — there is no authentication on this
listener, and mail for anything unclaimed is refused. See [Inbound Email](/docs/inbound/receiving).

## SMTP submission

Outbound submission for applications that speak SMTP rather than HTTP. Off by default,
`MAILYARD_SUBMISSION_ENABLED=true` turns it on and `MAILYARD_SUBMISSION_ADDR`
defaults to `:587`. The full table, including AUTH and STARTTLS, is in
[SMTP Submission](/docs/security/smtp-submission).

## CORS

Off by default, and it should stay off unless something needs it: the console is served from the same origin as the API,
so it never does. This exists for a separately hosted front end or a browser-based integration.

```yaml
cors:
    enabled: true
    allowed_origins:
        - https://app.example.com
    allow_credentials: true
```

| Key                      | Env                               | Default                                            | Description                                               |
|--------------------------|-----------------------------------|----------------------------------------------------|-----------------------------------------------------------|
| `cors.enabled`           | `MAILYARD_CORS_ENABLED`           | `false`                                            | Turn CORS on                                              |
| `cors.allowed_origins`   | `MAILYARD_CORS_ALLOWED_ORIGINS`   | -                                                  | Full origins including the scheme. Required when enabled. |
| `cors.allowed_methods`   | `MAILYARD_CORS_ALLOWED_METHODS`   | `GET,POST,PUT,PATCH,DELETE,OPTIONS`                |                                                           |
| `cors.allowed_headers`   | `MAILYARD_CORS_ALLOWED_HEADERS`   | `Content-Type,Authorization,X-Mailyard-Project-Id` |                                                           |
| `cors.expose_headers`    | `MAILYARD_CORS_EXPOSE_HEADERS`    | -                                                  | Response headers the browser may read                     |
| `cors.allow_credentials` | `MAILYARD_CORS_ALLOW_CREDENTIALS` | `false`                                            | Let the browser send the session cookie cross-origin      |
| `cors.max_age`           | `MAILYARD_CORS_MAX_AGE`           | `300`                                              | Preflight cache, in seconds                               |

The configuration is checked at startup and the server refuses to boot on these, rather than leaving you to debug a
silent browser rejection:

- `enabled` with no origins listed.
- An origin without a scheme (`app.example.com` instead of `https://app.example.com`).
- `allowed_origins: ["*"]` together with `allow_credentials: true` - browsers reject that combination anyway, and
  configuring it means somebody expected cookies to work.

{{< callout type="warning" title="allow_credentials is the dangerous switch" >}}
It turns every listed origin into somewhere an authenticated request can be made from. List only origins you control.
{{< /callout >}}

## TLS

Two separate questions, answered in two separate places, and keeping them apart is the point.

**Whether** a listener terminates TLS is configuration. It is a port-level decision — the common deployment puts a
reverse proxy in front that terminates TLS and speaks HTTP upstream — and it needs a restart either way.

| Variable                          | Default | Description                         |
|-----------------------------------|---------|-------------------------------------|
| `MAILYARD_SERVER_TLS_ENABLED`     | `false` | HTTPS on the console and API        |
| `MAILYARD_SUBMISSION_TLS_ENABLED` | `true`  | STARTTLS on the submission listener |
| `MAILYARD_INBOUND_TLS_ENABLED`    | `true`  | STARTTLS on the MX listener         |

The HTTP default differs from the two SMTP ones deliberately. On 25 and 587 a client prefers encryption and almost none
verify the certificate, so even a self-signed pair is a real improvement over cleartext. In front of the console there
is usually a proxy, and terminating here would make it talk HTTPS upstream to a certificate it has no reason to trust.
Relay nodes are the sharper half of that: a node verifies this certificate with its system trust store, so turning
`SERVER_TLS_ENABLED` on with a self-signed pair stops every node enrolling until you install the root on each machine.

**Which** certificate is served is not configured in this file. It is assigned in the console under **Administration →
Certificates**, stored in the database, and resolved per handshake — so a replacement takes effect with no restart and
reaches every node. Each listener walks one chain:

1. the certificate assigned to it, if any
2. the ACME certificate, if ACME is configured for that hostname
3. a self-signed pair, generated on first use and shared by every node

A listener always has something to present, and an unreachable console is recoverable with
`mailyard tls unassign --listener server`, which writes to the database with no server running.

### ACME

One block for the whole installation, covering every listener above.

| Variable                       | Default | Description                                                                |
|--------------------------------|---------|----------------------------------------------------------------------------|
| `MAILYARD_ACME_ENABLED`        | `false` | Order certificates from Let's Encrypt                                      |
| `MAILYARD_ACME_HOSTS`          | derived | Hostnames to issue for. Defaults to the host in `server.public_url`        |
| `MAILYARD_ACME_EMAIL`          | —       | Registration contact, where the CA sends expiry warnings                   |
| `MAILYARD_ACME_CHALLENGE_ADDR` | `:80`   | HTTP-01 challenge listener, must be reachable as port 80 from the internet |

A name that is not in `hosts` falls through to the self-signed pair rather than failing the handshake. That is the
ordinary state of an MX: `public_url` names the console, so the derived list names the console, and the mail listeners
answer under a different hostname.

There is no cache directory. Issued certificates go into the database, so every node serves the same one and a restart
re-issues nothing. Renewal happens about 30 days before expiry, and Mailyard touches each configured host at startup and
hourly after that, so a listener that has seen no traffic still renews.

## Example environment

```bash
# Server
MAILYARD_SERVER_ADDR=:3000
MAILYARD_SERVER_PUBLIC_URL=https://mail.example.com
MAILYARD_SERVER_TRUSTED_PROXIES=10.0.0.0/8

# Database
MAILYARD_DATABASE_DSN=postgres://mailyard:secure-password@localhost:5432/mailyard?sslmode=require

# Secrets - generate both with `openssl rand -hex 32`
MAILYARD_AUTH_JWT_SECRET=...
MAILYARD_DATABASE_CRYPTO_ENCRYPTION_KEY=...

# Sign-in
MAILYARD_AUTH_LOCAL_ENABLED=true
MAILYARD_AUTH_LOCAL_EMAIL=admin@example.com

# HTTPS here rather than at a proxy. Which certificate is served is
# chosen in the console, not set here.
MAILYARD_SERVER_TLS_ENABLED=true
MAILYARD_ACME_ENABLED=true
MAILYARD_ACME_EMAIL=ops@example.com

# Optional SMTP listeners, both off unless enabled
MAILYARD_SUBMISSION_ENABLED=true
MAILYARD_INBOUND_ENABLED=true

# Observability
MAILYARD_LOGGING_FORMAT=json
MAILYARD_METRICS_ENABLED=true
MAILYARD_METRICS_TOKEN=...
```

On first start against an empty database the bootstrap admin password is written to stderr once. Migrations run
automatically, so nothing else is needed.

Platform mail — invitations, password resets, signup confirmations — is not in this list because it is not configured
here. Set `platform_mail_from` in **Admin → Settings** and give the shared pool a server. See
[Platform mail](#platform-mail).
