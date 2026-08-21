---
title: "Installation"
description: "Deploy Mailyard with Docker Compose or build it from source"
weight: 20
---

Mailyard is a single binary. It embeds the console frontend, this documentation, and its database migrations, so a
deployment is one container plus a PostgreSQL.

The delivery queue lives in PostgreSQL and the worker runs inside the server process, so `mailyard serve` is the whole
system. When one process is no longer enough, several run against the same database off the same binary and the same
config. See [Scaling out](/docs/getting-started/scaling).

## Docker Compose

```yaml
services:
  postgres:
    image: postgres:17-alpine
    environment:
      POSTGRES_USER: mailyard
      POSTGRES_PASSWORD: change-me
      POSTGRES_DB: mailyard
    volumes:
      - pgdata:/var/lib/postgresql/data
    healthcheck:
      test: ["CMD-SHELL", "pg_isready -U mailyard -d mailyard"]
      interval: 5s
      retries: 10
    restart: unless-stopped

  mailyard:
    image: mailyard:latest
    # --init applies pending migrations. See the note below.
    command: ["serve", "--init"]
    depends_on:
      postgres:
        condition: service_healthy
    ports:
      - "3000:3000"
      # Only if you enable the SMTP listeners.
      - "587:587"
      - "25:25"
    environment:
      MAILYARD_DATABASE_DSN: postgres://mailyard:change-me@postgres:5432/mailyard?sslmode=disable
      # Both from `openssl rand -hex 32`. The encryption key is what
      # seals stored secrets - losing it means losing them.
      MAILYARD_AUTH_JWT_SECRET: change-me
      MAILYARD_DATABASE_CRYPTO_ENCRYPTION_KEY: change-me-to-a-long-random-string-32b
      MAILYARD_AUTH_LOCAL_ENABLED: "true"
      MAILYARD_AUTH_LOCAL_EMAIL: admin@example.com
      MAILYARD_SERVER_PUBLIC_URL: https://mail.example.com
    healthcheck:
      test: ["CMD", "wget", "-qO-", "http://localhost:3000/healthz"]
      interval: 30s
      start_period: 15s
      retries: 3
    restart: unless-stopped

volumes:
  pgdata:
```

```bash
docker compose up -d
```

{{< callout type="warning" title="`--init` is not the default, and without it the first boot fails" >}}
A serving node checks that the schema matches the binary and **exits** if it does not — an empty database included. So
a plain `serve` against a fresh Postgres will not start, and that is deliberate: the alternative is an upgraded binary
quietly serving from last month's schema, with the features its new columns belong to silently broken.

`--init` applies pending migrations first. On a single node, leave it in the command permanently. In a fleet, exactly
**one** node carries it and the rest start once it has finished — two processes migrating an empty database at the same
time race each other and it comes out as a duplicate key.
{{< /callout >}}

`MAILYARD_SERVER_PUBLIC_URL` is worth setting before the first send rather than after. Tracking links, hosted
unsubscribe pages and invitation mail are all absolute URLs built from it, and
[campaigns refuse to start](/docs/campaigns/sending) without it.

On the first start the bootstrap admin password is printed to stderr **once** — read it out of the container log before
doing anything else:

```bash
docker compose logs mailyard | grep -i password
```

The console is then at `http://localhost:3000/app`.

{{< callout type="tip" >}}
Lost the bootstrap password? `mailyard set-password` resets it from the command line against the same database.
{{< /callout >}}

## TLS

The configuration file answers one question about TLS: **whether** each listener terminates it. It says nothing about
which certificate — that is chosen in the console and stored in the database, so every node serves the same one and a
replacement needs no restart.

| Variable                          | Default | What it does                 |
|-----------------------------------|---------|------------------------------|
| `MAILYARD_SERVER_TLS_ENABLED`     | `false` | HTTPS on the console and API |
| `MAILYARD_SUBMISSION_TLS_ENABLED` | `true`  | STARTTLS on submission       |
| `MAILYARD_INBOUND_TLS_ENABLED`    | `true`  | STARTTLS on the MX listener  |

The HTTP default is off because the common deployment puts a reverse proxy in front that terminates TLS and speaks plain
HTTP upstream. Turn it on to serve HTTPS directly.

### Terminating TLS in Mailyard

Bind 443 and switch the listener on. There is nothing else to configure: with nothing assigned, a self-signed pair is
generated on first use and shared by every node, so the console is reachable immediately over HTTPS with a browser
warning.

```yaml
services:
    mailyard:
        ports:
            - "443:443"
            - "587:587"
            - "25:25"
        environment:
            MAILYARD_SERVER_ADDR: ":443"
            MAILYARD_SERVER_TLS_ENABLED: "true"
            MAILYARD_SERVER_PUBLIC_URL: https://mail.example.com
```

Then sign in and replace that pair under **Administration → Certificates**:
upload one you already have, generate one signed by an internal CA, or order one from Let's Encrypt.
See [Certificates](/docs/admin/certificates).

{{< callout type="warning" title="public_url must match the scheme and host you browse" >}}
It decides the `Secure` flag on the session cookie, so `https://` in the setting plus `http://` in the browser means the
login succeeds, the browser discards the cookie, and every request after it is unauthenticated — with nothing in the
response to say why.

A bare IP address has the same effect in Safari, which will not keep a cookie for a host with no registrable domain.
Give the installation a hostname.
{{< /callout >}}

