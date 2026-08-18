---
title: "Stylesheets"
description: "Manage CSS stylesheets for email templates"
weight: 60
---

A stylesheet is a named CSS block a [template version](/docs/templates/versioning) can point at. Keeping it separate
from the markup means one house style can back every template you have, and correcting a button colour is one edit
rather than one per localization.

## Why it is inlined rather than linked

Mail clients are not browsers. Gmail's web client strips a `<style>` block, Outlook does the same, and nothing loads an
external stylesheet at all. So at render time the rules are resolved against the document and written onto the elements
they matched, as `style` attributes — which is the only form of CSS that survives the trip reliably.

Two things follow from that:

- **Class names are kept.** Inlining does not strip them, so the delivered markup stays readable and anything that has
  to find an element by class still can.
- **Selectors that cannot be resolved statically do nothing.** A rule keyed on `:hover`, a media query, or a
  pseudo-element has no element to write itself onto, so it stays in the block and reaches only the clients that read
  one. Write the layout with attributes and inline-able rules, and treat the rest as progressive enhancement.

If inlining fails outright the message still goes out with the `<style>` block intact. That is worse styling in some
clients, which beats a failed send on a path where refusing costs somebody their mail.

## Create

```
POST /api/v1/stylesheets
```

`name` is required, `css` is optional and capped at 256 KB.

```bash
curl -X POST http://localhost:3000/api/v1/stylesheets \
  -H "Authorization: Bearer myk_..." \
  -H "Content-Type: application/json" \
  -d '{
    "name": "house",
    "css": "h1 { font: 600 22px/1.3 Helvetica, Arial, sans-serif; color: #1a1a1a } .btn { display: inline-block; padding: 12px 24px; background: #6d28d9; color: #fff; border-radius: 6px; text-decoration: none }"
  }'
```

## List

```
GET /api/v1/stylesheets?limit=20
```

## Read one

```
GET /api/v1/stylesheets/{id}
```

## Replace

```
PUT /api/v1/stylesheets/{id}
```

A full replace, not a patch — `name` is required on every call, and omitting `css` empties it.

## Delete

```
DELETE /api/v1/stylesheets/{id}
```

Returns `204`. A version still pointing at the deleted sheet is **not** broken by this: it renders unstyled rather than
failing, on the same reasoning as the inlining fallback above. Watch for it in a preview, since nothing raises an error.

## Attaching one

A stylesheet applies through the version, not the template:

```json
PATCH /api/v1/templates/{templateId}/versions/{versionId}
{ "stylesheet_id": "0198f6a1-3c7e-7b21-9f4d-2a5c8e0b1d33" }
```

That is deliberate — restyling is a content change like any other, so it goes through a draft version you can preview
and activate rather than altering live mail the moment you save the CSS.

{{< callout type="tip" title="Export carries the CSS with it" >}}
`GET /api/v1/templates/{id}/export` inlines the referenced stylesheet into the document instead of naming it, so a
template imported into another project arrives styled. See [Import/Export](/docs/templates/import-export).
{{< /callout >}}
