---
title: "Rate Limiting"
description: "Email and API rate limits"
weight: 60
---

Mailyard enforces rate limits to prevent abuse and ensure fair usage.

There are two independent mechanisms: **send volume** limits, which come from the project's plan, and **request rate**
limits on the HTTP and SMTP edges.

## Email Send Limits

Send volume is governed by the [plan](/docs/admin/plans) assigned to the project (`hourly_email_limit`,
`daily_email_limit`, `0` meaning unlimited). Counts are read from the emails table over the trailing hour and day, so
there is no counter state to drift.

Exceeding a limit returns `429 Too Many Requests` on the HTTP API:

```json
{
    "error": "hourly email limit reached (100 per hour on plan \"Free\")"
}
```

The SMTP relay maps the same condition to a transient `452 4.7.0`, so a well-behaved client retries after the window
rolls.

Each recipient in a batch send counts separately. A batch of 100 recipients counts as 100 emails.

`GET /api/v1/usage` reports the current limits alongside consumption.

## Request Rate Limits

Fixed-window counters on the HTTP edge, configured under `ratelimit`:

| Setting                                   | Env                                                | Default | Description                                                |
|-------------------------------------------|----------------------------------------------------|---------|------------------------------------------------------------|
| `ratelimit.enabled`                       | `MAILYARD_RATELIMIT_ENABLED`                       | `true`  | Master switch for every limiter below.                     |
| `ratelimit.login_per_minute`              | `MAILYARD_RATELIMIT_LOGIN_PER_MINUTE`              | `10`    | Console sign-in, keyed by client IP.                       |
| `ratelimit.oidc_per_minute`               | `MAILYARD_RATELIMIT_OIDC_PER_MINUTE`               | `30`    | The OIDC callback, keyed by client IP.                     |
| `ratelimit.api_per_minute`                | `MAILYARD_RATELIMIT_API_PER_MINUTE`                | `120`   | `/api/v1/*`, keyed by API key (falling back to client IP). |
| `ratelimit.ses_webhook_per_minute`        | `MAILYARD_RATELIMIT_SES_WEBHOOK_PER_MINUTE`        | `600`   | `POST /webhooks/ses`, keyed by client IP.                  |
| `ratelimit.relay_node_chatter_per_minute` | `MAILYARD_RATELIMIT_RELAY_NODE_CHATTER_PER_MINUTE` | `600`   | Relay node heartbeats, certificate renewal and status.     |
| `ratelimit.relay_node_inbound_per_minute` | `MAILYARD_RATELIMIT_RELAY_NODE_INBOUND_PER_MINUTE` | `1200`  | Mail a relay node forwards back to the platform.           |

Setting an individual value to `0` disables that limiter while leaving the others in place.

The login limiter is per client address. The account counts its own failures too: ten wrong passwords in a row lock
sign-in for that account for fifteen minutes, whatever addresses they came from, and the answer during the lockout is the
same `invalid credentials` a wrong password gets. A right password clears the count. Passkey and SSO sign-in are not
subject to it - neither takes a password to guess.

The last three are paced by other software rather than by a person at a keyboard, which is why they sit an order of
magnitude higher. SNS retries hard and for hours, so throttling it loses bounces. A hundred nodes reporting every two
minutes from behind one NAT address is fifty legitimate requests a minute, and setting that budget too low drops the
whole fleet out of the pool at once.

{{< callout type="warning" title="Raise the forwarding budget, do not lower it" >}}
`relay_node_inbound_per_minute` is the one endpoint here whose rate **strangers** set, because a node's MX takes
whatever the internet sends it. It is a runaway guard, not a delivery policy: by the time a request reaches it the node
has already answered `250` at the SMTP layer, so a refusal loses a message the sender believes was accepted. The per-IP
filtering belongs on the node, in `relay_node.inbound.rate_per_minute`.
{{< /callout >}}

## SMTP Listener Limits

