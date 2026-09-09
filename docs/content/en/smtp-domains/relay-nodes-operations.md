---
title: "Running Relay Nodes"
description: "Enrolling, approving and operating your own egress machines"
weight: 51
---

What a relay node IS, and why you would want one, is on
[Relay Nodes](/docs/smtp-domains/relay-nodes). This page is how to run one.

{{< callout type="danger" title="Read this before you provision anything" >}}
The software is the easy part. Four things decide whether a node delivers at all,
and **none of them are fixed by Mailyard**.

**Outbound port 25 is blocked by default** at AWS, GCP, Azure, DigitalOcean, Hetzner
and most others. Unblocking is a support request that is routinely refused. The
symptom is every connection timing out, which looks like a slow network rather than
a closed port - so the node tests it at startup and says so plainly.

**The PTR record must exist and must match the node's hostname.** Gmail and Outlook
reject on a missing or mismatched PTR before looking at the message. You set PTR in
your hosting provider's control panel, not here. The node checks it at startup.

**A new IP has no reputation.** Volume ramps over weeks. A fresh address that sends
100k on day one is blocked, not throttled. Register for Microsoft SNDS/JMRP and
Google Postmaster Tools per address.

**Every node's IP must be in your bounce domain's SPF record.** Shared-pool mail
leaves with `sending.bounce_address` as its return path, and the receiver checks that
domain's SPF against the address that connected - which is now the node's. The Relay
Nodes admin page gives you the exact fragment to paste.
{{< /callout >}}

## How a node joins

```yaml
# on the Mailyard control plane
relay_nodes:
  enabled: true
  auto_register_token: "<openssl rand -hex 32>"
```

```yaml
# on the node
relay_node:
  control_url: "https://mail.example.com"
  enroll_token: "<the same token>"
  hostname: "node1.mail.example.com"   # must match this host's PTR
  addr: ":2587"
  spool_dir: "/var/lib/mailyard/relay"
```

Then `mailyard relay`. Every key the node reads, with its default, is in
`examples/mailyard-relay.yaml.example` in the release archive.

On first run the node generates a keypair, sends a **certificate signing request**,
and receives a certificate signed by your installation's own authority. The private
key never leaves the machine. After that the certificate is its identity and the
enrolment token can be removed from the config.

The node appears under **Admin -> Relay Nodes** as `pending`. It carries no mail
until you approve it.

{{< callout type="warning" title="Approval is not a formality" >}}
A node in the pool receives the **content** of real messages to deliver, and one
token enrols the whole fleet. With approval in the way, a leaked token gets somebody
a pending row you will not recognise. Without it, it gets them a copy of everybody's
mail. The `relay_nodes_auto_approve` [setting](/docs/admin/platform-settings) exists
for autoscaling groups and is off by default.
{{< /callout >}}

## A node for one project

The same binary, enrolled by a tenant instead of the operator. It becomes one of
that project's own SMTP servers rather than joining the shared pool.

```yaml
relay_node:
  control_url: "https://mail.example.com"
  enroll_token: "myk_..."          # an API key holding relay:write
  hostname: "smtp.user.com"
  spool_dir: "/var/lib/mailyard/relay"
```

Create the key under **Developers -> API Keys** holding `relay:write`. A key without
that permission is refused.

It is a resource of its own rather than part of `smtp`, because enrolling a node hands
that machine the CONTENT of the project's outbound mail to deliver - a credential given
to a deployment script for that one job should not also read the email log. The node appears under the project's own relay node
list and an admin **of that project** approves it - not a platform admin.

Two consequences follow without any further setup:

- **Owning a node means owning delivery.** The shared pool applies only to a project
  with no server of its own, and a node is a server. Enrolling one takes the project
  off the pool entirely, exactly as adding a server by hand does.
- **It lands in a server group like any other server** - the default one unless
  `relay_node.server_group` names another. A slug that does not exist is refused at
  enrolment rather than quietly falling back, because a node silently carrying
  traffic somebody routed elsewhere is worse than a node that will not start.
- **The return path becomes the project's own.** Mail leaves from the node's address
  under the project's `bounce_address`, so SPF, PTR and reputation all belong to the
  tenant - which is the point of running one.

