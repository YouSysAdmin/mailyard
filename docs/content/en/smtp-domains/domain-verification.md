---
title: "Domain Verification"
description: "Claim a domain, sign outbound mail with DKIM, and publish SPF and DMARC"
weight: 30
---

A domain has to be claimed before the platform will **send** mail as it, route inbound mail for it, or sign it with
DKIM. Claiming proves ownership with one TXT record. Three further records are optional and govern how receivers treat
your mail.

{{< callout type="info" title="Verification gates sending" >}}
Every outbound surface refuses a `From` on a domain this project has not verified — the API, templates, batches,
campaigns and the SMTP relay alike — and the check runs again at delivery, so unverifying a domain stops messages that
are already queued.

It is a per-project claim, not a global one. Domain names are unique across an install, so a domain another project
verified is refused here exactly as an unknown one is, with the same message.

A claim covers **subdomains**. Verifying `example.com` is enough to send as
`news@mail.example.com` and to receive mail addressed there. Matching is by whole labels, so `evilexample.com` is not
covered. The most specific claim wins: if another project separately verified
`mail.example.com`, that name is theirs.
{{< /callout >}}

## Register a Domain

```
POST /api/v1/domains
```

```bash
curl -X POST http://localhost:3000/api/v1/domains \
  -H "Content-Type: application/json" \
  -H "Authorization: Bearer myk_..." \
  -d '{"domain": "yourdomain.com"}'
```

Response:

The body carries the domain row and every DNS record it needs, each with whether we can currently see it.

```json
{
    "domain": {
        "id": "0f8e...",
        "domain": "yourdomain.com",
        "verification_token": "abc123def456",
        "verified": false,
        "spf_verified": false,
        "dkim_verified": false,
        "dmarc_verified": false
    },
    "dns_records": [
        {
            "kind": "ownership",
            "type": "TXT",
            "host": "yourdomain.com",
            "value": "mailyard-verification=abc123def456",
            "required": true,
            "verified": false
        },
        {
            "kind": "spf",
            "type": "TXT",
            "host": "yourdomain.com",
            "required": false,
            "verified": false
        },
        {
            "kind": "dkim",
            "type": "TXT",
            "host": "mailyard._domainkey.yourdomain.com",
            "required": false,
            "verified": false
        },
        {
            "kind": "dmarc",
            "type": "TXT",
            "host": "_dmarc.yourdomain.com",
            "required": false,
            "verified": false
        }
    ]
}
```

`verified` on the domain means ownership alone. The three `*_verified` flags are independent: a domain can be provably
yours while publishing no SPF record at all, and sending does not wait on them.

## 1. Ownership (required)

| Type | Host             | Value                                        |
|------|------------------|----------------------------------------------|
| TXT  | `yourdomain.com` | `mailyard-verification=<verification_token>` |

Nothing else works until this resolves. Inbound routing and DKIM signing both key off it.

## 2. DKIM (strongly recommended)

The signing key is generated for you the moment ownership verifies. There is nothing to upload, and the private half
never leaves the database, where it is encrypted with `database.crypto.encryption_key`. Re-read the domain after
verifying and the `dkim` record will have its value filled in:

| Type | Host                                 | Value                            |
|------|--------------------------------------|----------------------------------|
| TXT  | `mailyard._domainkey.yourdomain.com` | `v=DKIM1; k=rsa; p=<public key>` |

Outbound mail is signed as soon as ownership is verified, before you publish this. That ordering is deliberate: until
the record exists a receiver simply ignores the signature, so there is no window in which signing hurts, and no reason
to make you wait for DNS to propagate before mail starts going out correctly.

Signing is per domain and per project. A message is signed only when its From domain is verified **to the project that
sent it**, so one tenant cannot sign as another tenant's domain by naming it in `From`.

Keys are RSA-2048. Ed25519 (RFC 8463) is shorter and faster but receiver support is still patchy enough that signing
with one alone means some receivers see no valid signature at all, which is worse than not signing.

