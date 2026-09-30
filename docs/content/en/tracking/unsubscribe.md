---
title: "Unsubscribe & One-Click"
description: "Hosted unsubscribe pages, RFC 8058 one-click unsubscribe, and List-Unsubscribe headers"
weight: 20
---

Every message Mailyard sends can carry a `List-Unsubscribe` header. There are two ways to put one there, and which you
want depends on who owns the opt-out:

- **Mailyard-managed** — you reference an unsubscribe list, Mailyard mints a signed one-click URL, hosts the page, and
  records the opt-out on its own suppression list.
- **Caller-managed** — your application supplies its own URL, Mailyard carries the header and nothing else. Suitable
  when the application already runs its own preference centre and knows who its recipients are.

Campaigns are Mailyard-managed, unless the campaign turns its unsubscribe off (`unsubscribe_disabled`), in which
case its mail carries neither header and no link.

## Why it matters even for a non-marketing send

Gmail and Yahoo require a working `List-Unsubscribe` on bulk mail. Neither bounces a message that lacks one, they
**filter** it, so the sender sees a clean delivery report and the recipient sees nothing. An application relaying a
hundred thousand notifications is bulk mail by their definition regardless of what it is about.

## Mailyard-managed opt-out

### Hosted unsubscribe endpoints

```
GET  /tracking/unsubscribe/{token}
POST /tracking/unsubscribe/{token}
```

Both campaign and transactional opt-outs land here. They are **public — no authentication**: the URL carries an
HMAC-signed token instead of an `Authorization`
header, because the caller is a recipient or their mailbox provider.

- `GET` renders a small page with a confirm button. It never shows the address: the link is unauthenticated, and a
  leaked one should not name a mailbox.
- `POST` performs the opt-out. This is also the RFC 8058 one-click target, so it is idempotent and safe for a mailbox
  provider to call unattended.

The token encodes what is being unsubscribed, and the two kinds cannot be swapped for each other:

| Kind          | Payload                                               | Effect of a confirmed opt-out                                                                                                |
|---------------|-------------------------------------------------------|------------------------------------------------------------------------------------------------------------------------------|
| Campaign      | the campaign message id                               | Records an opt-out from that campaign's list, stamps the message, and by default sets the subscriber's status to `unsubscribed` |
| Transactional | the unsubscribe list id **and** the recipient address | Writes a suppression for that one address, scoped to that one list                                                           |

Neither token expires. An unsubscribe link in a message already delivered has to keep working.

### What a campaign unsubscribe does

A person pressing Unsubscribe in their mail client is saying stop, not "stop this one list". So the one-click `POST`
a mailbox provider sends is **global for campaigns**: the subscriber's status becomes `unsubscribed`, every campaign of
the project skips them from then on, the list records the opt-out (`opted_out_at` on the member, and on
`GET /api/v1/subscriber-lists/{id}/opt-outs`), and the campaign message is stamped `unsubscribed_at`, which is what
`engagement.unsubscribed` on the campaign counts.

The hosted page offers a choice instead. Its first button, **Unsubscribe from <list>**, records the per-list opt-out
only and leaves the status alone, for somebody who subscribed to several lists on purpose and wants out of one. The
second, **Unsubscribe from all newsletters**, does what one-click does. Any `POST` that does not carry the page's own
`scope=list` field is treated as one-click, because a mailbox provider sends `List-Unsubscribe=One-Click` or nothing
at all.

