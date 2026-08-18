---
title: "Certificates"
description: "TLS certificates for the HTTP and SMTP listeners"
weight: 90
---

Every certificate this installation holds lives in the database, not in a directory on one node. A private key in a file
belongs to the machine that wrote it: no other node can use it, and it does not survive that machine.

That matters as soon as there is more than one node:

- **ACME** with a per-node cache means each node orders its own certificate. Let's Encrypt allows five duplicates per
  week for one name set, so the sixth node serves no TLS, and the ones that succeeded renew independently forever.
- **Self-signed** with a per-node cache means each node generates a different pair, so a client that reaches two nodes
  sees two certificates under one hostname - which is exactly what pinning a self-signed fingerprint is meant to detect.

Both are one shared row now. Private halves are encrypted with `database.crypto.encryption_key`; the public certificate
is stored in the clear so the console can show an expiry without the key being involved.

## What a listener serves

The configuration file answers one question about TLS: **whether** a listener terminates it, with `server.tls.enabled`,
`submission.tls.enabled` and
`inbound.tls.enabled`. It says nothing about which certificate, because that would be a second place to say what this
page already says.

Each listener that does terminate TLS walks the same chain, resolved per handshake:

1. **The certificate assigned to it** here, if any.
2. **ACME**, if `acme.enabled` is set and the name being asked for is in
   `acme.hosts`.
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

{{< callout type="note" title="Keys that were removed" >}}
`tls.mode`, `tls.cert`, `tls.key`, `tls.fqdn`, `tls.alg`, `tls.cachedir` and the per-listener `tls.acme.*` blocks are
gone. A mode duplicated an assignment with nothing reconciling the two, and they disagreed in both directions:
`mode: none`
made an assignment silently inert, and an assignment overrode a mode written in a file. The keys still parse, so any you
have set are named in the boot log rather than ignored in silence.
{{< /callout >}}

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

It used to be a yaml block, one per listener, which in every real configuration was three identical copies. What put it
in a file was the challenge port - and there is no port any more, see below.

There is no `MAILYARD_ACME_ENABLED`. The old config keys are ignored, and the boot log names the ones you set:
`these settings no longer exist and are ignored`.

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
Set `acme.challenge_addr` (empty by default, usually
`:80`) in the config file and make that port reachable. This is the one thing about ACME still in yaml, because it binds
a port.

It is also the one step here with an order to it. That listener is bound at startup and only when ACME is already on, so
turn `acme_enabled` on first and restart after. A restart with the address set and ACME still off says so and carries on
without it:

```
acme.challenge_addr is set but ACME is off, so no challenge listener was bound
 - turn ACME on and restart if you need http-01
```

`GET /api/v1/admin/certificates/acme` reports which case you are in as
`tls_terminated_here`, and the console warns before you press Order when neither route is open.

Both challenge types put their token in the shared cache, so validation works on more than one node: the CA can be
answered by whichever node it is routed to, not only the one that ordered.

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