{{< callout type="tip" title="Locked out by an assignment" >}}
`mailyard tls unassign --listener server` writes to the database with no server running, and drops the listener back to
the self-signed pair.
{{< /callout >}}

### Ordering from Let's Encrypt

**There is no `MAILYARD_ACME_ENABLED`.** ACME is turned on in the console, not in the config file, because none of it
binds a port any more — see
[Certificates](/docs/admin/certificates#acme). Setting the old keys is not an error you have to hunt for, but it does
nothing:

```
level=WARN msg="these settings no longer exist and are ignored ..."
  keys="[acme.enabled acme.email]"
```

The order is:

1. Turn the listener on and restart — `MAILYARD_SERVER_TLS_ENABLED: "true"` on port 443, as above. The `tls-alpn-01`
   handshake **is** the validation, so without a TLS listener there is nothing for the CA to talk to.
2. Sign in, open **Administration → Certificates → Settings**, and set
   `acme_enabled`, then `acme_hosts` — one hostname per line. Optionally
   `acme_email`, where the CA sends expiry warnings.
3. Press **Order** beside a host. It is synchronous, so a refusal comes back with the CA's own words. Nothing else needs
   a restart: the settings are read fresh on every handshake and every order.

{{< callout type="warning" title="An empty host list issues nothing, and says nothing" >}}
`acme_hosts` has no default — the name in `public_url` is **not** used. With the list empty, every name falls through to
the self-signed pair exactly as it does when ACME is off, and the only symptom is a certificate that never changes.
{{< /callout >}}

The CA has to reach this installation to validate a name, and there are two ways it can:

- **`tls-alpn-01`**, when the TLS handshake on port 443 reaches Mailyard — bound directly, or through a TCP-passthrough
  proxy, which preserves ALPN. Nothing else is needed and port 80 is never used.
- **`http-01`**, otherwise. This is the path for a proxy that *terminates* TLS, where the handshake never gets to us.
  Set `MAILYARD_ACME_CHALLENGE_ADDR` — it is **empty** by default, and `:80` is the usual value.

That last one is the one dependency with an order to it: the challenge listener is bound at startup, and only when ACME
is already on. So turn ACME on in the console first, then restart. Otherwise the boot says so and carries on without it:

```
level=WARN msg="acme.challenge_addr is set but ACME is off, so no challenge
  listener was bound - turn ACME on and restart if you need http-01"
```

A hostname that is not in the ACME list falls through to the self-signed pair rather than failing the handshake, so a
mail listener answering under a name the certificate does not cover still offers working STARTTLS.

## Ports

| Port        | Listener               | Notes                                   |
|-------------|------------------------|-----------------------------------------|
| 3000 or 443 | HTTP console and API   | 443 only if this process terminates TLS |
| 587         | SMTP submission        | Off by default                          |
| 25          | Inbound MX             | Off by default                          |
| 80          | ACME HTTP-01 challenge | Only when ACME cannot use `tls-alpn-01` |

443, 587, 25 and 80 are all privileged. Docker sets
`net.ipv4.ip_unprivileged_port_start=0` inside containers, so the published image binds them as uid 1000 with no extra
configuration. Kubernetes does not — grant
`NET_BIND_SERVICE`. Elsewhere, bind a high port and map the low one to it.

## Scaling out

Run more replicas of the same container against the same database. Every node runs the worker, and delivery is claimed
with a conditional `UPDATE`, so two nodes cannot send the same message twice.

Two things to know before you do:

- **Rate limits are per process.** The HTTP limiters are in-process fixed windows, so three replicas mean three times
  the configured ceiling. Size them accordingly, or apply the limit at a shared proxy instead.
- **Scheduled jobs run on every node.** There is no leader election. The jobs are written to be idempotent and safe to
  run concurrently.

## Build from Source

### Prerequisites

- Go 1.27+
- PostgreSQL 15+
- Node.js 18+, to build the console
- [go-task](https://taskfile.dev) 3+, which runs the build steps
  (`go install github.com/go-task/task/v3/cmd/task@latest`)

PostgreSQL 15 is a hard floor, not a preference: the schema uses
`UNIQUE NULLS NOT DISTINCT`, which 14 cannot parse, so migrations fail outright on an older server rather than
degrading.

### Steps

```bash
git clone https://github.com/yousysadmin/mailyard.git
cd mailyard

# Builds the console into web/dist, the docs into docs/dist, then the binary.
task build

# --init applies the schema. Exactly one node in a fleet carries it;
# a single-node install always does.
./bin/mailyard serve --init
```

`task build` is what produces a binary with the console and documentation inside it. A bare `go build ./...` compiles
and is fine for development, but the resulting binary serves no console and no docs, because the embedded directories
are empty.

Configuration comes from `./mailyard.yaml` (see `../../../examples/mailyard.yaml.example`) or from the environment.
See [Configuration](/docs/getting-started/configuration).

## Health Checks

```bash
# Liveness - the process is up.
curl http://localhost:3000/healthz

# Readiness - the process is up and the database answers.
curl http://localhost:3000/readyz
```

Use `/healthz` for a restart probe and `/readyz` to gate traffic. Pointing a restart probe at `/readyz` means a brief
database blip restarts every node at once, which turns a recoverable outage into a longer one.
