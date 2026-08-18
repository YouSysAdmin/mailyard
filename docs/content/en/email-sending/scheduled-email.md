---
title: "Scheduled Email"
description: "Schedule emails for future delivery"
weight: 40
---

Any send can name a future time. Add `send_at` and the message is accepted, stored complete, and held until then.

Available on both `POST /api/v1/emails/send` and `POST /api/v1/emails/send-template`.

```bash
curl -X POST http://localhost:3000/api/v1/emails/send \
  -H "Authorization: Bearer myk_..." \
  -H "Content-Type: application/json" \
  -d '{
    "from": "sender@example.com",
    "to": ["recipient@example.com"],
    "subject": "Your appointment is tomorrow",
    "html": "<p>See you at 09:00.</p>",
    "send_at": "2026-03-20T09:00:00Z"
  }'
```

The response comes back immediately with status `scheduled` and the time echoed as `scheduled_at`.

## The timestamp

`send_at` must be **RFC 3339**, and it is converted to UTC on the way in. An offset is honoured, so
`2026-03-20T09:00:00+02:00` and `2026-03-20T07:00:00Z` are the same instant — write whichever is clearer at the call
site.

A time in the past is refused with `send_at is in the past`, but only once it is more than **a minute** behind. That
minute of slack is there so a client whose clock runs slightly fast does not get its sends rejected. Inside the window,
the message goes out now rather than being held.

## What happens while it waits

The message is a complete row in the [email log](/docs/email-sending/email-log) from the moment it is accepted, with
status `scheduled`. Everything about it is already decided:

- Recipients were checked against the [suppression list](/docs/contacts/suppression-list) at accept time, not at send
  time.
- A template was **rendered at accept time**. Editing the template between now and then changes nothing about this
  message.
- The quota was spent when the message was accepted.

{{< callout type="warning" title="Sender verification is checked twice" >}}
The `from` domain is checked when the message is accepted **and again at delivery**. A domain that was verified when
you scheduled the message and is not when it goes out will fail the send.

That matters for anything scheduled far ahead: removing a domain quietly kills the mail that was already queued
against it.
{{< /callout >}}

## There is no cancel

{{< callout type="warning" title="A scheduled message cannot be called back" >}}
The API has no route to cancel or reschedule one. Once accepted, it will be sent at the named time.

Data erasure will not remove it either — the erasure endpoints deliberately skip rows that are queued, scheduled or in
flight, so there is no way through that door.

Schedule close to the send, or keep the decision on your side and call `/emails/send` when the moment arrives. For
anything you may need to stop, a [campaign](/docs/campaigns/sending) is the right shape instead — those can be paused
and cancelled.
{{< /callout >}}

## How the worker picks it up

`scheduled` is not a separate queue. The row sits with its `next_attempt_at` set to your `send_at`, and the ordinary
claim query takes it as soon as that time has passed. So a scheduled message is delivered by the same path, with the
same retries, as an immediate one.

The consequence worth knowing: precision is bounded by how often a worker looks. A message scheduled for 09:00 leaves at
09:00 or shortly after, never before.

## Watching one

Do not poll a `scheduled` message. It is waiting on purpose, possibly for days, and
[the status route](/docs/email-sending/email-status) will keep saying so. Poll from the point it becomes `queued`, or
let a [webhook](/docs/webhooks/overview) tell you.
