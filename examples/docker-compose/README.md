# Examples

## `docker-compose.yml`

A production-shaped single node: Mailyard terminating TLS on 443 with its
own SMTP listeners, and one PostgreSQL that is not published.

```bash
cp .env.example .env      # fill in the three secrets
docker compose up -d
docker compose logs mailyard | grep -i password
```

The bootstrap password is printed **once**. Lost it? `mailyard set-password`
works offline against the same database:

```bash
docker compose exec mailyard mailyard set-password --email admin@example.com
```

Then sign in at `https://<your-host>/app` and go to **Administration →
Certificates**. Until you order or upload one the listener serves a
generated self-signed pair, so the browser will warn - that is the
expected first-boot state, not a misconfiguration.

### What to change before using it

| Setting | Why |
|---------|-----|
| `MAILYARD_SERVER_PUBLIC_URL` | Must match the scheme and host you browse, or sign-in silently fails - see the comment in the file |
| `MAILYARD_AUTH_LOCAL_EMAIL` | The bootstrap admin address |
| `MAILYARD_SUBMISSION_ENABLED` / `MAILYARD_INBOUND_ENABLED` | Both on in the example. Turn off what you do not publish - each is an open port |

### Behind a reverse proxy instead

If something in front terminates TLS, drop `MAILYARD_SERVER_TLS_ENABLED`
and put the listener back on `:3000`. Two things change with it:

- Set `MAILYARD_SERVER_TRUSTED_PROXIES` to the proxy's CIDRs, or the rate
  limiter, the audit log and the access log all record the proxy's address
  as the client.
- ACME can no longer validate over `tls-alpn-01`, because the proxy
  answers the handshake. Publish port 80 and set
  `MAILYARD_ACME_CHALLENGE_ADDR=:80` so `http-01` can be answered.