## 3. SPF (recommended)

| Type | Host             | Value                                       |
|------|------------------|---------------------------------------------|
| TXT  | `yourdomain.com` | `v=spf1 include:<sending.spf_include> ~all` |

The value depends on which hosts actually send your mail. When the operator has set
`sending.spf_include`, the console fills the record in for you. Without it the SPF row still reports whether a record
was found, but suggests no value — there is no correct one to suggest, and a placeholder is something somebody publishes
verbatim.

`sending.spf_include` is an installation-wide setting with no per-domain equivalent, so the console never asks a project
to change it. If the row has no value and you need one, it comes from whichever SMTP provider delivers your mail, or
from your administrator when the project sends through the platform's own servers.

Only the record's **presence** is checked. Whether a given message passes SPF depends on the IP it was sent from, which
is not known until the message is on the wire, so a check here claiming more than presence would be lying.

{{< callout type="tip" title="SPF alone is not enough" >}}
SPF breaks on forwarding. A mailing list that re-sends your message replaces the envelope sender, and SPF then evaluates
the list's domain rather than yours. DKIM survives that, which is why it is the one worth publishing first.

{{< /callout >}}

## 4. DMARC (recommended)

| Type | Host                    | Value                                               |
|------|-------------------------|-----------------------------------------------------|
| TXT  | `_dmarc.yourdomain.com` | `v=DMARC1; p=none; rua=mailto:dmarc@yourdomain.com` |

Start at `p=none`, read the aggregate reports for a few weeks, and tighten to
`quarantine` and then `reject` once everything that legitimately sends as your domain is passing.

## Verify

```
POST /api/v1/domains/:id/verify
```

Runs all four checks live and stores the outcome. Safe to call repeatedly: a record that disappears un-verifies again on
the next run. On the first pass where ownership succeeds, the DKIM keypair is generated.

```json
{
    "domain": {
        "domain": "yourdomain.com",
        "verified": true,
        "spf_verified": true,
        "dkim_verified": false,
        "dmarc_verified": false,
        "checked_at": "2026-08-06T12:00:00Z"
    },
    "dns_records": [
        "..."
    ]
}
```

DNS changes take time to propagate. If a record you just published is not found, wait and run it again.

## Inbound authentication

The MX listener authenticates every message it receives, independently of everything above. Those records are about mail
you **send**. This is about mail you **receive**.

Each stored message carries the verdict:

```json
"auth": {
    "spf": "pass",
    "dkim": "pass",
    "dmarc": "pass",
    "dmarc_policy": "reject",
    "aligned": true,
    "client_ip": "203.0.113.9"
}
```

`aligned` is the field that matters. A valid DKIM signature only means *somebody* signed the message. The question DMARC
answers is whether the domain in the `From` header, the one a person actually reads, is the domain that vouched for it.
A message can have `dkim: "pass"` and `aligned: false`, and that combination is exactly what a spoofing attempt looks
like.

The verdict is also written into the message as an RFC 8601
`Authentication-Results` header. Any such header supplied by the sender is replaced, since only the receiver can produce
a real one.

Messages are stored regardless of the verdict. To also refuse mail that the sender's own domain says should be refused:

```yaml
inbound:
    reject_on_dmarc_fail: true
```

That refuses a message at SMTP time only when the From domain published
`p=reject` **and** nothing it vouches for passed. Off by default, because silently dropping mail is the most damaging
thing a receiver can do and forwarded mail fails SPF as a matter of routine. Turn it on after looking at what your real
traffic scores.

## List Domains

```
GET /api/v1/domains
```

Returns the project's domains with their verification state. Use
`GET /api/v1/domains/:id` for a single domain plus its `dns_records`.

## Delete a Domain

```
DELETE /api/v1/domains/:id
```

Project admin only. Deleting a domain discards its DKIM key, so re-adding the same name generates a new one and the
published record has to be replaced.
