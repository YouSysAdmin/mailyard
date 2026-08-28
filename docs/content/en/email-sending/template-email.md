---
title: "Template Email"
description: "Send emails using pre-defined templates"
weight: 20
---

Send a stored [template](/docs/templates/overview) instead of a body. Mailyard resolves the template's active version,
picks a localization, renders it against the data you pass, and sends the result.

```
POST /api/v1/emails/send-template
```

```bash
curl -X POST http://localhost:3000/api/v1/emails/send-template \
  -H "Authorization: Bearer myk_..." \
  -H "Content-Type: application/json" \
  -d '{
    "from": "noreply@example.com",
    "to": ["user@example.com"],
    "template_name": "welcome",
    "language": "en",
    "data": {
      "name": "Alice",
      "activation_url": "https://example.com/activate?token=abc123"
    }
  }'
```

{{< callout type="tip" title="Machine API schema" >}}
The `/api/v1` surface describes itself: run
[`mailyard export-api-spec`](/docs/security/api-keys#openapi-description) and feed the document to Swagger UI, Postman,
or a client generator.
{{< /callout >}}

## The body

`from` and `to` are required, as on any send, and the `from` domain must be
[verified by this project](/docs/smtp-domains/domain-verification). `reply_to` is optional and is not: it names where
an answer goes, not who sent the mail.

Address the template with **`template_name` or `template_id`** — one of the two. The name is unique within the project
and is usually the better choice: it survives an export and import into another install, where the id does not.

The values go in **`data`**. That is the field name on the wire.

Everything a [plain send](/docs/email-sending/single-email) accepts is accepted here too: `headers`, `attachments`,
`send_at`, `dry_run`, `disable_tracking`, `unsubscribe_list_id`, the caller-supplied `list_unsubscribe_*` targets, the
`smtp_group` and `smtp_server_id` routing selectors, and the sandbox controls.

## Variables

Write them with double braces. The leading dot Go templates normally require is optional:

```html
<h1>Welcome, {{ name }}</h1>
<p>Click <a href="{{ activation_url }}">here</a> to activate your account.</p>
```

```json
{ "name": "Alice", "activation_url": "https://example.com/activate?token=abc123" }
```

{{< callout type="warning" title="A missing key fails the send" >}}
Rendering is strict here. A variable the template references and `data` does not supply refuses the request with
`template render failed: ... map has no entry for key "activation_url"`, and nothing is queued.

That is deliberate. A blank where a name or an activation link belongs is mail that goes out wrong and never reports
it. Campaigns are the one place this is relaxed, because subscriber custom fields are uneven by nature.
{{< /callout >}}

See [Creating Templates](/docs/templates/creating-templates#writing-the-content) for conditionals, loops and the rules
about what is left untouched.

The reserved `{{ mailyard_* }}` names work here as they do in a campaign — a hosted "view in browser" link, and a
one-click opt-out when the send names an `unsubscribe_list_id`. You do not supply them in `data`, and a name you cannot
resolve is removed rather than shipped. See [System Variables](/docs/templates/system-variables).

## Which language goes out

Resolution walks four steps against the active version and takes the first localization that exists:

1. The `language` on the request
2. The template's `default_language`
3. `en`
4. Whichever localization the version lists first

The last two are safety nets so a version with content still sends. A version with **no** localizations is refused with
`template "welcome" has no localizations`, and a template with no active version with `has no active version`.

## What gets stored

The rendered subject and body are frozen into the email row. Editing the template afterwards changes the next send and
never changes what the log says was delivered — so the [email log](/docs/email-sending/email-log) remains an accurate
record of what each recipient actually received.

The template's [attachments](/docs/templates/overview#attachments-belong-to-the-template) are added automatically, on
top of any you pass in the request.

## Rehearse it first

```bash
curl -X POST http://localhost:3000/api/v1/emails/preview \
  -H "Authorization: Bearer myk_..." \
  -H "Content-Type: application/json" \
  -d '{"template_name": "welcome", "language": "en", "data": {"name": "Alice"}}'
```

The preview resolves the same active version and renders **just as strictly**, so if it comes back with output the send
will not fail on a missing key. It queues nothing. See [Preview & Test](/docs/templates/preview-and-test) for the two
template-level preview routes, which behave differently on purpose.
