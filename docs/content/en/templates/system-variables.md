---
title: "System Variables"
description: "Built-in template variables for unsubscribe and web-view links"
weight: 90
---

The `{{ mailyard_* }}` variable namespace is reserved for links Mailyard has to mint itself, because they identify a
particular message and a particular recipient — neither of which exists yet at the moment a template is rendered.

## The names

| Variable | Resolves to |
|---|---|
| `{{ mailyard_web_view_url }}` | A signed link to read this message on the web |
| `{{ mailyard_mail_web_link }}` | The same link, under an older name kept working |
| `{{ mailyard_unsubscribe_url }}` | The RFC 8058 one-click opt-out for this recipient |

Using one is opt-in. A variable that no template references costs nothing and produces nothing.

## Where they work

Anywhere a **template** is rendered: [campaigns](/docs/campaigns/overview),
[`/emails/send-template`](/docs/email-sending/template-email), template-mode
[batches](/docs/email-sending/batch-email), and a template test send.

{{< callout type="info" title="A plain send does not render anything" >}}
`POST /api/v1/emails/send` takes your HTML as given — there is no template step, so `{{ mailyard_web_view_url }}` in
that body is not a variable, it is text, and it reaches the recipient as text. Use a template if you want these.
{{< /callout >}}

## What each one needs

A variable that cannot be resolved is **removed**, not left in place. So a message never ships a half-built link — it
ships without one, and the surrounding markup is what a reader sees.

**`mailyard_web_view_url`** needs `server.public_url` and `auth.jwt_secret`. With those set it resolves on every
templated send. Without them there is no origin to build an absolute signed link from, and it comes out empty.

**`mailyard_unsubscribe_url`** needs something to unsubscribe *from*, and where that comes from depends on the send:

| Send | Resolves when |
|---|---|
| Campaign | Always — the link is bound to the campaign's list and this recipient |
| `send-template` | `unsubscribe_list_id` names an [opt-out scope](/docs/contacts/unsubscribe-lists), and there is exactly **one** recipient |
| Batch | Never — a batch has no opt-out scope, so use `list_unsubscribe_url` per item instead |

The one-recipient rule is not an implementation limit. A one-click link identifies a person, so on a message addressed
to three people there is no correct link to embed — and minting one would unsubscribe whoever clicked it on behalf of
somebody else. A scoped send is refused outright if it names more than one recipient.

```bash
curl -X POST http://localhost:3000/api/v1/emails/send-template \
  -H "Authorization: Bearer myk_..." \
  -H "Content-Type: application/json" \
  -d '{
    "from": "dispatch@example.com",
    "to": ["jane@customer.example"],
    "template_name": "shipping-notice",
    "unsubscribe_list_id": "0198f6a1-3c7e-7b21-9f4d-2a5c8e0b1d33",
    "data": {"order": "4471"}
  }'
```

## Why it takes two passes

These URLs identify a **particular message**, and a template is rendered before any particular message exists — most
obviously in a campaign, which renders once and sends the result to everybody, but it is true of a single send too,
because the id is minted after the content is built.

So the render substitutes a stable placeholder, and a second pass — once the message has an id — swaps that placeholder
for the real URL.

The placeholders are root-relative paths, which is what lets them survive the three things that happen to the markup in
between: Go's URL filter passes them instead of writing `ZgotmplZ`, the CSS inliner leaves them alone, and the
click-tracking rewriter only touches `http(s)` hrefs, so it skips them.

Two consequences worth knowing:

- **Your data cannot shadow them.** The reserved names are written after your `template_data`, so a key of your own
  called `mailyard_unsubscribe_url` is overwritten rather than honoured.
- **An unresolvable placeholder is removed, not left.** It becomes an empty string, so a send with no opt-out scope, or
  an install with no public URL, ships `href=""` rather than a link to `/__mailyard_unsubscribe__` — which a mail client
  would resolve against nothing and show as a dead link.

The same removal happens on anything that renders a template without sending it: a preview, a `dry_run`, and a
[sandbox](/docs/email-sending/sandbox) capture. None of them produces a message, so there is no id to bind to.

## View in browser

```html
<a href="{{ mailyard_web_view_url }}">View this email in your browser</a>
```

It works in the text part too:

```
View online: {{ mailyard_web_view_url }}
```

The link is an HMAC-signed capability bound to the message id and **expires after 90 days**. The page serves the exact
HTML that was sent, from the origin in `MAILYARD_SERVER_PUBLIC_URL`, under a CSP that sandboxes the document and
forbids scripts, frames, and form submission. Images and inline styles stay on, because they are what the email is and
the reader chose to open it.

An expired link answers `410` with "This message is no longer available online", a malformed one `404`.

{{< callout type="info" title="Two things the hosted page does not do" >}}
`cid:` inline attachments do not resolve there — the CSP admits `data:`, `http:` and `https:` only, so a message whose
images are embedded parts shows gaps. Use `https` or `data:` URLs if the web view matters to you.

And opening the page **does** count as an open. It serves the stored body, tracking pixel included, so a reader who
clicks through registers the same event a mail client would have.
{{< /callout >}}

## Unsubscribe

```html
<a href="{{ mailyard_unsubscribe_url }}">Unsubscribe</a>
```

The link opts the recipient out of one scope: the campaign's list, or the `unsubscribe_list_id` the send named. It is
the same signed endpoint Mailyard puts in the `List-Unsubscribe` header, so the mailbox provider's own Unsubscribe
button and your link in the footer do the same thing and stay in step.

On a scoped transactional send, Mailyard also stamps that header for you when you have not set one — so a receipt sent
under a "shipping notices" scope carries a working one-click opt-out whether or not the template has a footer link.

## In a preview

A preview has no message, so there is nothing to bind a link to and the variables come out empty. What a preview can
tell you is where the link sits, not whether it works. To see a resolved URL, send a test to a real mailbox.
