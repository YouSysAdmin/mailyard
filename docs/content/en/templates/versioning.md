---
title: "Versioning"
description: "Manage template versions"
weight: 30
---

A version is a numbered revision of a template. Numbers are assigned server-side as one above the highest the template
has, so they never repeat and never fill a gap left by a deletion.

{{< callout type="warning" title="A version holds no subject and no body" >}}
The renderable content lives one level further down, in the version's **localizations**. A single-language template is
one with a single localization — there is no separate simple case. A version carries only the settings that apply to all
of its languages: which stylesheet the content was written against, and the sample data a preview renders with.
{{< /callout >}}

## List

```
GET /api/v1/templates/{templateId}/versions
```

`GET /api/v1/templates/{templateId}` returns the same list alongside the template, which is usually the call you want.

## Create

```
POST /api/v1/templates/{templateId}/versions
```

Both fields are optional, and an empty body is valid — that is how you start a version whose content you are about to
write:

```json
{
  "stylesheet_id": "0198f6a1-3c7e-7b21-9f4d-2a5c8e0b1d33",
  "sample_data": "{\"name\": \"Alice\"}"
}
```

`sample_data` is a JSON **string**, not an object. The response is `201`:

```json
{
  "version": {
    "id": "0198f6a1-3c80-7c44-b6e1-9d2f7a0c5188",
    "version": 2,
    "created_at": "2026-01-15T00:00:00Z"
  }
}
```

Two clients creating a version at the same instant can land on the same number. One of them is refused with `409` and
the message says to retry — the second attempt reads the new maximum and gets the number after it.

## Update

```
PATCH /api/v1/templates/{templateId}/versions/{versionId}
```

Changes the same two settings. To change what the version renders, edit its
[localizations](/docs/templates/localization).

## Activate

```
POST /api/v1/templates/{templateId}/activate/{versionId}
```

This is the publish step, and it is the only thing that changes what live sends resolve. The response carries the
template with its new `active_version_id`.

## Delete

```
DELETE /api/v1/templates/{templateId}/versions/{versionId}
```

Returns `204`. The active version is refused with `409` — activate another one first, so a template is never left
sendable-by-name but with nothing to send.

## A release, end to end

1. `POST .../versions` to open a draft. The live version keeps sending throughout.
2. `PUT .../localizations` for each language you support.
3. `POST .../versions/{versionId}/preview` with representative data, and read the rendered HTML.
4. `POST /api/v1/templates/{id}/send-test` to a real mailbox, because a preview is a browser and a mail client is not.
5. `POST .../activate/{versionId}`.

Step 5 is atomic and takes effect on the next send. If something is wrong with the new copy, activating the previous
version rolls it back just as fast — the old version was never modified, only overtaken.