Separate from the table above and configured where each listener is declared. Both count **new sessions per client IP
per minute**, not messages - one session can carry many - and both refuse with a transient `421 4.7.0` so a well-behaved
sender retries. `0` disables one.

| Setting                              | Default | Applies to                                                          |
|--------------------------------------|---------|---------------------------------------------------------------------|
| `submission.rate_per_minute`         | `60`    | The submission listener, mail from an application you authenticate. |
| `inbound.rate_per_minute`            | `120`   | The MX-facing listener, mail from the internet.                     |
| `relay_node.inbound.rate_per_minute` | `120`   | A relay node's own MX, set in the config file on that machine.      |

The check runs when the session opens, before AUTH and before anything is parsed, so an abusive client is shed before it
costs a database round trip.

{{< callout type="note" title="Multi-node deployments" >}}
Every one of these windows is held in process memory. Across N nodes the effective ceiling is the configured value times
N. Size accordingly, or terminate the limit at a shared reverse proxy. Plan-based send limits do not have this
property - they count rows in the shared database.
{{< /callout >}}

## Trusting a Proxy

Both edges key on the TCP peer address, and behind a load balancer that address is the balancer's. Each edge has its own
way of learning the real one, because HTTP and SMTP are not the same problem.

### HTTP

`server.trusted_proxies` lists the IPs or CIDRs of your balancer, and `X-Forwarded-For` is then read - but only when the
connection itself came from one of those hops. Without it, every request behind a proxy shares one bucket.

**List every hop, not just the nearest one.** The header is a list, each hop appends the address it saw, and the entries on
the left are whatever the caller chose to send. So the address is resolved by reading the list **from the right** and
stopping at the first entry that is not one of your proxies - a hop you leave out of the list becomes the answer, and a
caller who sends the header cannot become someone else by doing it:

```
X-Forwarded-For: 8.8.8.8, 203.0.113.9, 10.0.0.7
                 ^ sent by the caller
                              ^ what your edge proxy saw, and the answer
                                            ^ your internal hop, in trusted_proxies
```

A chain that cannot be read to the end - an entry that is not an address, which some proxies write as `unknown` - falls
back to the address that connected, rather than trusting the rest of the list.

### SMTP

There is no `X-Forwarded-For` in SMTP - the protocol has nowhere to put the original address - so the balancer has to
speak the **PROXY protocol** and the listener has to read it. Available on all three SMTP listeners, v1 and v2, off by
default:

```yaml
submission:
    proxy_protocol:
        enabled: true
        trusted:
            - 10.0.0.0/8      # a CIDR
            - 192.0.2.7       # or a bare address
```

The same block exists under `inbound` and, in the config file on the node itself, under `relay_node.inbound`. As
environment variables the list is **comma separated**:

```
MAILYARD_SUBMISSION_PROXY_PROTOCOL_ENABLED=true
MAILYARD_SUBMISSION_PROXY_PROTOCOL_TRUSTED=10.0.0.0/8,192.0.2.7
```

Leaving it off behind a balancer costs more than a wrong log line:

- **The per-IP session limits become installation-wide.** `submission.rate_per_minute: 60` stops meaning 60 per client
  and starts meaning 60 in total, so the 61st connection in a minute is refused `421 4.7.0` no matter who opened it.
- **SPF is computed for the wrong host.** The MX checks the connecting IP against the sender's SPF record, and no
  sender's record names your balancer.
- **`client_ip`** on every inbound and sandbox message records a hop of your own.

{{< callout type="danger" title="`trusted` may not be empty" >}}
A PROXY header is an unauthenticated claim about who is calling. Honoring one from an arbitrary peer is worse than not
reading it at all: anyone able to reach the port could assert any address, which forges an SPF pass as that sender and
spends their rate budget instead of their own. Enabling the protocol without a trusted list is refused at startup.

A peer outside the list is never asked for a header at all, so its address is the real one and a `PROXY ...` line it
writes is answered `500` as the unknown command it is. A trusted peer that sends **no** header is refused - a proxy
always sends one, and guessing would silently put the balancer's address back.
{{< /callout >}}
