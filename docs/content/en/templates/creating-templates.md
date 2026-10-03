---
title: "Creating Templates"
description: "Create and manage email templates"
weight: 20
---

## Create

```
POST /api/v1/templates
```

Only `name` is required, and it must be unique within the project without regard to case — `Welcome` beside `welcome`
is refused with `409`, and a send naming `WELCOME` finds `welcome`.

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

The template language is Go's own ([text/template](https://pkg.go.dev/text/template), with
[html/template](https://pkg.go.dev/html/template)'s escaping on the HTML part), so everything it has works here:
`if` / `else if` / `else`, `range` with `$index, $value`, `break` and `continue`, `with`, variables, pipelines,
`define` / `template` / `block`, and the builtin functions (`eq`, `ne`, `lt`, `le`, `gt`, `ge`, `and`, `or`, `not`,
`len`, `index`, `slice`, `print`, `printf`, `println`, `html`, `js`, `urlquery`).

The dot is optional wherever a name means data. Any name that is not one of the functions above is read as a field,
in any position - `{{ slice name 0 1 }}`, `{{ if gt (len items) 1 }}`, `{{ user.first }}`. `{{ .name }}` works too and
is left exactly as written.

```html
{{ if premium }}<p>Your priority support line: 555-0100</p>{{ end }}

<ul>
{{ range $i, $item := items }}
  {{ if ge $i 5 }}{{ break }}{{ end }}
  <li>{{ $item.name }} - {{ printf "%.2f" $item.price }}</li>
{{ else }}
  <li>Nothing in this order</li>
{{ end }}
</ul>
```

{{< callout type="note" title="Numbers from JSON compare as numbers" >}}
A JSON number has no integer type, and Go's comparisons refuse to compare a fraction with a whole number. Mailyard
compares any two numbers by value, so `{{ if gt count 2 }}` and `{{ if lt price 9.99 }}` both work. A whole number
in the data is an integer, which is what `range`, `index`, `slice` and `%d` need, and `printf "%.2f"` still prints it
as `10.00`.
{{< /callout >}}

What is NOT there is a library of extra functions: no `upper`, `default` or date formatting. Format a value before
sending it.

{{< callout type="tip" title="Whitespace trimming survives" >}}
`{{- name -}}` keeps its trim markers, which is how you stop a control structure leaving blank lines through the middle
of a plain-text part.
{{< /callout >}}

A key the send does not supply is an **error** rather than a blank — the request fails with `template render failed` and
nothing is queued. Campaign sends are the exception and render a missing key as empty, because subscriber custom fields
are uneven by nature.

## List

```
GET /api/v1/templates
```

Returns every template in the project by name, with `total`. `q` narrows it to names containing the term, and
`limit` with `offset` asks for a page of at most 200. Without `limit` the whole list comes back.

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

Renaming onto a name another template holds is refused with `409`. A `name` that is only whitespace is refused with
`400` rather than ignored, and `description` is trimmed. The update writes only the fields the body names, so it never
puts back an `active_version_id` or a field another request changed meanwhile.

## Delete

```
DELETE /api/v1/templates/{id}
```

Returns `204`, and takes the versions, localizations and attachments with it. A template that a campaign still renders -
one in `draft`, `scheduled`, `sending` or `paused`, directly or as an A/B variant - is refused with `409` naming the
campaign. Finish or cancel the campaign, or point it at another template, first.

Mail already queued from the template is not affected. Messages reference the template's attachments, and deleting an
attachment or the whole template keeps the stored file until no message references it, so a queued message still
delivers it and a sent one can still be downloaded.

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

`from` is required and its domain must be [verified by this project](/docs/smtp-domains/domain-verification). Address
the
template by `template_name` or by `template_id` — one of the two. Everything a
[plain send](/docs/email-sending/single-email) accepts is accepted here as well: `headers`, `attachments`, `send_at`,
`dry_run`, the routing selectors and the sandbox controls.

The rendered result is **frozen into the email row**. Editing the template afterwards changes what the next send
produces and never changes what the log says was delivered.
