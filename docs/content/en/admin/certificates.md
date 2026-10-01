---
title: "Certificates"
description: "TLS certificates for the HTTP and SMTP listeners"
weight: 90
---

Every certificate this installation holds lives in the database, not in a directory on one node. A private key in a file
belongs to the machine that wrote it: no other node can use it, and it does not survive that machine.

That matters as soon as there is more than one node: every node would order its own ACME certificate against Let's
Encrypt's limit of five duplicates a week for one name set, and every node would generate a different self-signed pair,
so a client reaching two nodes would see two certificates under one hostname.

So both are one shared row. Private halves are encrypted with `database.crypto.encryption_key` and the public
certificate is stored in the clear, so the console can show an expiry without the key being involved.

## What a listener serves

The configuration file answers one question about TLS: **whether** a listener terminates it, with `server.tls.enabled`,
`submission.tls.enabled` and
`inbound.tls.enabled`. It says nothing about which certificate, because that would be a second place to say what this
page already says.

Each listener that does terminate TLS walks the same chain, resolved per handshake:

1. **The certificate assigned to it** here, if any.
2. **ACME**, if `acme_enabled` is set and the name being asked for is in
   `acme_hosts`.
3. **The self-signed pair**, generated on first use and shared by every node.

So a listener always has something to present, an assignment takes effect within 30 seconds with no restart, and a name
outside the ACME list gets working opportunistic TLS instead of a failed handshake.

{{< callout type="note" title="Assigned to a listener with TLS off" >}}
A listener with `tls.enabled: false` does no handshake, so an assignment there is recorded and nothing presents it. The
console shows it as **TLS off** rather than as in use, and such an assignment does not block deleting the certificate -
it is cleared along with it.
{{< /callout >}}

### Recovering a console you cannot reach

Assigning a certificate that no browser will accept locks you out of the only place assignments are made. `mailyard tls`
is the way back: it reads and writes the same rows offline, against the database the config names, whether or not the
server is up.

```bash
mailyard tls status
mailyard tls unassign --listener server
mailyard tls assign --listener server --certificate edge
```

`unassign` drops the listener to the rest of the chain, so it keeps serving TLS.

A running node notices within 5 minutes, on its settings refresh. Restart it if you need the change now.

## Self-signed

The pair is generated once and shared by every node. It covers the host from `server.public_url`, a wildcard under it,
and localhost:

```
https://mail.example.com
  -> DNS:mail.example.com, DNS:*.mail.example.com, DNS:localhost
```

Both the name and the wildcard, because `*.mail.example.com` does **not** match `mail.example.com` - a certificate
carrying only the wildcard fails on the very name you configured.

It is the last step of the chain, so it is what a listener presents when nothing is assigned and ACME is off or does not
cover the name. Nothing has to be configured for it to exist.

## Your own certificate authority

The problem with a self-signed certificate per listener is not the certificate, it is the arithmetic: three listeners
means three fingerprints to install somewhere and three to replace when they expire.

An authority collapses that. Generate one, install **its** certificate wherever mail clients, browsers and scripts have
to trust you, and sign each listener's certificate with it. Replacing a listener certificate then needs nothing done to
any client.

```bash
curl -X POST http://localhost:3000/api/v1/admin/certificates/generate-ca \
  -H "Authorization: Bearer mya_..." \
  -H "Content-Type: application/json" \
  -d '{
    "name": "internal-ca",
    "validity_days": 3650,
    "subject": {
      "common_name": "Acme Internal CA",
      "organization": "Acme Ltd",
      "unit": "Infrastructure",
      "country": "UA", "state": "Kyiv", "locality": "Kyiv"
    }
  }'
```

Then sign a listener certificate with it by naming it as the `issuer`:

```bash
curl -X POST http://localhost:3000/api/v1/admin/certificates/generate \
  -H "Authorization: Bearer mya_..." \
  -H "Content-Type: application/json" \
  -d '{
    "name": "edge",
    "hosts": ["mail.internal", "10.0.0.7"],
    "issuer": "internal-ca",
    "algorithm": "ecdsa"
  }'
```

