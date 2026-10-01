---
title: "Bulk Sender Requirements"
description: "What Gmail, Yahoo, Microsoft and Apple demand of a domain that sends at volume, and which Mailyard feature answers each demand"
weight: 45
---

Since February 2024 Gmail and Yahoo hold a **bulk sender** to a fixed list of requirements, and a message that misses
one is **filtered rather than bounced**: the delivery report is clean, the recipient never sees it, and nothing in
your logs says so. This page is what the two ask for and where in Mailyard each demand is met. The originals are
Google's [Email sender guidelines](https://support.google.com/mail/answer/81126) and Yahoo's
[Sender Hub](https://senders.yahooinc.com/best-practices/), and those are the authority when this page and they
disagree.

## Who counts as a bulk sender

A domain sending about **5,000 messages a day** to Gmail addresses. Yahoo draws its line in the same place. Two things
about how that is counted decide how it applies to you:

- **It is counted per domain, not per address and not per IP.** Every From address on `example.com` and on its
  subdomains rolls into one figure, Google calls it the _primary sending domain_. Sending from several addresses
  does not split the count, and a dedicated IP does not lift the requirement.
- **It is the domain's whole traffic.** Receipts and password resets count toward the threshold beside the newsletter.
  A domain that crosses it is judged as a whole, so the rules below are worth meeting on transactional mail too.

Below the threshold nothing is enforced, but filtering is reputation driven either way, and a message without an
unsubscribe header goes to spam faster once complaints start.

## The requirements and where Mailyard meets them

| Requirement                                   | What it means                                                                                                   | In Mailyard                                                                                                                                                                                                                                                                         |
|-----------------------------------------------|-----------------------------------------------------------------------------------------------------------------|-------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------|
| **SPF and DKIM**                              | Both must pass for the domain                                                                                   | [Domain Verification](/docs/smtp-domains/domain-verification) publishes the SPF record and signs outbound mail with DKIM once the domain is claimed                                                                                                                                 |
| **DMARC**                                     | A policy on the From domain, `p=none` is enough, and the From domain must **align** with the SPF or DKIM domain | The DMARC record is on the same page. Alignment is automatic: Mailyard signs with the From domain's own key                                                                                                                                                                         |
| **One-click unsubscribe**                     | `List-Unsubscribe` with an `https` target plus `List-Unsubscribe-Post`, honoured within two days                | Campaigns carry both headers unconditionally, a transactional send carries them when scoped to an [unsubscribe list](/docs/contacts/unsubscribe-lists) or given [your own targets](/docs/tracking/unsubscribe#caller-managed-opt-out). The hosted endpoint answers the POST at once |
| **A visible unsubscribe link**                | In the body, for marketing and subscribed mail                                                                  | `{{ mailyard_unsubscribe_url }}` in the [template](/docs/templates/system-variables)                                                                                                                                                                                                |
| **Spam rate under 0.3%**                      | Measured in Google Postmaster Tools, aim for 0.1%                                                               | [Bounce handling](/docs/smtp-domains/bounce-handling) turns complaints into suppressions so the same address is not mailed again                                                                                                                                                    |
| **Forward and reverse DNS on the sending IP** | A PTR record that resolves back to the sending host                                                             | A property of the [SMTP server](/docs/smtp-domains/smtp-servers) or relay node you send through, not of Mailyard itself. Check it when adding a server                                                                                                                              |
| **TLS on the connection**                     | The SMTP session to their servers                                                                               | Configure the server with STARTTLS or SSL                                                                                                                                                                                                                                           |
| **No impersonation**                          | The From header must not imitate Gmail or another provider                                                      | [Sender addresses](/docs/smtp-domains/sender-addresses) and strict sender mode keep the From on verified domains                                                                                                                                                                    |

{{< callout type="warning" title="Turning the unsubscribe off" >}}
A campaign can be sent with `unsubscribe_disabled`, which drops both headers and the link. On a domain past the
threshold that is a decision to be filtered. It exists for the case where the opt-out is handled entirely elsewhere -
see [Campaigns](/docs/campaigns/overview).
{{< /callout >}}

## The other providers

Gmail and Yahoo published the list first and in the most detail. The others ask for the same things, with less of a
threshold and their own tooling:

| Provider                                   | Threshold                                                        | What they require                                                                                                                                                                                                                                    | Where complaints are read                                                                                                                      |
|--------------------------------------------|------------------------------------------------------------------|------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------|------------------------------------------------------------------------------------------------------------------------------------------------|
| **Yahoo, AOL**                             | About 5,000 a day                                                | The list above. AOL mailboxes are Yahoo's infrastructure and follow the same rules                                                                                                                                                                   | Yahoo's Complaint Feedback Loop, per verified domain                                                                                           |
| **Microsoft** (Outlook.com, Hotmail, Live) | About 5,000 a day to consumer addresses, enforced since May 2025 | SPF **and** DKIM **and** DMARC, `p=none` at least, aligned with SPF or DKIM. A miss lands in Junk first and is rejected with `550 5.7.515` later. Also asked for: a valid From and Reply-To, a working unsubscribe, bounce handling and list hygiene | SNDS (Smart Network Data Services) for IP reputation and JMRP (Junk Mail Reporting) for per-message complaints, both registered per sending IP |
| **Microsoft 365** (business tenants)       | Reputation based, no published figure                            | The same authentication trio. Tenant filtering is stricter than the consumer side and an unauthenticated domain is quarantined outright                                                                                                              | The same SNDS and JMRP                                                                                                                         |
| **Apple** (iCloud Mail)                    | None published                                                   | SPF and DKIM that pass, DMARC on the domain, reverse DNS on the IP, TLS, RFC 5321/5322 compliant messages, unsubscribes honoured, low complaint rates                                                                                                | No feedback loop. Apple's postmaster guidelines are the reference                                                                              |
| **Everyone else**                          | Varies                                                           | The same three records plus reverse DNS carry most of the weight. A provider that publishes no rules still scores on them                                                                                                                            | Their postmaster page, where one exists                                                                                                        |

Registering the sending IP with Microsoft's SNDS and JMRP is worth doing on day one: without JMRP a complaint from an
Outlook.com recipient reaches nobody, and the address keeps being mailed.

## What it adds up to

Every provider above, and the regional ones below, score the same handful of things. A domain that has all of them
is in good standing everywhere and the rest is reputation, which only volume and time build:

1. **SPF, DKIM and DMARC on the From domain**, DMARC at `p=none` or stricter and aligned with one of the other two.
   In Mailyard that is the four records on [Domain Verification](/docs/smtp-domains/domain-verification).
2. **Reverse DNS on the sending IP** that resolves back to the sending host, and **TLS** on the connection. Both
   belong to the [SMTP server](/docs/smtp-domains/smtp-servers) or relay node, not to Mailyard.
3. **A working opt-out**: `List-Unsubscribe` with a one-click target for anything a person subscribed to, honoured at
   once, and a link in the body. Campaigns do this by themselves, a transactional send does it through an
   [unsubscribe list](/docs/contacts/unsubscribe-lists).
4. **Complaints handled**: bounces and complaints turned into [suppressions](/docs/contacts/suppression-list) so the
   same address is never mailed twice, and the provider's feedback loop registered where one exists.
5. **A steady, honest sending pattern**: a consistent From on a verified domain, no sudden volume spikes from a fresh
   IP, and mail people asked for.

The difference between providers is how they punish a miss. Google and Yahoo filter quietly. Microsoft moves mail to
Junk and later refuses it. T-Online, GMX and the Chinese providers act at the IP level and refuse the connection.

## Regional providers

The same five points, with these particulars:

| Provider                                                         | Particulars                                                                                                                                                                                                                                                               |
|------------------------------------------------------------------|---------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------|
| **ukr.net** (Ukraine)                                            | Publishes rules for bulk mail: SPF, DKIM, DMARC, reverse DNS, an unsubscribe link and `List-Unsubscribe`, connection limits per IP. Complaints go through its own form rather than a feedback loop. Most Ukrainian mailboxes are Gmail, so the list above covers the rest |
| **GMX, web.de** (Germany)                                        | Strict on the three records and reverse DNS. Whitelisting through the Certified Senders Alliance (CSA) for high volume                                                                                                                                                    |
| **T-Online** (Germany)                                           | Refuses connections from an IP with no reverse DNS or no reachable postmaster contact, and unblocks by application                                                                                                                                                        |
| **Seznam** (Czechia), **WP, Onet** (Poland), **Orange** (France) | The standard set. Seznam publishes its own postmaster rules                                                                                                                                                                                                               |
| **Mail.ru, Yandex**                                              | SPF and DKIM required, DMARC recommended, `List-Unsubscribe` required for bulk mail, per-domain statistics and a complaint feedback loop on their postmaster sites, tight limits on an IP with no history                                                                 |

## China

Delivery into China is a different problem and deserves its own paragraph. **NetEase** (163.com, 126.com), **QQ Mail**
(Tencent), **Sina**, **Sohu** and **Aliyun** judge by the **sending IP** far more than by the domain:

- An IP with no history is **greylisted or refused at connection**, whatever the domain's records say. The three
  records and reverse DNS are still required, they are just not sufficient.
- **Rate limits are per IP and per connection**: messages per connection, connections per hour, recipients per
  message. NetEase publishes the figures in its anti-spam rules and blocks an IP that exceeds them.
- **Unblocking is by application** to the provider's postmaster, in Chinese, with the IP and the sending domain.
- **Content and links** are weighed harder: a body full of tracked links to foreign hosts scores worse than the same
  mail elsewhere.

What works in practice, in order of effort: a dedicated IP warmed up slowly with genuine mail, a
[server group](/docs/smtp-domains/server-groups) for Chinese recipients so their limits do not throttle everything
else, and for real volume a local relay such as Alibaba Cloud DirectMail or Tencent Cloud SES as the SMTP server of
that group, since their IPs already carry the reputation.

## What this means for a multi-tenant install

Projects are separate domains, so a shared IP does not merge their counts, and one project's complaints do not push
another over the line. Inside one project every From address on one domain shares a single count and a single
reputation: a marketing list and a transactional service on the same domain rise and fall together. Where that is a
concern, send them from different subdomains verified in different projects, and give bulk mail its own
[server group](/docs/smtp-domains/server-groups) so a campaign burning an IP does not take receipts down with it.
