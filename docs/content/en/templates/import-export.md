---
title: "Import & Export"
description: "Move templates between environments"
weight: 80
---

Export templates as JSON and import them into other Mailyard instances. This is useful for migrating templates between
staging and production environments.

## Export a Template

```
GET /api/v1/templates/{templateId}/export
```

Returns the complete template - every version, every localization, and any stylesheet those versions use - as one
self-contained JSON document. The stylesheet is **inlined**
rather than referenced, so the document imports cleanly into another project or another installation that has never seen
it.

```json
{
    "format": "mailyard-template-v1",
    "template": {
        "name": "welcome",
        "description": "Welcome email for new users",
        "default_language": "en",
        "sample_data": "{\"name\": \"Alice\"}"
    },
    "versions": [
        {
            "version": 1,
            "active": true,
            "sample_data": "{\"name\": \"Alice\"}",
            "stylesheet": {
                "name": "Base",
                "css": "body { font-family: sans-serif }"
            },
            "localizations": [
                {
                    "language": "en",
                    "subject": "Welcome, {{name}}!",
                    "html": "<h1>Welcome, {{name}}!</h1>",
                    "text": "Welcome, {{name}}!"
                },
                {
                    "language": "fr",
                    "subject": "Bienvenue, {{name}} !",
                    "html": "<h1>Bienvenue, {{name}} !</h1>",
                    "text": "Bienvenue, {{name}} !"
                }
            ]
        }
    ]
}
```

Note where the content lives: a version holds no subject or body of its own. Every rendered string belongs to a
**localization**, and a single-language template is simply one with a single localization. `sample_data` is a JSON
string, not an object.

## Import a Template

```
POST /api/v1/templates/import
```

Send the exported JSON as the request body. Returns `201 Created` with the new template.

```bash
curl -X POST http://localhost:3000/api/v1/templates/import \
  -H "Authorization: Bearer myk_..." \
  -H "Content-Type: application/json" \
  -d @template-export.json
```

The `format` field is checked first: a document without
`"format": "mailyard-template-v1"` is refused with `400` rather than half-imported.

{{< callout type="note" >}}
If a template with the same name already exists the import fails with `409 Conflict`. Rename the template inside
the document and try again, or delete the existing one first.
{{< /callout >}}
