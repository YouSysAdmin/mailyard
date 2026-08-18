---
title: "Data Export & Import"
description: "Export and import project data"
weight: 10
---

A portable JSON snapshot of one project: templates, contacts, subscribers, suppressions, webhooks, domains, and sender
addresses. Any project member can export their own project.

## From the console

**Project - Settings - Data Export - Export Project Data.**

The file downloads as `mailyard-export-<slug>-<date>.json`, and the page then lists how many records each section
contained so you can see at a glance what you got. This is the way to do it for a one-off request - no credentials to
arrange.

## With an API key

```
GET /api/v1/data/export
```

Requires an API key holding **`data:read`**. This is the right choice for scheduled exports and anything scripted:

```bash
curl http://localhost:3000/api/v1/data/export \
  -H "Authorization: Bearer myk_..." \
  -o mailyard-export.json
```

The key is bound to a project, so there is no header to set and no way to point it at someone else's data - a request
naming a different project is rejected with `403`.

Export sits behind `admin` rather than `read` because one call returns every tenant record at once, which is a broader
grant than any other read route. Create the key under **Developers - API Keys** holding `data:read` -
see [API Keys](/docs/security/api-keys).

## With a session cookie

```
GET /api/v1/data/export
```

The console button behind **Project Settings - Export Project Data** calls the same route.

The key is bound to a project, so there is no header to set and no way to point it at someone else's data - a request
naming a different project is rejected with `403`.

Export sits behind `admin` rather than `read` because one call returns every tenant record at once, which is a broader
grant than any other read route. Create the key under **Developers - API Keys** holding `data:read` -
see [API Keys](/docs/security/api-keys).

## With a session cookie

```
GET /api/v1/data/export
```

```bash
curl http://localhost:3000/api/v1/data/export \
  -H "Authorization: Bearer myk_..." \
  -o mailyard-export.json
```

The key is bound to a project, so there is no header to set and no way to point it at someone else's data - a request
naming a different project is rejected with `403`.

Export sits behind `admin` rather than `read` because one call returns every tenant record at once, which is a broader
grant than any other read route. Create the key under **Developers - API Keys** holding `data:read` -
see [API Keys](/docs/security/api-keys).

## With a session cookie

```
GET /api/v1/data/export
```

The console button behind **Project Settings - Export Project Data** calls the same route.

The key is bound to a project, so there is no header to set and no way to point it at someone else's data - a request
naming a different project is rejected with `403`.

Export sits behind `admin` rather than `read` because one call returns every tenant record at once, which is a broader
grant than any other read route. Create the key under **Developers - API Keys** holding `data:read` -
see [API Keys](/docs/security/api-keys).

## With a session cookie

```
GET /api/v1/data/export
```

```bash
curl http://localhost:3000/api/v1/data/export \
  -H "Authorization: Bearer myk_..." \
  -o mailyard-export.json
```

The key is bound to a project, so there is no header to set and no way to point it at someone else's data - a request
naming a different project is rejected with `403`.

Export sits behind `admin` rather than `read` because one call returns every tenant record at once, which is a broader
grant than any other read route. Create the key under **Developers - API Keys** holding `data:read` -
see [API Keys](/docs/security/api-keys).

## With a session cookie

```
GET /api/v1/data/export
```

The console button behind **Project Settings - Export Project Data** calls the same route.

The key is bound to a project, so there is no header to set and no way to point it at someone else's data - a request
naming a different project is rejected with `403`.

Export sits behind `admin` rather than `read` because one call returns every tenant record at once, which is a broader
grant than any other read route. Create the key under **Developers - API Keys** holding `data:read` -
see [API Keys](/docs/security/api-keys).

## With a session cookie

```
GET /api/v1/data/export
```

```bash
curl http://localhost:3000/api/v1/data/export \
  -H "Authorization: Bearer myk_..." \
  -o mailyard-export.json
```

The key is bound to a project, so there is no header to set and no way to point it at someone else's data - a request
naming a different project is rejected with `403`.

Export sits behind `admin` rather than `read` because one call returns every tenant record at once, which is a broader
grant than any other read route. Create the key under **Developers - API Keys** holding `data:read` -
see [API Keys](/docs/security/api-keys).

## With a session cookie

```
GET /api/v1/data/export
```

The console button behind **Project Settings - Export Project Data** calls the same route.

The key is bound to a project, so there is no header to set and no way to point it at someone else's data - a request
naming a different project is rejected with `403`.

Export sits behind `admin` rather than `read` because one call returns every tenant record at once, which is a broader
grant than any other read route. Create the key under **Developers - API Keys** holding `data:read` -
see [API Keys](/docs/security/api-keys).

## With a session cookie

```
GET /api/v1/data/export
```

```bash
curl http://localhost:3000/api/v1/data/export \
  -H "Authorization: Bearer myk_..." \
  -o mailyard-export.json
```

The project header is optional only if you belong to exactly one project. With several, name the one you mean.

## The document

```json
{
    "export": {
        "mailyard_version": "1.2.0",
        "exported_at": "2026-08-06T04:55:00Z",
        "project": {
            "id": "...",
            "name": "My Project",
            "default_language": "en"
        },
        "templates": [],
        "stylesheets": [],
        "languages": [],
        "contacts": [],
        "subscribers": [],
        "subscriber_lists": [],
        "suppressions": [],
        "unsubscribe_lists": [],
        "webhooks": [],
        "smtp_servers": [],
        "domains": [],
        "senders": []
    }
}
```

Contacts and subscribers are walked in pages internally, so an export is complete regardless of how large those tables
are.

{{< callout type="info" title="Secrets are not included" >}}
SMTP passwords, API key hashes, and TOTP seeds are omitted - the models carry `json:"-"`
on those fields, so an export is safe to hand to whoever requested it. Verified by test:
an SMTP password set through the API does not appear anywhere in the exported bytes.
{{< /callout >}}

## Import

**There is no import, and that is a decision rather than a gap.** No
`POST /api/v1/data/import` endpoint exists or is planned.

An export deliberately omits every credential - SMTP passwords, API keys, webhook signing secrets, DKIM private keys -
so a restore could never bring back the parts that make a project able to send. Domain verification is per-install, so
imported domains would arrive unverified too. An import that silently produced a project which looks complete but cannot
deliver anything would be worse than not having one.

The export is for handing a person or an auditor a complete, secret-free copy of the data. It is not a backup - back up
the database itself for that.

For moving templates between projects, use the per-template
[import and export](/docs/templates/import-export) endpoints, which do handle their own conflicts.
