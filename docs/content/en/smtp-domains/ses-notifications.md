---
title: "SES Notifications"
description: "Receive Amazon SES bounces and complaints over SNS"
weight: 80
---

Amazon SES replaces the envelope sender with its own so the receiver's bounce comes back to Amazon. That is not
something to work around - it is how SES collects the feedback its reputation system runs on. It does mean **no return
path you could set will ever see an SES bounce**, so the news has to come back another way.

That way is SNS. SES publishes bounce and complaint notifications to a topic, and the topic posts them to Mailyard over
HTTPS.

## Setup

### 1. Create an SNS topic

A standard topic in the same region as the SES identity. Note its ARN.

### 2. Put the ARN on the server

Open the SMTP server that sends through SES - **Infrastructure -> SMTP Servers**, or **Admin -> Shared SMTP Pool** for a
platform-owned one - and paste the topic ARN into **SES topic ARN**.

There is no server-wide setting and no restart. The ARN lives on the server because SES belongs to one: your SES account
is configured as your server, so a platform-wide list could only ever serve whoever owned the AWS account.

`POST /webhooks/ses` always exists. With no server carrying a topic it accepts nothing, which is why it needs no switch
of its own.

### 3. Subscribe the topic to that URL

In SNS, add a subscription of protocol **HTTPS** with endpoint
`https://mail.example.com/webhooks/ses`.

SNS immediately posts a `SubscriptionConfirmation`. Mailyard verifies its signature, checks the topic is configured on a
server, and confirms it automatically - the subscription goes to **Confirmed** on its own. If it stays pending, the log
says why.

### 4. Turn on the notifications, with original headers

On the SES identity: **Verified identities -> your domain -> Notifications ->
Feedback notifications -> Edit**.

- Bounce feedback: your SNS topic
- Complaint feedback: your SNS topic
- **Include original email headers: checked**

{{< callout type="warning" title="The headers checkbox is not optional" >}}
It is what carries `X-Mailyard-Email-Id` back, and that header is the only thing that says which message a notification
is about. Without it every notification is logged as unattributed and nothing is recorded. Nothing is guessed from the
bounced address alone, because that would let anyone who can reach the endpoint suppress an address just by naming it.
{{< /callout >}}

Feedback forwarding by email can be turned **off** once this works. The two are independent, and running both means
processing every bounce twice.

{{< callout type="note" title="If you already publish through a configuration set" >}}
An event destination pointed at the same topic works and needs no extra setting here. AWS renames one key in that
payload - `notificationType` becomes `eventType` - and Mailyard reads both, so bounce and complaint events are recorded
identically. Original headers are still what carries the id, under **Event publishing -> your destination**, and the
extra event types a configuration set can emit (sends, opens, clicks) are ignored.
{{< /callout >}}

## Why the allowlist

The endpoint is public - SNS presents no session and no API key. Two separate things authenticate a notification, and it
needs both:

- **The SNS signature.** Verified against the certificate the message names, which is fetched only from
  `sns.<region>.amazonaws.com`. That host check is the load-bearing part: the message names the URL of the key that
  verifies it, so without pinning the host an attacker signs with their own key, points at their own certificate, and
  every signature they produce validates.
- **The topic being configured on a server.** A valid signature only proves *some*
  AWS account sent the message. Anyone can open one. The ARN on your server is what makes a notification yours, and no
  server carrying a topic means nothing is accepted rather than everything.
- **The message having left through that server.** A notification from topic T may only speak about mail that a server
  publishing to T actually delivered. Attribution still comes from the header - this is what stops one tenant's topic
  reporting on another tenant's mail.

A subscription confirmation is honored only for a topic some server carries, so nobody can point a topic of their own at
your endpoint and have it confirm itself.

## What is recorded

| SES notification                      | Recorded as | Suppressed |
|---------------------------------------|-------------|------------|
| Bounce, `Permanent`                   | hard bounce | yes        |
| Bounce, `Transient` or `Undetermined` | soft bounce | no         |
| Complaint                             | complaint   | yes        |
| Delivery and everything else          | nothing     | no         |

Then the same two rules as every other feedback channel: the id must name a real message, and each reported recipient
must be one that message actually went to. See
[Bounce Handling](/docs/smtp-domains/bounce-handling). SES additionally has to clear the topic-to-server check above,
which the DSN path has no equivalent of.

## Troubleshooting

| Log line                                             | Meaning                                                                        |
|------------------------------------------------------|--------------------------------------------------------------------------------|
| `notification from an unlisted topic`                | No SMTP server carries this ARN. Logged at debug, since the endpoint is public |
| `reporter is not entitled to report on this message` | The message did not leave through a server publishing to this topic            |
| `failed signature verification`                      | Not from Amazon, or modified in transit                                        |
| `report carries no sending id`                       | Include original email headers is off                                          |
| `original headers were truncated at 10 kb`           | It is on, but the message's own headers exceeded what SES forwards             |
| `names an unknown sending id`                        | The message is not in this install, or was purged by retention                 |

Mailyard answers `200` to a notification it accepts and to one it decides to drop, because SNS retries a non-2xx for
hours and no retry would help. Only a message that fails authentication gets a `403`, so a misconfigured topic shows up
in the SNS delivery status rather than looking like a success.