Assign `edge` to a listener as below, and anything holding the authority verifies it:

```
openssl verify -CAfile internal-ca.pem edge.pem   ->  edge.pem: OK
```

### Getting the authority out

```
GET /api/v1/admin/certificates/{name}/pem
```

returns the certificate, PEM encoded, with no private key - the console's **Download** button is this call. That file is
what goes into a trust store.

The private half never leaves. There is no route that returns it, for any certificate.

### Rules worth knowing before you rely on it

- **A listener cannot be assigned an authority.** An authority carries no host names and no `serverAuth`, so a listener
  serving one would refuse *every*
  client - which is strictly worse than serving nothing, since an unassigned listener works. Assigning one is refused,
  uploading one over the name a listener is already serving is refused, and if the row is edited in the database anyway
  the listener logs a warning and keeps serving its configured certificate.
- **Listener certificates are capped at 398 days.** Chrome and Apple refuse any server certificate with a longer
  lifetime, *including* one signed by a root you installed yourself. An authority has no such limit - nothing serves it
  in a handshake - and defaults to ten years.
- **A certificate is never issued to outlive its issuer.** Ask for longer and it is shortened to match. Left unchecked
  it would mint happily and then stop verifying on a date nothing warns about, with an error naming the leaf - the one
  certificate that is still fine.
- **No intermediates.** An authority is marked `pathlen:0`, so it signs certificates and nothing else. The stored
  certificate is the leaf **alone**, never bundled with the root: a self-signed root is a trust anchor, and a client
  that does not have it does not come to trust it because it arrived in a handshake.
- **rsa or ecdsa, not ed25519.** An Ed25519 root is perfectly valid and several operating system trust stores will not
  install one, which defeats the point.
- **Names are not reused.** `generate-ca` answers 409 rather than replacing an existing name. Replacing an authority
  invalidates every certificate it signed, and nothing would notice: the row still parses and its expiry is still in the
  future.

## Managed certificates

A certificate you upload or generate, under a name you choose.

```bash
curl -X POST http://localhost:3000/api/v1/admin/certificates \
  -H "Authorization: Bearer mya_..." \
  -H "Content-Type: application/json" \
  -d '{
    "name": "production",
    "certificate": "-----BEGIN CERTIFICATE-----\n...",
    "private_key": "-----BEGIN PRIVATE KEY-----\n..."
  }'
```

The certificate may carry a chain, leaf first. The key is checked against it before anything is stored - a mismatch
would bring the listener up and then fail every handshake, with nothing in the upload to say why.

To generate one instead, for an internal listener or a test instance:

```bash
curl -X POST http://localhost:3000/api/v1/admin/certificates/generate \
  -H "Authorization: Bearer mya_..." \
  -H "Content-Type: application/json" \
  -d '{ "name": "internal", "hosts": ["mail.internal"], "algorithm": "ecdsa" }'
```

Every subject field is optional and the common name defaults to the first host.
`hosts` is not optional: Go and every browser stopped matching hostnames against the common name years ago, so a
certificate with no subject alt name matches nothing anywhere.

Omit `issuer` and it is self-signed, which is the whole of what this endpoint did before authorities existed.

## Assigning one to a listener

Three [platform settings](/docs/admin/platform-settings) name which certificate each listener serves:

| Setting                      | Listener                      |
|------------------------------|-------------------------------|
| `tls_certificate_server`     | HTTP - console, API, tracking |
| `tls_certificate_submission` | SMTP submission               |
| `tls_certificate_inbound`    | Inbound MX                    |

```bash
curl -X PUT http://localhost:3000/api/v1/admin/settings \
  -H "Authorization: Bearer mya_..." \
  -H "Content-Type: application/json" \
  -d '{"settings":[{"key":"tls_certificate_server","value":"production"}]}'
```

**No restart.** The certificate is resolved per handshake through a 30-second cache, and the node handling the request
refreshes its settings immediately - so a replacement takes effect while you are still looking at the page.

Other nodes take up to 5 minutes: they learn the new assignment on their settings refresh, not from the write. The same
is true of a change made with `mailyard tls`, which no node is told about at all. Restart a node to apply one
immediately.