A campaign unsubscribe writes **no suppression**. Suppressions block transactional mail as well, and a person who left
a newsletter still expects their receipts and password resets. Their status is lifted where it lives: the status on
`PATCH /api/v1/subscribers/{id}` (or the console's subscriber page), or `POST /api/v1/subscriber-lists/subscribe`,
which sets `subscribed` and clears that list's opt-out in one call. `POST /api/v1/subscriber-lists/{id}/resubscribe`
lifts the list opt-out only.

An opt-out fires **no webhook**. A transactional opt-out is read from `GET /api/v1/suppressions` - each row carries
its `unsubscribe_list_id`, so a scoped opt-out is distinguishable from a global block. A campaign opt-out is read from
the subscriber (`status`), the list (`opted_out_count`, `/members`, `/opt-outs`) and the campaign
(`engagement.unsubscribed`, `unsubscribed_at` on each message). There is no `email.unsubscribed` event to subscribe
to; see [Event Types](/docs/webhooks/event-types) for the seven that do exist.

### Scoping a send to a list

```json
{
    "from": "news@example.com",
    "to": [
        "jane@example.com"
    ],
    "subject": "Weekly digest",
    "html": "<p>...</p>",
    "unsubscribe_list_id": "0f7c1b2e-2f1a-4a5e-9c3d-7b1f0a2d5e64"
}
```

Mailyard then mints the signed URL, emits both headers with one-click enabled, and resolves
`{{ mailyard_unsubscribe_url }}` in the body to the same link.

A scoped send is limited to **one recipient**. The token binds a single address, so there is no correct link to put in a
message addressed to several people, and Mailyard refuses the send rather than mint one that would opt out the wrong
person.

Recipients already opted out of that list are dropped from the send, in addition to the project's global suppressions.

## Caller-managed opt-out

Supply the targets directly and Mailyard emits them verbatim. What happens when the recipient acts on them is entirely
your application's business — Mailyard neither receives the click nor records anything.

```json
{
    "from": "notifications@example.com",
    "to": [
        "jane@example.com"
    ],
    "subject": "Your weekly summary",
    "html": "<p>...</p>",
    "list_unsubscribe_url": "https://app.example.com/prefs/u/eyJ1IjoxN30",
    "list_unsubscribe_mailto": "unsubscribe@example.com",
    "list_unsubscribe_post": true
}
```

| Field                     | Notes                                                                                                                                                             |
|---------------------------|-------------------------------------------------------------------------------------------------------------------------------------------------------------------|
| `list_unsubscribe_url`    | An `http` or `https` endpoint. Emitted as the second URI.                                                                                                         |
| `list_unsubscribe_mailto` | A `mailto:` URI. A bare address is accepted and completed. Emitted first.                                                                                         |
| `list_unsubscribe_post`   | Adds `List-Unsubscribe-Post`, which invites the provider to opt the recipient out with a single unattended `POST` to `list_unsubscribe_url`. Requires that field. |

Available on `POST /api/v1/emails/send`, `POST /api/v1/emails/send-template`, and per item on
`POST /api/v1/emails/batch` — per item because the link identifies the recipient, and one link shared across a batch
would opt out nobody in particular.

{{< callout type="warning" title="Only set one-click if you mean it" >}}
`list_unsubscribe_post` is a promise that an unauthenticated `POST` to that URL unsubscribes the recipient. A provider
will make that request without asking. If the endpoint answers with a login page or a `405`, the message looks worse
than it would have with no header at all.
{{< /callout >}}

### Not combinable with a managed list

`unsubscribe_list_id` and `list_unsubscribe_url` on the same send are refused with a
`400`. They are opposite arrangements: the list means Mailyard filters the send and mints the link, so a header pointing
somewhere else would leave recipients who used it still receiving the mail, because their opt-out never reached the list
doing the filtering.

## Over the SMTP relay

An application relaying through Mailyard composes an ordinary RFC 2369 header, and that is what the relay honors:

```
List-Unsubscribe: <mailto:unsubscribe@example.com>, <https://app.example.com/prefs/u/abc>
List-Unsubscribe-Post: List-Unsubscribe=One-Click
```

Both headers are lifted off the submission and re-emitted by Mailyard's own message builder, which is what keeps a
single, validated copy on the wire. A URI in a scheme Mailyard cannot use is dropped rather than treated as an error.

The relay has no equivalent of `unsubscribe_list_id`. A submitted message is always caller-managed.

## On the wire

Mailyard emits the mailto first, then the URL, matching the order most clients expect:

```
List-Unsubscribe: <mailto:unsubscribe@example.com>, <https://app.example.com/prefs/u/abc>
List-Unsubscribe-Post: List-Unsubscribe=One-Click
```

`List-Unsubscribe` and `List-Unsubscribe-Post` are reserved in the `headers` map on a send. Setting them there is
rejected with a `400` pointing at the fields above — the message builder owns these headers so there is exactly one of
each, from one place that has validated it.

## Relationship to the unsubscribe URL system variable

`{{ mailyard_unsubscribe_url }}` resolves to the hosted endpoint for the send type: the campaign unsubscribe page for
campaigns, the one-click endpoint for a send scoped to an unsubscribe list. It is only substituted once the message
identity is known, so it renders as its own name in template previews. It has no value on a caller-managed send, where
Mailyard does not know the link. See
[System Variables](/docs/templates/system-variables).

## Unsubscribe lists and suppression

Mailyard-managed unsubscribe lists are project-scoped resources you manage via the API:

```
POST   /api/v1/unsubscribe-lists
GET    /api/v1/unsubscribe-lists
GET    /api/v1/unsubscribe-lists/{id}
PATCH  /api/v1/unsubscribe-lists/{id}
DELETE /api/v1/unsubscribe-lists/{id}
```

A list has a `name`, an optional `public_name` and `description`, and an `active` flag. Referencing one by
`unsubscribe_list_id` on a send scopes a one-click opt-out to it.

When a recipient unsubscribes from a send scoped to one of these lists, they land on your **suppression list** so
future sends under that scope skip them. See [Suppression List](/docs/contacts/suppression-list) for managing
suppressions and [Contact Management](/docs/contacts/contact-management) for how suppression interacts with contacts.
A campaign unsubscribe is the other mechanism and never reaches the suppression list - see above.

In the console, the Send email page offers the unsubscribe list as a picker. The caller-managed fields are API
only: an application with its own opt-out endpoint is not sending from a browser.
