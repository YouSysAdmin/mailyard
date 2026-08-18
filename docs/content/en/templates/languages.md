---
title: "Languages"
description: "Manage the languages used for template localizations"
weight: 50
---

The language registry is a per-project list of locale codes. It exists so the console can offer a fixed set of choices
when you localize a template, rather than leaving every language a free-text field where `pt-BR`, `pt_br` and `ptbr`
become three different locales by accident.

{{< callout type="warning" title="The registry does not decide what a send resolves" >}}
Which localization a send picks is decided by the **template's** `default_language` field, not by this registry. Marking
a language default here only preselects it in the console when you create a template — the console labels it
**Fallback** for that reason. See [Overview](/docs/templates/overview#resolving-a-language) for the resolution order that
actually runs.
{{< /callout >}}

## The record

| Field | Type | Notes |
|---|---|---|
| `id` | string | UUID, minted on create |
| `code` | string | The locale code a localization references, 2-10 characters, lowercased on write |
| `name` | string | What a person reads in the picker, up to 100 characters |
| `is_default` | boolean | At most one per project — setting it clears whichever language held it |
| `created_at` | timestamp | |

Every route is project-scoped, so a session request needs the `X-Mailyard-Project-Id` header. An API key carries its
project already and needs nothing extra.

## Create

```
POST /api/v1/languages
```

```bash
curl -X POST http://localhost:3000/api/v1/languages \
  -H "Authorization: Bearer myk_..." \
  -H "Content-Type: application/json" \
  -d '{"code": "fr", "name": "French", "is_default": false}'
```

`code` and `name` are both required. The response is `201` with the stored record:

```json
{
  "language": {
    "id": "0198f6a1-3c7e-7b21-9f4d-2a5c8e0b1d33",
    "code": "fr",
    "name": "French",
    "is_default": false,
    "created_at": "2026-01-01T00:00:00Z"
  }
}
```

A code already present in the project is refused with `409`.

## List

```
GET /api/v1/languages?limit=20
```

## Replace

```
PUT /api/v1/languages/{id}
```

{{< callout type="warning" title="PUT replaces the whole record" >}}
This is not a partial update. `code` and `name` are required on every call, and **omitting `is_default` sets it to
false** — which silently clears the project's fallback if the language you are editing held it. Send the fields you want
kept, not only the ones you want changed.
{{< /callout >}}

```json
{ "code": "fr", "name": "Francais", "is_default": true }
```

Renaming a code onto one another language already uses is refused with `409`.

## Delete

```
DELETE /api/v1/languages/{id}
```

Returns `204`. This removes the registry entry only. Localizations already written against that code keep their content
and keep sending — they store the code as a string, not a reference. The effect is that the code stops being offered in
the console.

## Next

[Localization](/docs/templates/localization) covers writing the per-language content itself.
