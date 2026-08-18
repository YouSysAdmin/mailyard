---
title: "Amazon SES"
description: "Send through AWS SES SMTP and understand what SES rewrites on the way out"
weight: 60
---

Two ways, and the choice is about credentials rather than about deliverability.

- **Over the API** (`provider: ses`). No stored secret at all when Mailyard runs on AWS - the instance role signs the
  calls. Nothing outbound on 587. This is the one to pick on EC2, ECS or EKS.
- **Over SMTP.** A dial like any other provider, with SES SMTP credentials. Pick this when Mailyard runs outside AWS, or
  when the SES account belongs to somebody who will only hand you credentials.

Either way SES is **not a passive relay**: it rewrites parts of every message, and skipping
the [rewrites section](#what-ses-rewrites-and-why-it-matters)
means meeting those rewrites as confusing authentication results rather than as obvious errors.

## Over the API, with no stored secret

The case this exists for: Mailyard on an EC2 instance whose IAM role grants `ses:SendEmail`. Over SMTP that deployment
has to mint SES SMTP credentials and store a long-lived secret to reach a service the machine can already call.

Grant the role `ses:SendEmail` and `ses:GetAccount` - the second is what the **Test** button uses, and it proves the
credentials without sending anything.

*SMTP Servers* > *Add server*, choose **Amazon SES (API)**, and leave the key fields empty:

```bash
curl -X POST http://localhost:3000/api/v1/smtp-servers \
  -H "Authorization: Bearer myk_..." \
  -H "Content-Type: application/json" \
  -d '{
    "name": "SES eu-west-1",
    "provider": "ses",
    "provider_config": {
      "region": "eu-west-1",
      "configuration_set": "mailyard-events"
    }
  }'
```

- **No host, port or encryption.** The row is reached over HTTPS, and the form hides those fields rather than asking for
  values nothing reads.
- **`region` is required** and is not guessed. The wrong one answers
  "email address is not verified" about an identity that plainly is.
- **Leave `username` and `password` empty** to use the machine's own credentials - the instance role, an ECS task role,
  an EKS service account, or the environment. Set them to an access key id and secret when the SES account is not the
  one this machine belongs to, which is the ordinary case for a tenant.
- **`configuration_set` is how bounces come back.** On the SMTP path notifications are usually attached to the identity.
  On the API path a configuration set is what carries sending events to SNS, and without one the send works and the
  feedback is silent. See
  [SES notifications](/docs/smtp-domains/ses-notifications).
- **Nothing to say about DKIM.** SES rewrites `Date` and `Message-ID` and signs the result with its own key, and both
  are in Mailyard's signed header set - so a signature applied on the way to it always arrives broken. That is a fact
  about SES rather than a setting, so the choice is not offered on this provider and not read from the row: send
  `skip_dkim: false` and it is still skipped. The console says so where the checkbox used to be.

  A broken signature is ignored rather than punished (RFC 6376), which is exactly why this was worth taking away:
  getting it wrong produced no error anywhere, just mail that quietly stopped being authenticated by us.

{{< callout type="note" title="SES caps a raw message at 10 MiB" >}}
Lower than the 25 MiB of attachments an installation accepts by default, so this is reachable with nothing
misconfigured. A message over the limit is refused before the API is called, and refused **permanently** - retrying
cannot shrink it.
{{< /callout >}}

### What differs from the SMTP path

|                         | SMTP                                             | API                                                  |
|-------------------------|--------------------------------------------------|------------------------------------------------------|
| Credentials             | SES SMTP username and password, stored           | The machine's role, or an access key                 |
| Outbound port           | 587                                              | 443                                                  |
| Per-recipient rejection | Named at `RCPT TO`, so the address is suppressed | SES refuses the message and names nobody - see below |
| Sending events          | Identity notifications or a configuration set    | A configuration set                                  |

The middle row is the one worth reading twice. On the SMTP path a `550`
at `RCPT TO` names the address that was refused, and Mailyard suppresses it. The API accepts or refuses the whole
message and returns one id - it never says which recipient it objected to. So an API-path refusal suppresses **nobody**,
on purpose: bounces still arrive later over SNS, and guessing at the first recipient would block an address on no
evidence.

## Verify your identity in SES

Required for both paths.

SES only accepts mail whose `From` address belongs to a **verified identity**. Verify the whole domain (SES console:
*Identities* >
*Create identity* > *Domain*) rather than a single address - it unlocks Easy DKIM, which you will want below.

While your SES account is in the **sandbox**, recipients must also be verified identities. Request production access
(SES console >
*Account dashboard*) before pointing real traffic at it, or every send to an unverified address fails with:

```
554 Message rejected: Email address is not verified
```

## Over SMTP

### 1. Create SMTP credentials

SES SMTP credentials are **not** your AWS access key. Generate them in the SES console under *SMTP settings* > *Create
SMTP credentials*. You get a username (looks like an access key id) and a password (a derived signature). They are
region-bound.

### 2. Add the server in Mailyard

*SMTP Servers* > *Add server*, or via the API:

```bash
curl -X POST http://localhost:3000/api/v1/smtp-servers \
  -H "Authorization: Bearer myk_..." \
  -H "Content-Type: application/json" \
  -d '{
    "name": "AWS SES eu-west-1",
    "host": "email-smtp.eu-west-1.amazonaws.com",
    "port": 587,
    "encryption": "starttls",
    "username": "<ses-smtp-username>",
    "password": "<ses-smtp-password>"
  }'
```

- The host is `email-smtp.<region>.amazonaws.com` - use the region where your identity is verified.
- Port **587 with `starttls`** is the standard pairing. `465` with
  `ssl` also works. Port 25 is throttled inside AWS networks, avoid it.
- Run *Test connection* after saving - it authenticates without sending, so it catches a wrong region or bad credentials
  immediately.

Consider restricting the server's **allowed senders** to your SES-verified domain (`*@example.com`). SES rejects
anything else at send time anyway, so the restriction turns a late SMTP error into an early validation error.

## What SES rewrites, and why it matters

This is the part that surprises people.

### Return-Path

SES replaces the envelope sender (`MAIL FROM`, which becomes the
`Return-Path` header at the receiver) with its own bounce address at
`<region>.amazonses.com`. That is how SES collects bounces and complaints for its reputation system.

Consequences:

- **SPF is evaluated against `amazonses.com`, not your domain.** The SPF record Mailyard shows on the domain page (the
  `include:` you publish for direct sending) is simply not consulted for SES traffic. This is normal.
- **SPF can never produce DMARC alignment on its own.** For DMARC you need DKIM alignment instead (next section), or
  configure a **custom MAIL FROM domain** in SES (*Identity* > *Custom MAIL FROM domain*, publish the MX and SPF records
  SES gives you). With a custom MAIL FROM, the Return-Path becomes `mail.example.com` and SPF aligns again.
- Bounces do not flow back to Mailyard's SMTP conversation after acceptance, and no return path can collect them either,
  because the envelope SES puts on the wire is its own. Feedback comes over SNS instead -
  see [SES Notifications](/docs/smtp-domains/ses-notifications)
  for the four steps. Nothing to configure per project.

### Message-ID and Date

SES **overwrites the `Message-ID` and `Date` headers** with its own values on every message.

Mailyard signs outbound mail with the domain's DKIM key once the domain is verified, and that signature covers
`Message-ID` and `Date` - as any sound signature must, since they are part of what the message *is*. SES changing them
after signing means:

- **Mailyard's own DKIM signature arrives broken** at the receiver (`dkim=fail` for `d=example.com` with your `mailyard`
  selector).
