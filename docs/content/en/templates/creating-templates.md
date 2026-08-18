---
title: "Creating Templates"
description: "Create and manage email templates"
weight: 20
---

## Create

```
POST /api/v1/templates
```

Only `name` is required, and it must be unique within the project — a repeat is refused with `409`.

```bash
curl -X POST http://localhost:3000/api/v1/templates \
  -H "Authorization: Bearer myk_..." \
  -H "Content-Type: application/json" \
  -d '{
    "name": "welcome",
    "description": "Sent once, on signup",
    "default_language": "en",
    "sample_data": "{\"name\": \"Alice\"}",
    "subject": "Welcome, {{ name }}",
    "html": "<h1>Welcome, {{ name }}</h1><p>Thanks for joining.</p>",
    "text": "Welcome, {{ name }} - thanks for joining."
  }'
```

{{< callout type="warning" title="`subject` is what makes this one call instead of four" >}}
Passing `subject` creates a first version, writes the body into a localization in the default language, and activates
it — so the template is sendable immediately.

**Leave `subject` out and `html` and `text` are discarded.** You get a bare template with no version, and a send against
it is refused with `template "welcome" has no active version`. There is no error at create time, because a template
without content is a legitimate thing to make before adding versions by hand.
{{< /callout >}}

Two details the shape does not show:

- **`sample_data` is a JSON string, not a JSON object.** It is stored verbatim and handed to the preview as-is, so it
  goes on the wire escaped, as above. An object is refused.
- **`default_language` defaults to `en`** when omitted, and it is that field — not the project
  [language registry](/docs/templates/languages) — that a send falls back to.

The response is `201` with the template:

```json
{
  "template": {
    "id": "0198f6a1-3c7e-7b21-9f4d-2a5c8e0b1d33",
    "name": "welcome",
    "description": "Sent once, on signup",
    "default_language": "en",
    "active_version_id": "0198f6a1-3c80-7c44-b6e1-9d2f7a0c5188",
    "created_at": "2026-01-01T00:00:00Z"
  }
}
```

## Writing the content

Values come from the `data` object on the send. Write them with double braces, and the leading dot Go templates normally
want is optional:

```html
<p>Hello {{ name }}, order {{ order_id }} is on its way.</p>
```

`{{ .name }}` works too and is left exactly as written. So are `$variables`, pipelines and function calls — anything
with an operator or a pipe in it is passed through untouched, on the grounds that whoever wrote it knows the syntax.

Control structures work the same way, with the keyword left alone and its arguments dotted:

```html
{{ if premium }}<p>Your priority support line: 555-0100</p>{{ end }}

<ul>
{{ range items }}
  <li>{{ .name }} - {{ .price }}</li>
{{ end }}
</ul>
```

{{< callout type="tip" title="Whitespace trimming survives" >}}
`{{- name -}}` keeps its trim markers, which is how you stop a control structure leaving blank lines through the middle
of a plain-text part.
{{< /callout >}}

A key the send does not supply is an **error** rather than a blank — the request fails with `template render failed` and
nothing is queued. Campaign sends are the exception and render a missing key as empty, because subscriber custom fields
are uneven by nature.

## List

```
GET /api/v1/templates?limit=20
```

## Read one

```
GET /api/v1/templates/{id}
```

Answers the template **and its full version list** in one response, so picking a version to edit or activate does not
need a second call.

## Update

```
PATCH /api/v1/templates/{id}
```

Partial — send only what changes. This route touches the container, never the content:

```json
{ "name": "welcome-2026", "description": "Rewritten for the new plan tiers" }
```

Renaming onto a name another template holds is refused with `409`.

## Delete

```
DELETE /api/v1/templates/{id}
```

Returns `204`, and takes the versions, localizations and attachments with it.

## Sending one

```bash
curl -X POST http://localhost:3000/api/v1/emails/send-template \
  -H "Authorization: Bearer myk_..." \
  -H "Content-Type: application/json" \
  -d '{
    "from": "hello@example.com",
    "to": ["user@example.com"],
    "template_name": "welcome",
    "data": {"name": "Bob"}
  }'
```

`from` is required and its domain must be [verified by this project](/docs/smtp-domains/domain-verification). Address the
template by `template_name` or by `template_id` — one of the two. Everything a
[plain send](/docs/email-sending/single-email) accepts is accepted here as well: `headers`, `attachments`, `send_at`,
`dry_run`, the routing selectors and the sandbox controls.

The rendered result is **frozen into the email row**. Editing the template afterwards changes what the next send
produces and never changes what the log says was delivered.