{{< callout type="warning" title="The deliverability burden moves with it" >}}
Every warning at the top of this page becomes the tenant's problem: outbound port 25,
the PTR record, warming the address, feedback loops. A project running its own node
is running a mail server, and Mailyard cannot do those parts for them.
{{< /callout >}}

## Seeing and ending the authority

Your installation's relay authority is on **Admin -> Certificates**, with what it
has signed: the certificate every delivery worker presents, and one record per
node it issued to. Those records are the public half only - a node generates its
own key and never sends it, which is the whole reason enrolment uses a
certificate request.

**Destroy** is the emergency lever. It exists because an authority nobody can
audit or end is not much of an authority.

It takes the whole fleet off at once, and it removes the node enrolments too.
That second part is not an extra: a node left holding a certificate signed by a
destroyed authority keeps heartbeating, keeps being listed as alive, and quietly
carries nothing - so the console would show a healthy fleet that delivers no
mail.

Afterwards:

- A new authority is minted the moment anything enrols. There is nothing to set
  up.
- **Each node has to enrol again, and will not do it by itself.** A node holding
  a stored identity keeps using it, so give it `relay_node.enroll_token` again -
  or clear its `spool_dir`, which is what "enrol as a new node" means.
- Other API nodes keep the old authority cached until they restart.

## Why there is no password

The worker-to-node hop crosses the public internet. A username and password would be
a long-lived secret every worker holds, proving only that the holder has the
password - in one direction.

Instead both ends present certificates from your installation's private authority.
The node proves it is the node you enrolled, the worker proves it is yours, and
removing a node is un-enrolling it rather than rotating a secret every other node
also has. The listener runs implicit TLS with a verified client certificate and
offers no AUTH at all.

The names in a certificate request are **ignored**: a node is signed for the name the
control plane decided it may answer to. A node asking for a neighbour's name gets its
own.

## What the node does with a message

It spools it, then delivers it.

- **The bytes are forwarded unchanged.** They were DKIM-signed by the process that
  built them and the node cannot re-sign, so it reads one header and rewrites
  nothing.
- **Recipients are judged individually.** Some accepted and some refused in one
  session is ordinary, and only the deferred ones are retried - which is what stops a
  retry delivering twice.
- **TLS is opportunistic** (RFC 7435): preferred, verified when possible, and never a
  reason to refuse delivery. Most of the internet's mail exchangers present
  certificates that do not verify for the name their MX record gives.
- **Retries back off** to a four hour ceiling and stop at `max_lifetime`, three days
  by default.
- **The queue survives a restart.** The node has already told the worker it accepted
  the message, so losing it would lose mail Mailyard records as sent.

## A node that cannot be reached

By default the delivery worker dials the node: `relay_node.addr` is a port the platform must be able to open a
connection to, over mutual TLS. A node behind NAT, or one that may only leave its network through an HTTP proxy, cannot
offer that port. Such a node runs in **pull mode**:

```yaml
relay_node:
  mode: pull
```

Nothing about enrolment, heartbeats, reporting or receiving changes - all of that was already the node calling the
platform. What changes is the mail: instead of dialling the node, the worker builds the finished message - signed,
return path set, exactly the bytes a dialled node would have been handed - and **assigns** it to the node. The node
claims its assignments with `POST /api/relay-nodes/claim`, a request that parks on the platform for up to thirty seconds
and returns the moment something is assigned, so a message reaches the node within a round trip and an idle node costs
two requests a minute. From there the message is in the same spool and follows the same delivery, retry and reporting
path as one that arrived over SMTP.

The control channel is ordinary HTTPS and honours `HTTPS_PROXY`, so a pull node needs no inbound port at all.

Three things to know:

- **The platform decides who holds a message.** While an assignment stands the email row is `processing` and belongs to
  that node. The node says on every claim which messages it still has, which keeps the assignment alive. A node that
  stops claiming - it crashed, it lost its link - loses its assignments after `relay_nodes.assignment_ttl` (five minutes)
  and the messages go to the next server in the group, the same failover a refused dial gets.
- **Claiming is safe to repeat.** A claim changes nothing on the platform: a node that fetched a batch and crashed
  before writing it gets the same batch again. What ends an assignment is the node's report, recipient by recipient.