- A broken signature is treated by receivers as if it were absent (RFC 6376), so this does not bounce mail by itself -
  but it also contributes nothing.

**The fix is Easy DKIM.** Enable it on the SES identity and publish the three CNAME records SES shows you. SES then
signs every message with `d=example.com` *after* its rewrites, so receivers see a valid, DMARC-aligned signature for
your domain. With Easy DKIM in place, Mailyard's broken signature is harmless noise and your DMARC policy passes via
DKIM alignment.

Once Easy DKIM carries your DMARC, turn on **Skip DKIM signing** on the SES server in Mailyard (server settings, or
`"skip_dkim": true` in the API). Mail routed through that server then goes out without the doomed local signature, while
every other SMTP server keeps signing as usual.

In short, when relaying through SES:

| Concern       | Who handles it                                |
|---------------|-----------------------------------------------|
| DKIM          | SES Easy DKIM (publish the 3 CNAMEs)          |
| SPF alignment | SES custom MAIL FROM domain (optional)        |
| DMARC         | passes via the Easy DKIM signature            |
| Bounces       | SES event destinations, fed into suppressions |

The DKIM record on Mailyard's domain page still matters for any mail you send through *other* SMTP servers on the same
domain. Publishing both it and the SES CNAMEs is fine - they are different selectors and do not conflict.

## Rate limits

SES enforces a per-second send rate and a 24-hour quota per account. Mailyard's delivery worker retries throttled sends,
but if you are pushing campaign volume, set the project plan's hourly and daily limits below your SES quota so the queue
smooths the burst instead of hammering into `454 Throttling failure`.
