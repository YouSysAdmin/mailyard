# Mailyard

Self-hosted email delivery: send transactional mail over HTTP or SMTP, run campaigns, receive on your own domains, and
watch what happened to every message from a built-in console.

One Go binary and a PostgreSQL. The binary carries the console, the documentation and its own migrations.

## Features

- Transactional email: `POST /api/v1/emails/send` (API keys with scopes, IP allowlists) or plain SMTP submission on port
  587 using an API key as the AUTH password.
- Templates: versioned, per-language localizations, `{{ var }}`rendering, CSS inlining, preview and test sends,
  import/export.
- Campaigns: subscriber lists (static or rule-based dynamic segments), A/B variants, throttled sending, delivery at each
  recipient's local time, open/click tracking, hosted one-click unsubscribe (RFC 8058).
- Inbound: point MX at the host and receive on port 25, claim domains with a DNS TXT record, received mail is stored per
  project and emitted as webhooks.
- Deliverability: suppression lists (doubling as inbound blocklists), bounce records, automatic suppression on permanent
  SMTP rejects.
- Multi-tenant projects with roles (owner/admin/editor/viewer), usage plans with volume limits and resource caps,
  per-project usage stats.
- Auth: local login with TOTP 2FA, or OIDC/SSO. Secrets (SMTP passwords, TOTP seeds) encrypted at rest.
- Attachments inline by default, or offloaded to filesystem/S3 storage.
- Observability: Prometheus metrics at `/metrics` (opt in), structured logs, outgoing webhooks with HMAC signatures and
  delivery logs.

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
crypto:
    encryption_key: "<any long random string>"   # required, seals secrets at rest
database:
    dsn: "postgres://mailyard:secret@localhost:5432/mailyard?sslmode=require"
```

First start prints the bootstrap admin password to stderr once. The console lives at `/app`, and the documentation is
served from the same binary at `/docs` (signed-in users only). Every setting maps to an env var
(`MAILYARD_AUTH_JWT_SECRET`, `MAILYARD_DATABASE_DSN`, ...).

Docker:

```sh
task docker
docker run -p 3000:3000 -p 587:587 -p 25:25 -v mailyard-data:/data \
  -e MAILYARD_DATABASE_DSN='postgres://mailyard:secret@postgres:5432/mailyard?sslmode=disable' \
  -e MAILYARD_AUTH_JWT_SECRET=... \
  -e MAILYARD_AUTH_LOCAL_ENABLED=true \
  -e MAILYARD_AUTH_LOCAL_EMAIL=admin@example.com \
  mailyard:devel
```

## Optional listeners and services

| Config               | Default | Purpose                                              |
|----------------------|---------|------------------------------------------------------|
| `submission.enabled` | off     | SMTP submission on :587, AUTH with an API key        |
| `inbound.enabled`    | off     | MX listener on :25 for verified domains              |
| `storage.backend`    | inline  | `fs` or `s3` attachment storage                      |
| `metrics.enabled`    | off     | Prometheus scrape endpoint, `metrics.token` gates it |

## Development

```sh
task check        # go vet + tests + gofmt + SPA type-check + prettier
cd web && npm run dev   # vite dev server proxying /api to :3000
cd web && npm run format  # prettier over src
task docs-dev     # the embedded documentation, live, on :1313/docs/
```

Building the documentation into the binary needs [Hugo](https://gohugo.io)


## License

Mailyard is source-available under the [Business Source License 1.1](LICENSE).
You may run it in production for your own organization and products - what
the license withholds is offering Mailyard itself as a hosted email service
to third parties. Each version converts to the Apache License 2.0 four years
after its release.

The client libraries under [`sdk/`](sdk) are MIT licensed, so they can be
embedded in anything.
