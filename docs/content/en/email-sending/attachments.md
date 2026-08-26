---
title: "Attachments"
description: "Send emails with file attachments"
weight: 50
---

Attach files to emails by providing base64-encoded content in the `attachments` array. Attachments work the same way on
`/emails/send` and `/emails/send-template`, on both the console API and the machine API.

## From the console

The **Send Email** page has an attachment picker that works in both Raw and Template mode. Files are read in the
browser, base64-encoded, and sent with the message - there is no staging upload, so abandoning the form leaves nothing
behind on the server.

The picker enforces the same limits the server does, reading them from
`GET /api/v1/emails/limits` rather than hardcoding them, so tuning the config below changes what the form accepts
without a rebuild.

{{< callout type="note" title="Template mode" >}}
A template can carry its own stored attachments (managed on the template's detail page, or under
`/api/v1/templates/:id/attachments`). Those are appended by the server on top of whatever the form or the request sends,
and both sets count toward the limits below.
{{< /callout >}}

## Limits

| Limit           | Config key                          | Default |
|-----------------|-------------------------------------|---------|
| Per attachment  | `sending.max_attachment_size`       | 10 MiB  |
| Total per email | `sending.max_total_attachment_size` | 25 MiB  |
| Count per email | not configurable                    | 10      |

Exceeding a size limit is a `400` naming the offending file:

```json
{
    "error": "attachment \"report.pdf\" exceeds maximum size of 10485760 bytes"
}
```

{{< callout type="warning" title="The HTTP body limit is derived from these" >}}
Base64 inflates a file by 4/3, so the request body cap on the send routes is computed from
`sending.max_total_attachment_size` at startup, not set independently. It applies to the routes that carry
attachments only - every other API route is capped at 8 MiB, whatever the attachment limits say.

A request larger than that cap is rejected by the HTTP layer before any handler runs, and that response is plain text
(`Request Entity Too Large`), not the usual JSON error shape. If you are building a client, treat a `413` as a size
failure without trying to parse it.
{{< /callout >}}

Query the effective limits instead of assuming the defaults:

```bash
curl http://localhost:3000/api/v1/emails/limits \
  -H "Authorization: Bearer myk_..."
```

```json
{
    "limits": {
        "max_recipients": 50,
        "max_attachments": 10,
        "max_attachment_size": 10485760,
        "max_total_attachment_size": 26214400
    }
}
```

The same endpoint is on the machine API at `/api/v1/emails/limits` under `emails:read`.

## The shape

Each entry has three fields, and only two of them matter:

| Field | Notes |
|---|---|
| `filename` | **Required.** An entry without one is refused |
| `content` | The file, base64 with standard padding |
| `content_type` | Optional — defaults to `application/octet-stream` |

Leaving `content_type` out is safe but rarely what you want: `application/octet-stream` tells the recipient's client
nothing, so a PDF arrives as an anonymous blob to be downloaded rather than something the client can preview inline.
Send the real type when you know it.

`filename` reaches the recipient twice, in the part's `Content-Type` name parameter and in its `Content-Disposition`.
Both are encoded properly, so a name with spaces or non-ASCII characters is safe to send as-is.

## Sending one

```bash
curl -X POST http://localhost:3000/api/v1/emails/send \
  -H "Authorization: Bearer myk_..." \
  -H "Content-Type: application/json" \
  -d "$(jq -n \
        --arg pdf "$(base64 < report.pdf | tr -d '\n')" \
        '{from: "sender@example.com",
          to: ["recipient@example.com"],
          subject: "Your report",
          html: "<p>The March report is attached.</p>",
          attachments: [{filename: "report.pdf", content: $pdf, content_type: "application/pdf"}]}')"
```

Building the body with `jq` rather than string interpolation is deliberate. Base64 output wraps at 76 columns on some
platforms and not others, and an embedded newline breaks the JSON — `tr -d '\n'` and a real JSON encoder remove both
problems at once.

Add further files by extending the array. Every entry counts toward all three limits above, and so do a template's own
stored attachments.

## When it is refused

Validation runs over the whole set before anything is written, and the message names the file:

| Message | Cause |
|---|---|
| `attachment filename is required` | An entry with no `filename` |
| `attachment "report.pdf" has invalid base64 content` | Not decodable — usually a stray newline or missing padding |
| `attachment "report.pdf" exceeds maximum size of 10485760 bytes` | Over the per-file limit |
| `total attachment size exceeds maximum of 26214400 bytes` | The set is over the combined limit |

Sizes are measured on the **decoded** bytes, not on the base64 you sent, so a 9 MB file is a 9 MB attachment even though
it travels as roughly 12 MB of text.

## Generating a client

The Go client's `Attachment` type mirrors the shape above; the Python and Ruby clients take it as a plain dict or hash.
See
[API Keys - clients](/docs/security/api-keys#openapi-description). For any other language, generate from the OpenAPI
document, which carries the shape above so you do not transcribe field names by hand.
