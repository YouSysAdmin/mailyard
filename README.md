# Mailyard

Self-hosted email delivery: send transactional mail over HTTP or SMTP, run campaigns, receive on your own domains, and
watch what happened to every message from a built-in console.

One Go binary and a PostgreSQL. The binary carries the console, the documentation and its own migrations.

## Features

- **Amazon SES, natively.** SES is a provider on an SMTP server row: sending goes over the SES API, and bounce and
  complaint notifications come back over SNS into the same bounce handling as everything else.
- **Sending.** `POST /api/v1/emails/send`, or plain SMTP submission on :587 with an API key as the AUTH password.
  Single, template, batch and scheduled sends, attachments (inline, or offloaded to filesystem/S3), a delivery log
  per message, and a sandbox that captures mail instead of delivering it.
- **Templates.** Versioned, per-language localizations, `{{ var }}` rendering, CSS inlining, preview and test sends,
  import/export.
- **Campaigns.** Subscriber lists (static or rule-based dynamic segments), A/B variants, throttling, delivery at each
  recipient's local time, open/click tracking, hosted one-click unsubscribe (RFC 8058).
- **Your own delivery.** Per-project SMTP servers in named groups with failover, a platform-wide shared pool, DKIM signing for verified domains, approved
  sender addresses, bounce records feeding the suppression list. Address verification (syntax, disposable, role, MX).
- **Inbound.** Point MX at the host and receive on :25, claim domains with a DNS TXT record, SPF/DKIM/DMARC checked
  at ingest, received mail stored per project and emitted as webhooks.
- **Multi-tenant.** Projects with their own roles over a permission catalogue, members and invitations, usage plans
  with volume limits and resource caps, per-project usage and analytics, export and erasure per address or in bulk.
- **Auth.** Local sign-in with passkeys and TOTP, or OIDC/SSO. Sessions are tracked and revocable. Secrets (SMTP
  passwords, TOTP seeds, private keys) sealed at rest.
- **Operations.** TLS for every listener from one certificate chain (assigned, ACME, or self-signed), Prometheus
  metrics, structured logs, an audit log, alert mail, retention windows, maintenance mode, PostgreSQL read replicas.
- **Integration.** Outgoing webhooks with HMAC signatures and delivery logs. Both API surfaces are described in
  OpenAPI (`mailyard export-api-spec`), and three clients are generated from it: [Go, Python and Ruby](sdk).

Documentation: [yousysadmin.github.io/mailyard](https://yousysadmin.github.io/mailyard/), or `/docs` on any running
instance.

## Quick start

Tasks are run with [go-task](https://taskfile.dev) - `go install
github.com/go-task/task/v3/cmd/task@latest`, or your package manager.
`task` on its own lists everything with a description.

```sh
task dev-up         # Postgres in Docker (compose.yaml)
task run            # the server against it, no config file needed
```

`task run` supplies everything through `MAILYARD_*` env vars, so there is nothing to configure to get going. The first
start prints the bootstrap admin password to stderr once - sign in at http://localhost:3000/app.

Everything the dev stack writes lives under `dev-data/`, which is gitignored: `dev-data/pg` for the database files and
`dev-data/blobs` for attachment storage. `task dev-down` stops Postgres and keeps both. To start clean, stop it and
delete the directory.

```sh
task test           # store tests SKIP without a database
task test-db        # runs them against the mailyard_test database
task build          # SPA + docs + binary (bin/mailyard)
```

For a real deployment, use a config file instead. See
`examples/mailyard.yaml.example` for every setting with defaults. Minimal config:

```yaml
server:
    addr: ":3000"
    public_url: "https://mail.example.com"   # enables tracking links
auth:
    jwt_secret: "<openssl rand -hex 32>"
    local:
        enabled: true
        email: admin@example.com               # bootstrap user
database:
    dsn: "postgres://mailyard:secret@localhost:5432/mailyard?sslmode=require"
    crypto:
        encryption_key: "<any long random string>"   # required, min 32 chars, seals secrets at rest
```

First start prints the bootstrap admin password to stderr once. The console lives at `/app`, and the documentation is
served from the same binary at `/docs` (signed-in users only). Every setting maps to an env var
(`MAILYARD_AUTH_JWT_SECRET`, `MAILYARD_DATABASE_CRYPTO_ENCRYPTION_KEY`, ...). A serving node refuses to start on a
schema older than itself - run one node with `--init` to migrate, the rest follow.

Docker:

```sh
task docker
docker run -p 3000:3000 -p 587:587 -p 25:25 -v mailyard-data:/data \
  -e MAILYARD_DATABASE_DSN='postgres://mailyard:secret@postgres:5432/mailyard?sslmode=disable' \
  -e MAILYARD_AUTH_JWT_SECRET=... \
  -e MAILYARD_AUTH_LOCAL_ENABLED=true \
  -e MAILYARD_AUTH_LOCAL_EMAIL=admin@example.com \
  -e MAILYARD_DATABASE_CRYPTO_ENCRYPTION_KEY=... \
  mailyard:<version>            # task docker tags the image with git describe
```

## Optional listeners and services

| Config               | Default | Purpose                                              |
|----------------------|---------|------------------------------------------------------|
| `submission.enabled` | off     | SMTP submission on :587, AUTH with an API key        |
| `inbound.enabled`    | off     | MX listener on :25 for verified domains              |
| `server.tls.enabled` | off     | TLS on the HTTP listener (the SMTP ones default on)  |
| `storage.backend`    | inline  | `fs` or `s3` attachment storage                      |
| `metrics.enabled`    | off     | Prometheus scrape endpoint, `metrics.token` gates it |
| `database.replica_dsns` | none | Read replicas for the list and analytics queries    |

## Development

```sh
task check        # vet + golangci-lint + tests + gofmt + SPA type-check + prettier
task test-db      # store tests against the compose database
cd web && npm run dev   # vite dev server proxying /api to :3000
task docs-dev     # the embedded documentation, live, on :1313/docs/
task pages-dev    # the public site (landing plus docs), live, on :1314/
task sdk          # regenerate and check the three clients
```

Building the documentation into the binary needs [Hugo](https://gohugo.io). The public site is the same Hugo
source built in the `pages` environment by `.github/workflows/docs.yaml`.


## License

Mailyard is source-available under the [Business Source License 1.1](LICENSE).
You may run it in production for your own organization and products - what
the license withholds is offering Mailyard itself as a hosted email service
to third parties. Each version converts to the Apache License 2.0 four years
after its release.

The client libraries under [`sdk/`](sdk) are MIT licensed, so they can be
embedded in anything.
