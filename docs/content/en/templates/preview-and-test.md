---
title: "Preview & Test"
description: "Preview templates and send test emails"
weight: 70
---

There are three preview routes and one test send. They differ in what they render, and picking the wrong one is how you
end up previewing something the send path would never produce.

| Route | Renders | Use it for |
|---|---|---|
| `POST /api/v1/templates/preview` | Content in the request body | The editor, while somebody types |
| `POST /api/v1/templates/{id}/versions/{versionId}/preview` | A stored version, in one language | Checking a draft before activating |
| `POST /api/v1/emails/preview` | What a send would produce, from the **active** version | Confirming what your integration will actually mail |

They also differ in how they treat missing data, and that difference is deliberate:

- The two **template** routes render leniently. A variable your data does not supply comes out empty, so an author
  previewing half-written copy sees the layout instead of an error.
- `POST /api/v1/emails/preview` renders **strictly**, exactly as a transactional send does — a missing key fails the
  request. That is what makes it the honest rehearsal: if the preview refuses, the send would have refused too.

## Ad-hoc content

```
POST /api/v1/templates/preview
```

Nothing is read from or written to the store, so this works before a template exists.

```bash
curl -X POST http://localhost:3000/api/v1/templates/preview \
  -H "Authorization: Bearer myk_..." \
  -H "Content-Type: application/json" \
  -d '{
    "subject": "Welcome, {{ name }}",
    "html": "<h1>Welcome, {{ name }}</h1><p><a href=\"{{ activation_url }}\">Activate</a></p>",
    "text": "Welcome, {{ name }} - activate at {{ activation_url }}",
    "css": "h1 { color: #1a1a1a }",
    "data": {
      "name": "Alice",
      "activation_url": "https://example.com/activate?token=abc"
    }
  }'
```

`subject` is required, everything else is optional. Passing `css` runs the same inlining a send would, which is the
point of having it here — you see the `style` attributes the recipient will get.

```json
{
  "preview": {
    "subject": "Welcome, Alice",
    "html": "<h1 style=\"color:#1a1a1a\">Welcome, Alice</h1><p><a href=\"https://example.com/activate?token=abc\">Activate</a></p>",
    "text": "Welcome, Alice - activate at https://example.com/activate?token=abc"
  }
}
```

## A stored version

```
POST /api/v1/templates/{templateId}/versions/{versionId}/preview
```

```json
{ "language": "fr", "data": {"name": "Marie"} }
```

Both fields are optional. Omit `language` and it resolves through the
[usual four steps](/docs/templates/localization#choosing-one-at-send-time). Omit `data` and it falls back to the
**version's** sample data, then to the template's — so a template with sample data set previews sensibly with an empty
body.

The response names which localization it actually rendered, which matters when you asked for a language that does not
exist yet:

```json
{ "preview": { "subject": "...", "html": "...", "text": "..." }, "language": "fr" }
```

This route is how you check a draft. It reads the version you name, not the active one.

## What a send would produce

```
POST /api/v1/emails/preview
```

```bash
curl -X POST http://localhost:3000/api/v1/emails/preview \
  -H "Authorization: Bearer myk_..." \
  -H "Content-Type: application/json" \
  -d '{"template_name": "welcome", "language": "en", "data": {"name": "Alice"}}'
```

Addresses the template the way a send does — by name or id, resolving the **active** version, refusing a missing data
key — so this is the one to call from an integration test. It renders and returns, and queues nothing. The response
names the template it resolved:

```json
{ "template": "welcome", "preview": { "subject": "...", "html": "...", "text": "..." } }
```

{{< callout type="warning" title="A preview is not the delivered message" >}}
None of these apply tracking. There is no open pixel and links are not rewritten, because both are per-message and no
message exists. [System variables](/docs/templates/system-variables) render empty for the same reason.

A preview also skips everything the send path does around rendering: sender verification, suppression filtering, quota,
and the template's attachments. Use `send-test` when you need to see those.
{{< /callout >}}

## Test send

```
POST /api/v1/templates/{templateId}/send-test
```

```bash
curl -X POST http://localhost:3000/api/v1/templates/$TPL/send-test \
  -H "Authorization: Bearer myk_..." \
  -H "Content-Type: application/json" \
  -d '{
    "from": "hello@example.com",
    "to": ["you@example.com"],
    "language": "en",
    "data": {"name": "Test User"}
  }'
```

`from` is required and its domain must be verified by the project, exactly as on a real send. `to` accepts up to **five**
addresses.

This is a genuine send. It renders the active version, attaches the template's files, checks suppressions and quota, and
puts a row in the email log like any other message:

```json
{
  "email": { "id": "0198f6a1-3c7e-7b21-9f4d-2a5c8e0b1d33", "status": "queued" },
  "suppressed_recipients": []
}
```

An address on the project's suppression list comes back in `suppressed_recipients` and is not mailed — worth checking
when a test to your own address appears to vanish.

{{< callout type="tip" title="Send one before activating" >}}
A preview renders in a browser and a mail client is not a browser. Outlook ignores rules a preview honours, Gmail
clips a long message, and dark mode inverts colours nothing declared. The five-address limit exists because this step
belongs in the release, not in a loop.
{{< /callout >}}