- **A report finishes the message.** Every recipient delivered or refused is what turns the row `sent` or `failed`, and
  every hook a worker-delivered message fires - webhooks, the live feed, campaign status, contact tallies - fires from
  that report.

A fleet can mix modes: each node reports its own on every heartbeat.

## How you learn what happened

A node reports every terminal outcome back, keyed on the same
`X-Mailyard-Email-Id` header that carries [bounce
attribution](/docs/smtp-domains/bounce-handling) everywhere else. Failures become
bounce rows through the same intake as a DSN or an SES notification, under the same
two rules: the id must name a real message, and each recipient must be one that
message went to.

Two outcomes are final and they are not the same:

| Outcome | Recorded as | Suppressed |
|---|---|---|
| Permanent refusal (5xx, null MX, no such domain) | hard bounce | yes |
| Ran out of time after `max_lifetime` | soft bounce | no |

The second is deliberate. Running out of time is our failure, not the address's, and
suppressing a good mailbox over it would be worse than the original delay.

Outcomes are written to the node's own queue before they are sent, so a node that
crashes with a report pending still delivers it afterwards.

## When a node goes quiet

A node reports in every two minutes. If nothing is heard for ten, the pool stops
offering it mail - and that check lives in the query the delivery path runs, not only
in the background sweep that marks it in the console. A sweep that stops running must
not mean mail routed to a machine that is gone.

The node keeps delivering whatever it already holds.

## A node that also receives

A node can run an MX of its own and forward what it receives back to Mailyard.

The reason is a network Mailyard cannot be reached from. Mail is sent into a
region behind a national firewall, the bounces come back to your bounce domain -
and they never arrive, because the connection they need is the one that does not
work. Nothing is wrong with the sending, and nothing in the console says so:
bounces simply stop.

Point the **MX record at the node** instead. It takes the mail on port 25 from
inside that region, and forwards it to Mailyard over the same HTTP control
channel it already uses for everything else - which means it goes out through
whatever proxy or tunnel that node has (`HTTPS_PROXY`, `ALL_PROXY`, including
`socks5://`, are honoured).

```yaml
relay_node:
  inbound:
    enabled: true
    addr: ":25"
```

Two consequences worth stating plainly:

- **Mailyard itself no longer needs a port 25.** `inbound.enabled` can stay off
  on the platform entirely. Several nodes are several MX records with
  priorities, and mail retries on its own.
- **The node holds message content on its disk** until it is forwarded, in
  whatever jurisdiction it sits in. That is the trade the arrangement is made of.

### What the node decides on its own

Nothing about whose mail it is. A node has no database, so:

- It is handed the list of **recipient domain names** this installation receives
  mail for, on the heartbeat, and caches it. Names only - never which project
  owns them.
- RCPT is answered from that list, by the same whole-label rule the platform
  uses, so a subdomain of a verified name is accepted here exactly as it would be
  there.
- Before the list has ever arrived, an unknown recipient gets a **451**, not a
  550. "I do not know yet" is not "no such domain", and a hard refusal would
  bounce good mail for the minutes between a node starting and its first
  heartbeat.
- Everything else - suppression, SPF/DKIM/DMARC, dedup, whether a message is a
  delivery report - is decided by Mailyard when the message arrives, through the
  same pipeline the platform's own MX feeds.

### The queue

The node writes the message down **before** it answers 250, and deletes its copy
only once Mailyard has taken it. That is the whole point: the link to the
platform is the unreliable part, and it is not touched until after the sending
MTA has been told the mail is safe.

If the platform is unreachable the node retries with a growing backoff and gives
up only at `max_lifetime`, the same window the outbound queue uses.

That queue is reported on every heartbeat and shown on the Relay Nodes page, and
it is the only symptom there is. A count that only grows means the node is fine
and the link back is not - where the mail itself just goes quiet.

### DNS

The Relay Nodes page prints the exact MX record set once a node is approved and
reporting an MX, next to the SPF fragment. A platform admin sees the platform's
nodes, a project sees its own.

```
bounce.user.com   MX   10   node1.example.com.
```

Nothing else in the product will tell you this step is missing. A node that
receives is inert until DNS points at it, and the symptom - bounces that stop
appearing - reads exactly like nothing bouncing.