An empty setting means the listener falls through to the rest of the chain - ACME, then the self-signed pair. That is
the fallback, not a failure, and it is also what happens if the assigned certificate is missing, unreadable, or
an [authority](#your-own-certificate-authority), with a warning in the log rather than a listener that will not serve.

{{< callout type="info" title="An assignment can never take a listener down" >}}
Deliberately, and it is the reason the chain has a last step that needs no configuration. The console is reached through
one of these listeners, so an assignment that fails to load must degrade rather than stop serving - otherwise the only
tool that could undo it is behind the thing it broke. `mailyard tls unassign` is the other half of that.
{{< /callout >}}

A certificate a listener is **serving** cannot be deleted. Assign that listener something else first. A dormant
assignment - a listener with `tls.enabled: false` - does not block the delete, and is cleared with it.

## ACME

Certificates ordered from a CA and cached in the same table, shared by every node. Everything about it is a **platform
setting**, not configuration, so it is turned on and changed in the console with no restart:

| Setting              | Description                                                                       |
|----------------------|-----------------------------------------------------------------------------------|
| `acme_enabled`       | Order certificates at all                                                         |
| `acme_hosts`         | Hostnames to issue for, one per line in the console and a JSON array over the API |
| `acme_email`         | Account contact, where the CA sends expiry warnings                               |
| `acme_directory_url` | A different directory. Empty is Let's Encrypt production                          |

**Administration → Certificates** has all of it: a **Settings** button for those four values, a host list you add to and
remove from, and **Order** beside each host.

{{< callout type="warning" title="`acme_enabled` on its own does nothing" >}}
`acme_hosts` has no default and is not derived from `server.public_url`. While the list is empty, every name falls
through to the self-signed pair exactly as it does with ACME off - no error, no log line, just a certificate that never
changes. Name a host, then press Order.
{{< /callout >}}

No wildcard is ever requested, and that is not an omission. The challenges this speaks cannot issue one - Let's Encrypt
requires DNS-01 for a wildcard - so asking would fail the order and leave the listener with nothing.

### How the CA reaches you

Two ways, and which one applies decides whether you need a second port at all.

**`tls-alpn-01`** — the CA opens a TLS connection to port 443 with the `acme-tls/1`
protocol, and Mailyard answers the handshake itself. This needs **nothing**: no port 80, no challenge listener, no
firewall rule beyond the one already letting clients in. It works when the handshake reaches this process - bound
directly, or through a TCP-passthrough proxy.

**`http-01`** — for a proxy that *terminates* TLS. It answers the handshake itself, so ALPN validation never arrives.
Set `acme.challenge_addr` (empty by default) in the config file. This is the one thing about ACME in yaml, because it
binds a port - at startup, whether or not ACME is on, so turning ACME on later needs no restart. `:80` when this
process is what answers port 80, any other address when a proxy forwards the challenge path to it, see below.

`GET /api/v1/admin/certificates/acme` reports which case you are in as
`tls_terminated_here`, and the console warns before you press Order when neither route is open.

Both challenge types put their token in the shared cache, so validation works on more than one node: the CA can be
answered by whichever node it is routed to, not only the one that ordered.

### Behind a proxy that terminates TLS

The common deployment: Caddy, nginx or Traefik holds ports 80 and 443 with a certificate of its own for the console, and
Mailyard's SMTP ports are published straight from the container. The CA can never reach Mailyard's handshake, so
`tls-alpn-01` is out, and STARTTLS on 587 and 25 would be left with the self-signed pair. `http-01` through the proxy is
the answer, and it takes one setting and one proxy route.

1. Bind the responder on a port the proxy can reach and nothing else needs to. It is not published:

   ```yaml
   environment:
     MAILYARD_ACME_CHALLENGE_ADDR: ":8080"
   ```

2. In the proxy, forward the challenge path for the mail hostname to it, ahead of the route that carries the console. A
   Caddyfile site block:

   ```
   mail.example.com {
       handle /.well-known/acme-challenge/* {
           reverse_proxy mailyard:8080
       }

       reverse_proxy mailyard:3000
   }
   ```

   The same site as [caddy-docker-proxy](https://github.com/lucaslorentz/caddy-docker-proxy) labels on the Mailyard
   service, where the numeric prefix orders the two routes:

   ```yaml
   labels:
     caddy: mail.example.com
     caddy.0_handle: /.well-known/acme-challenge/*
     caddy.0_handle.reverse_proxy: "{{upstreams 8080}}"
     caddy.1_reverse_proxy: "{{upstreams 3000}}"
   ```

3. Sign in over the proxy, turn `acme_enabled` on, add the hostname to `acme_hosts` and press **Order**. Nothing
   restarts: the port was bound at boot and asks the settings on every request.

A complete compose file with the Caddyfile is in `examples/docker-compose/caddy` in the repository.

Two things make this work, and both are worth knowing when the proxy is not Caddy:

- **The proxy's own challenges come first.** Caddy answers a challenge it is solving itself and hands every other one
  down the route, so Caddy and Mailyard each hold a certificate for the same name and neither knows about the other.
- **The `Host` header is passed through unchanged.** Mailyard refuses a token request for a name outside `acme_hosts`,
  and it reads that name from the request. Caddy's `reverse_proxy` keeps it by default, nginx needs
  `proxy_set_header Host $host`.

The CA follows a redirect from port 80 to 443, so a proxy that redirects plain HTTP to HTTPS needs no separate
plain-HTTP
site: the request arrives over the proxy's own certificate and the route above still applies. A mail hostname that
differs from the console's needs a site block of its own in the proxy, with the same challenge route.

`server.tls.enabled` stays off in this shape. The chain is walked by every listener that terminates TLS, so the
certificate ordered this way lands on the STARTTLS listeners, which is where it was missing.

An order behind a proxy takes a little longer than one that is not, and the reason is worth knowing when reading the
CA's account page: the ACME client tries `tls-alpn-01` first, that attempt reaches the proxy and fails, and only then
does it open a fresh order over `http-01`. Let's Encrypt counts the first attempt against its failed-validation limit,
five per hostname per hour, which a renewal every two months never reaches - a session of pressing **Order** to debug
something else might, so use the staging directory for that.

### Ordering

```bash
curl -X POST http://localhost:3000/api/v1/admin/certificates/acme/order \
  -H "Authorization: Bearer mya_..." \
  -H "Content-Type: application/json" \
  -d '{ "host": "mail.example.com" }'
```

Synchronous, including the challenge, so the answer tells you whether it worked. Over
`tls-alpn-01` that is seconds. A refusal carries the CA's own words - `DNS problem:
NXDOMAIN looking up A for mail.example.com` says what to fix in a way "could not issue"
does not.

`POST .../acme/renew` is the same thing with the cached entry cleared first. There is no renew-now in the ACME client:
its renewal timer only runs on a handshake, so dropping the cached entry is what turns the next ask into a real order.

{{< callout type="tip" title="Use the staging directory while working it out" >}}
Production allows five duplicate certificates per week for one name set, and a session spent finding out why validation
fails will spend that. Set `acme_directory_url` to
`https://acme-staging-v02.api.letsencrypt.org/directory`, get it working, then clear it. Staging issues an untrusted
certificate on purpose - the console says so while it is set.
{{< /callout >}}

## Expiry

A certificate is the one piece of configuration that breaks by doing nothing, and a listener holding an expired one
starts perfectly - only the handshake fails.

So a sweep runs every six hours on the worker node, over everything in the table:

- more than a week left: a warning in the log
- inside a week, or already expired: an error
- one mail a day to the platform admins while anything is inside the thirty-day window,
  if [platform mail](/docs/admin/system-mail) is configured

Thirty days is chosen to sit outside autocert's own renewal point, so an ACME certificate appearing in that window means
renewal is **failing**, not pending.

## What the installation holds for itself

```
GET /api/v1/admin/certificates/system
```

lists the ACME cache, the self-signed pair and the relay authority. Read-only: these are maintained by the code that
needs them, and deleting the relay authority would take every relay node offline.
