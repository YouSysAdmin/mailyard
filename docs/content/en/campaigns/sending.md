---
title: "Sending & Lifecycle"
description: "Send, pause, resume, and cancel campaigns"
weight: 20
---

## Starting a send

```
POST /api/v1/campaigns/{id}/send
```

The body is optional. Send nothing and the campaign starts now:

```bash
curl -X POST http://localhost:3000/api/v1/campaigns/$ID/send \
  -H "Authorization: Bearer myk_..."
```

Or name a time, RFC 3339:

```bash
curl -X POST http://localhost:3000/api/v1/campaigns/$ID/send \
  -H "Authorization: Bearer myk_..." \
  -H "Content-Type: application/json" \
  -d '{"scheduled_at": "2026-04-01T09:00:00Z"}'
```

The campaign moves to `sending` in the first case and `scheduled` in the second. A `scheduled_at` in the past is
refused — unlike a plain transactional send, there is no tolerance window here, because a campaign is not something you
fire in a retry loop.

Only a `draft` or `scheduled` campaign can be sent. Anything else answers `409`.

### Two checks run before anything is queued

Both are deliberately up front, so a mistake costs you one refused request rather than a hundred thousand failed
messages.

{{< callout type="warning" title="Campaigns require a public URL" >}}
A campaign will not start unless `server.public_url` and `auth.jwt_secret` are set. The refusal explains why: the
one-click unsubscribe link is absolute and signed, and without those it cannot be built.

Sending anyway is not a lesser evil. Gmail and Yahoo have required one-click unsubscribe from bulk senders since
February 2024, and mail without it is **filtered rather than bounced** — so the whole audience would go to spam and
nothing in your logs would say so.
{{< /callout >}}

The `from_email` domain is also checked for
[verification by this project](/docs/smtp-domains/domain-verification), once, here — rather than once per recipient
while the send fails its way through the audience.

## How it runs

The runner works in batches (`campaign.batch_size`, 100 by default), resolving the list, rendering the template per
subscriber and queueing the results. Recipients who are suppressed, or who hold a per-list opt-out, are marked
`skipped` rather than mailed.

**`send_rate`** caps throughput in emails per minute, applied between batches. Zero is unthrottled. It is the control
for staying inside a provider's rate limit, and for not putting a cold IP through a step change in volume.

**`send_at_local_time`**, together with `scheduled_at`, delivers at that wall-clock time in each subscriber's own
timezone — 09:00 in Lagos and 09:00 in Lisbon, two hours apart. Subscribers with no timezone recorded get the scheduled
instant as it stands.

## Pause and resume

```
POST /api/v1/campaigns/{id}/pause
POST /api/v1/campaigns/{id}/resume
```

Pausing stops the runner **between batches**. Messages already handed to the delivery queue still go out — there is no
recall — so expect a short tail after the pause takes effect. Resuming picks up at the next unsent recipient rather than
starting over.

This is the control for "the copy is wrong, stop": it will not un-send what has already left, and the sooner it is
pressed the less has.

## Cancel

```
POST /api/v1/campaigns/{id}/cancel
```

Works from `sending`, `paused` or `scheduled`, and is final — there is no resume from `cancelled`. Recipients who had
not been reached are marked `skipped`, which is how the message counts still add up to the audience size afterwards.

## Duplicate

```
POST /api/v1/campaigns/{id}/duplicate
```

Copies the definition into a new `draft` with " (copy)" on the end of the name. Every setting carries over: template,
list, language, template data, send rate, variants.

This is how you re-run anything. A `sent` campaign cannot be edited or started again, so the second attempt is always a
copy — which also keeps the first one's statistics intact instead of overwriting them.

## The transitions, in full

```
draft ──────────────► sending ──────────────► sent
  │                     │  ▲                    (every message terminal)
  │                     │  │
  │                     ▼  │
  │                    paused
  │                     │
  ├──► scheduled ───────┤   (at the scheduled time, into sending)
  │                     │
  └─────────────────────┴──► cancelled
```

`sent` is reached by the runner, not by a call — when every message has reached a terminal state. Note what is missing:
nothing returns to `draft`. Once a campaign has been sent, its definition is frozen, and duplicate is the way forward.
