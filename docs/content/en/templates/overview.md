---
title: "Overview"
description: "Email templates with versioning and localization"
weight: 10
---

A template is a named piece of content you address from the send API instead of pasting a body into every request. What
makes it more than a snippet store is that the content is **versioned** and **per-language**, and that the version a send
resolves is a deliberate choice rather than whatever was saved last.

## The three levels

Content does not live on the template. It lives two levels down, and knowing which level holds what is most of
understanding this section.

| Level | Holds | Changes how often |
|---|---|---|
| **Template** | Name, description, default language, sample data, the attachments | Rarely — it is the identity |
| **Version** | A numbered revision, plus the stylesheet its content was written against | Per edit round |
| **Localization** | The actual subject, HTML and text, for one language | Per language, per version |

The name is unique within a project and is what `POST /api/v1/emails/send-template` accepts as `template_name`. The `id`
works too, and is what the console uses.

## Exactly one version is active

Creating a version does not publish it. A template points at one `active_version_id`, and that is the only version a
send will resolve — every other version is history you can read, copy from, or switch back to.

{{< callout type="tip" title="Publishing is a separate call" >}}
`POST /api/v1/templates/{id}/activate/{versionId}` is the publish step. Editing a draft version cannot affect live mail
until you make that call, which is what lets you rewrite a receipt template during business hours.
{{< /callout >}}

A template with no active version is a template that cannot send: the request is refused with `template "x" has no
active version` rather than falling back to the newest.

## Resolving a language

A send may name a `language`. Resolution walks four steps and stops at the first localization that exists on the active
version:

1. The language the send asked for
2. The template's `default_language`
3. `en`
4. Whichever localization the version happens to have first

The last two are safety nets, not features to rely on — step 4 exists so a version with content in one unexpected
language still sends something rather than failing. If which language goes out matters, set `default_language` and
create that localization.

## Writing content

Values are substituted with `{{ name }}`, and the leading dot Go templates normally require is optional — see
[Creating Templates](/docs/templates/creating-templates) for the full syntax, including conditionals and loops.

The three parts are not rendered the same way, deliberately:

- **HTML** goes through Go's contextual escaper, so a value carrying `<` or a quote is escaped for the position it lands
  in — inside an attribute, inside a URL, inside a script.
- **Subject and text** do not, because nothing will interpret them. An ampersand in somebody's surname reaches the
  recipient as an ampersand instead of `&amp;`.

A missing key is an **error** on a transactional send and a **blank** on a campaign send. The strict default is
deliberate: rendering an empty space where a name belongs produces mail that goes out wrong and never reports it.
Campaigns relax it because subscriber custom fields are uneven by nature.

## Stylesheets are inlined

A version may reference a [stylesheet](/docs/templates/stylesheets). At render time its rules are resolved against the
document and written onto the elements as `style` attributes, because Gmail's web client and Outlook both discard a
`<style>` block. Class names are kept rather than stripped, so the markup stays readable and the tracking stripper can
still find what it needs.

If inlining fails the message still goes out with the block intact. That is worse styling in some clients, which beats
no mail at all on a path where refusing costs somebody their delivery.

## Attachments belong to the template

Files uploaded to a template ride along on **every** send that renders it — the terms PDF on a contract mail, the
calendar invite on a booking confirmation. They are bound to the template rather than to a version, so changing the copy
does not mean re-uploading the file.

## What ships out and back

`GET /api/v1/templates/{id}/export` produces one self-contained JSON document: the template, every version, every
localization, and the referenced **stylesheets inlined into it**. That last part is what makes the document portable —
it imports into another project or another install without carrying a dangling reference.

## Where to go next

- [Creating Templates](/docs/templates/creating-templates) — the variable syntax and the authoring calls
- [Versioning](/docs/templates/versioning) — drafting and activating
- [Localization](/docs/templates/localization) — adding languages
- [Stylesheets](/docs/templates/stylesheets) — shared CSS
- [Preview & Test](/docs/templates/preview-and-test) — rendering without sending
- [System Variables](/docs/templates/system-variables) — the links Mailyard fills in
- [Import/Export](/docs/templates/import-export) — moving templates between installs
