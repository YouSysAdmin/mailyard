---
title: "Localization"
description: "Multi-language template support"
weight: 40
---

A localization is the renderable content of one version in one language: a subject, and an HTML or text body. It is the
only level that holds content, so even a template that will never be translated has exactly one.

Localizations belong to a **version**, not to the template. Opening a new version and translating it does not disturb
the language content that is live.

## Write one

```
PUT /api/v1/templates/{templateId}/versions/{versionId}/localizations
```

There is no separate create and update. A localization is identified by its language within the version, so sending the
same language again replaces its content and answers `200` either way.

```bash
curl -X PUT http://localhost:3000/api/v1/templates/$TPL/versions/$VER/localizations \
  -H "Authorization: Bearer myk_..." \
  -H "Content-Type: application/json" \
  -d '{
    "language": "fr",
    "subject": "Bienvenue, {{ name }}",
    "html": "<h1>Bienvenue, {{ name }}</h1><p>Merci de nous avoir rejoints.</p>",
    "text": "Bienvenue, {{ name }} - merci de nous avoir rejoints."
  }'
```

`language` and `subject` are required. `html` and `text` are each optional, but a version whose localization has neither
renders an empty message.

The `language` value is trimmed and lowercased on the way in, so `FR ` and `fr` are the same localization. It is stored
as a plain string rather than a reference — the [language registry](/docs/templates/languages) drives the console's
picker and nothing else, so writing a code that is not registered works and simply will not appear in that list.

## List

```
GET /api/v1/templates/{templateId}/versions/{versionId}/localizations
```

## Delete

```
DELETE /api/v1/templates/{templateId}/localizations/{localizationId}
```

Note the path: deletion addresses the localization by its own id, under the template rather than under the version.

## Choosing one at send time

```bash
curl -X POST http://localhost:3000/api/v1/emails/send-template \
  -H "Authorization: Bearer myk_..." \
  -H "Content-Type: application/json" \
  -d '{
    "from": "hello@example.com",
    "to": ["user@example.fr"],
    "template_name": "welcome",
    "language": "fr",
    "data": {"name": "Marie"}
  }'
```

Resolution walks four steps against the **active** version and takes the first localization that exists:

1. The `language` on the request
2. The template's `default_language`
3. `en`
4. Whichever localization the version lists first

Steps 3 and 4 are there so a version with content still sends rather than failing, not as behaviour to plan around. A
version with no localizations at all is refused with `template "x" has no localizations`.

## One language per recipient in a batch

A batch names the template once and lets each item pick its own language, which is how a single call sends a
notification to a mixed-locale audience:

```json
{
  "from": "hello@example.com",
  "template_name": "newsletter",
  "language": "en",
  "items": [
    { "to": ["bob@example.com"],   "data": {"name": "Bob"} },
    { "to": ["marie@example.fr"],  "language": "fr", "data": {"name": "Marie"} },
    { "to": ["hans@example.de"],   "language": "de", "data": {"name": "Hans"} }
  ]
}
```

The top-level `language` is the default for items that name none. Each item resolves independently through the four
steps above, so an item asking for a language nobody translated still goes out.

## Preview one

```
POST /api/v1/templates/{templateId}/versions/{versionId}/preview
```

```json
{ "language": "fr", "data": {"name": "Marie"} }
```

Renders that version through the same resolution and the same stylesheet inlining a send would use, and returns the
result instead of queueing it. See [Preview & Test](/docs/templates/preview-and-test).