{{< callout type="info" title="What a node asserts" >}}
Until now a node **carried** bytes. A node that receives also **asserts a fact**:
the address it saw connect. SPF is computed from that, so the verdict on a
forwarded message is exactly as honest as the node.

This is the same trust tier a node already sits in - it holds a certificate from
your authority and receives the content of your outbound mail - but it is the
first time its word lands in a field somebody reads as a check. Approval gates
it: a node that is merely enrolled is refused, and is not handed the domain list
either.
{{< /callout >}}

### TLS

`relay_node.inbound.tls` takes a `cert` and `key` on the node's disk. With
neither set the node generates a self-signed pair and keeps it in the spool.

That is not a compromise. Inbound mail on the internet uses opportunistic TLS:
senders prefer it, almost none verify the certificate, and a receiver that
insisted on a verifiable one would simply not receive.

### A project's own node

A tenant node receives too, and is narrowed in two places rather than trusted
the way the platform's own MX is:

- Its accept list holds **only its own project's** verified domains, so a
  neighbour's name is refused at RCPT exactly like one nobody verified - the
  same 550, so this cannot be used to discover what a neighbour has registered.
  The platform re-checks ownership when the message is forwarded, so the answer
  does not depend on the node having an honest list.
- A delivery report forwarded by that node may only attribute to **its own
  project's** mail. The unscoped rule is right for the platform, where a
  provider forwards its bounce copy to a mailbox on the operator's domain and
  the two projects routinely differ. It is wrong for a machine on a tenant's
  network: filing a bounce against a neighbour otherwise needs only a message id
  and one address that message really went to.

The report itself is still stored - it arrived at a domain the project owns -
it just files no bounce and suppresses nothing.

## Configuration

| Key | Default | |
|---|---|---|
| `relay_node.control_url` | | Where the platform lives. Required. |
| `relay_node.enroll_token` | | First run only. |
| `relay_node.hostname` | | Certificate name AND the HELO announced to the internet. Must match PTR. |
| `relay_node.mode` | `listen` | `listen` binds `addr` for workers to dial. `pull` binds nothing and claims assigned mail over the control channel - see above. |
| `relay_node.addr` | `:2587` | Where workers connect. Listen mode only. |
| `relay_node.server_group` | | Slug of the project [server group](/docs/smtp-domains/server-groups) to join. Empty uses the default. Project nodes only. |
| `relay_node.spool_dir` | `./relay-spool` | The queue. Must survive a restart. |
| `relay_node.max_lifetime` | `72h` | How long a message may keep failing. |
| `relay_node.heartbeat_interval` | `2m` | Well under the ten minute stale window. |
| `relay_node.delivery_concurrency` | `8` | Simultaneous outbound sessions. |
| `relay_node.smtp_port` | `25` | Destination port. Change it only for testing. |
| `relay_node.inbound.enabled` | `false` | Run an MX on this node and forward what it receives. |
| `relay_node.inbound.addr` | `:25` | Where the internet delivers. An MX record carries no port. |
| `relay_node.inbound.max_message_size` | `26214400` | Must not exceed the platform's `inbound.max_message_size`. |
| `relay_node.inbound.rate_per_minute` | `120` | Per-IP session budget. The one listener here whose rate strangers set. |
| `relay_node.inbound.proxy_protocol.enabled` | `false` | Read the real client address from a balancer in front of this port. |
| `relay_node.inbound.proxy_protocol.trusted` | | Balancer addresses or CIDRs. Required when enabled - see [Rate Limiting](/docs/security/rate-limiting). |
| `relay_node.inbound.tls.cert` / `.key` | | STARTTLS pair. Neither set generates a self-signed one. |
| `relay_node.ipv6` | `false` | Off on purpose: a half-configured v6 address fails for reasons that look nothing like the cause. |

A node needs none of `database.dsn`, `database.crypto.encryption_key` or `auth.jwt_secret`, and
is refused at startup if it is missing what it does need.

Platform side, for pull nodes:

| Key | Default | |
|---|---|---|
| `relay_nodes.assignment_ttl` | `5m` | How long an assignment stands without the node claiming it, before the message goes to the next server. |
| `relay_nodes.claim_max` | `20` | Messages handed over per claim. |
| `relay_nodes.claim_wait_max` | `30s` | How long a claim may park waiting for an assignment. |
