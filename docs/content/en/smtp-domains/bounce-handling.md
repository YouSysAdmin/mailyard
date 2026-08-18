---
title: "Bounce Handling"
description: "How asynchronous bounces find their way back to the message that caused them"
weight: 70
---

A `sent` status means the next hop accepted the message - not that it reached an inbox. Receivers frequently accept
first and reject later, and that rejection comes back as a delivery status notification (RFC 3464) or, from a provider,
as a notification over its own API.

Mailyard reads either, records the bounce, and suppresses the address so nothing is sent to it again.

## Which message a report is about

Every outbound message carries a header naming the send it belongs to:

```
X-Mailyard-Email-Id: 61c47010-fbac-4d90-924a-ecd3cdfa49e0
```

A report returns the original headers, so that id comes back with it. Wherever the report lands, Mailyard resolves it to
the exact message and from there to the project that sent it.

That indirection matters because the two obvious alternatives both fail against a real provider:

- **The envelope.** Encoding the id in the return path only works while `MAIL FROM`
  is yours. Amazon SES and every comparable provider replace it.
- **The `Message-ID`.** SES rewrites that too - the same behaviour that makes
  `skip_dkim` necessary.

An unknown `X-` header is the one thing they leave alone.

**This is the same in all three cases below.** Only the route the report travels changes.

## Where a report goes

Here the three delivery shapes genuinely differ, and there is no single setting that covers them. The rule underneath is
not policy but arithmetic: **a receiver evaluates SPF for the return path's domain against the IP that connected**, so a
return path is only usable on a domain that authorizes that IP and points its MX at Mailyard. Both belong to whoever
owns the sending server.

| You send through                               | Return path                     | Reports arrive                           |
|------------------------------------------------|---------------------------------|------------------------------------------|
| Your own relay (postfix, anything plain)       | a domain of yours               | as DSN, to whatever MX that domain names |
| The shared platform pool                       | the operator's domain           | as DSN, to whatever MX that domain names |
| A [relay node](/docs/smtp-domains/relay-nodes) | the operator's domain           | reported by the node itself              |
| Amazon SES                                     | SES replaces it, nothing to set | over SNS                                 |

A relay node is the one case where the report does not have to travel at all: the node made the connection, so it saw
the answer. It reports the outcome directly, and every node's address has to be authorized in the SPF record of
whichever domain the return path is on.

That covers only what happened **during** the connection. A receiver that accepts and rejects afterwards sends a DSN
hours later, and that one arrives as ordinary mail at whatever the return path domain's MX points at - so it is the row
above that applies, not the node's own reporting.

{{< callout type="danger" title="Do not point a platform bounce domain at a tenant's relay" >}}
It is tempting to give every message one installation-wide return path. It fails: the receiver checks SPF for that
domain against the tenant's sending IP, the platform's record does not authorize it and never should, and the resulting
failure is recorded against the platform's reputation for mail it did not send the IP for. Not merely unaligned -
**failed**.
{{< /callout >}}

## Your own relay

Two DNS records on a subdomain of a domain the project has already verified. The subdomain, never the apex: the apex is
where your real mail arrives and where your existing MX points.

```
bounce.user.com   MX   -> mail.example.com     (the Mailyard inbound listener)
bounce.user.com   TXT  -> v=spf1 ip4:<your relay's IP> -all
```

Then set **Project settings -> Bounce Address** to `bounces@bounce.user.com`. It must be on a domain verified to the
project, and a subdomain of a verified one counts, so verifying `user.com` is enough.

DMARC is happy with this: `bounce.user.com` against a `From` of `user.com` aligns under the default relaxed policy, and
SPF passes because the record is yours. That is two passing legs rather than one.

The [inbound listener](/docs/inbound/overview) has to be on (`inbound.enabled`, default port `:25` because an MX record
has nowhere to put a port number - see
[Privileged ports](/docs/security/smtp-submission#privileged-ports)).

### When the reports cannot reach Mailyard

Point the MX at a [relay node](/docs/smtp-domains/relay-nodes) instead of at Mailyard. Relay nodes are part of the
enterprise edition.

```
bounce.user.com   MX   -> node1.example.com    (a relay node running an MX)
```

This is for a network Mailyard is not reachable from. Mail is sent into a region behind a national firewall, the bounces
come back to your bounce domain, and they never arrive - because the connection they need is the one that does not work.
Nothing is wrong with the sending and nothing says so: bounces simply stop.

A node inside that region takes them on port 25 and forwards them to Mailyard over its own outbound HTTP, through
whatever proxy or tunnel it has. It writes each message down before answering `250` and retries until the link comes
back, so an outage delays reports rather than losing them.

With every bounce domain pointed at nodes, Mailyard itself needs no port 25 at all.

## The shared platform pool

The sending IPs are the platform's, so the return path is too:

```yaml
sending:
    bounce_address: bounces@mail.example.com
```

Installation-wide, used only for mail leaving through `shared_smtp_servers`. Mailyard warns at startup if that address
is on a domain no project has verified, or if the inbound listener is off - in either case the reports would arrive
nowhere and nothing else would say so.

## Amazon SES

SES takes the bounce itself and reports over SNS. See
[SES Notifications](/docs/smtp-domains/ses-notifications) for the setup. A project bounce address has no effect on this
path and does not need clearing.

## What is recorded

- **Hard bounce** - a DSN `failed` recipient with `5.x.x`, or an SES `Permanent`
  bounce. Recorded under *Bounces*, and the address is suppressed.
- **Soft** - `4.x.x`, `delayed`, or an SES `Transient` bounce. Recorded, not suppressed. A full mailbox is not a dead
  address.
- **Complaint** - an ARF report (RFC 5965) or an SES complaint notification. Recorded and suppressed.

### Forgery protection

The MX port is open to the whole internet, and anyone with an AWS account can point an SNS topic at the webhook. Both
channels are equally untrusted and both go through the same two rules:

1. **The report must name a real send id.** It is a uuid, so it cannot be guessed, and it is what decides the project.
2. **A reported recipient must be one that message actually went to.** Holding a valid id is not enough to suppress an
   arbitrary address.

A report with no id is logged as unattributed and nothing is written. Nothing is guessed from the reported address
alone - that would let anyone who can reach either channel suppress an address just by describing it.

A relay node forwarding what its own MX received is a third channel and gets the same two rules. A node a **tenant**
enrolled gets one more: it may only attribute to its own project's mail. The unscoped rule is right where the report
lands on the operator's domain and the sending project is somebody else's - it is wrong for a machine on a tenant's own
network, where filing a bounce against a neighbour would need only an id and one address that message really went to.

## Nothing configured

`MAIL FROM` stays the From address. SPF aligns for free, and reports go wherever that domain's mail goes. If Mailyard
receives mail for it, they arrive and are processed with no further setup - otherwise a forwarding rule on that mailbox
works just as well, because the header does the attribution either way.
