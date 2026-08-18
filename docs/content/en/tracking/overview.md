---
title: "Tracking Overview"
description: "How Mailyard tracks email opens and clicks, the hosted web view, and where engagement data lands"
weight: 10
---

Mailyard measures engagement by injecting a 1×1 open-tracking pixel and rewriting links for click tracking. It also
serves a hosted "view in browser" page. All of these are served from public, unauthenticated endpoints under
`/tracking/*` so that recipients and mailbox providers can reach them.

Everything keys on the **email id** - the same `id` the send returned and the email log shows. Tracking is therefore not
a campaign feature: a campaign is simply the one kind of send where it is always on.

{{< callout type="info" title="Public endpoints" >}}
Everything under `/tracking/*` is **public — no authentication**. These URLs are meant to be opened by email recipients
and fetched by mailbox providers. They carry their own HMAC-signed tokens/signatures instead of an `Authorization`
header, so a missing or tampered signature returns `404`.
{{< /callout >}}

## Open tracking

```
GET /tracking/open/{email_id}.gif
```

When a tracked message is built, Mailyard injects a hidden pixel just before
`</body>`:

```html
<img src="http://localhost:3000/tracking/open/550e8400-e29b-41d4-a716-446655440000.gif?sig=..." width="1" height="1"
     alt="" style="display:none"/>
```

Opening the email loads the GIF, and Mailyard records an **open**. Details:

- The `sig` query parameter is a mandatory HMAC signature over the email id. A request with no signature or a bad
  signature returns `404` and records nothing, so a third party hitting the predictable path cannot inflate your
  metrics.
- The endpoint always returns a 1×1 transparent GIF with `Cache-Control: no-cache`.
- Requests from known bot user-agents (such as security scanners and link pre-fetchers) are served the pixel but **not**
  counted.
- The first open stamps `opened_at` on the email row, and `open_count` keeps counting repeats.

## Click tracking

```
GET /tracking/click/{email_id}/{hash}
```

Every `http(s)` link in the HTML body is rewritten to a Mailyard redirect URL before the message is sent. A rewritten
link looks like:

```
http://localhost:3000/tracking/click/550e8400-e29b-41d4-a716-446655440000/ab12cd34ef56gh78?sig=...
```

When the recipient clicks it, Mailyard records a **click** and then issues a
`302` redirect to the original destination. Details:

- `{hash}` is a deterministic hash of the link's **scope** and the original URL. The scope is the campaign for campaign
  mail and empty for everything else, so the same URL in two campaigns stays two separate tallies. Mailyard stores the
  original URL and looks it up on each click.
- The `sig` query parameter is a mandatory HMAC signature. A missing or bad signature returns `404`.
- Only `http://` and `https://` destinations are rewritten and redirected. `mailto:`
  and `tel:` links, and any link that already points at Mailyard's own `/tracking/`
  paths, are left untouched. Non-http redirect targets are rejected with `400` to prevent open-redirect and SSRF abuse.
- Bot user-agents are redirected but not counted.
- The first click stamps `clicked_at` on the email row, per-link totals are incremented, and a unique click is recorded
  per link per message.

## Enabling and disabling tracking

Tracking needs `server.public_url` and `auth.jwt_secret`: the pixel and redirect URLs are absolute and signed, so
neither can be built without them. With those missing, nothing is tracked anywhere and the server says so at boot.

Given that, who gets tracked:

| Send                           | Tracked                       |
|--------------------------------|-------------------------------|
| Campaign                       | always                        |
| API, template, SMTP submission | only when the project opts in |

### Campaigns

Always tracked. Measuring a campaign is what a campaign is for, and the
[unsubscribe header](/docs/tracking/unsubscribe) bulk mail must carry comes from the same machinery - which is why a
campaign refuses to start at all when `server.public_url` is unset, rather than sending unmeasured mail with no
`List-Unsubscribe`.

### Everything else

Off by default, and switched on per project under **Project Settings**, or:

```
PATCH /api/v1/projects/{id}
{ "track_opens": true, "track_clicks": true }
```

The two are separate because the objections to them are different. An open pixel is a privacy question and a small
deliverability cost. A rewritten link changes what the recipient sees when they hover, and breaks if this server is ever
unreachable.

Off by default is deliberate: a tracking pixel in a password reset is a bad look, and nobody should acquire one by
upgrading.

### Turning it off for one message

Both switches are project-level defaults. A single send can always opt **out** - never in, since enabling tracking is
the project owner's decision rather than a caller's.

Over the API:

```json
POST /api/v1/emails/send
{
    "from": "...",
    "to": [
        "..."
    ],
    "subject": "...",
    "html": "...",
    "disable_tracking": true
}
```

Over [SMTP submission](/docs/security/smtp-submission), where there is no JSON body to put a flag in, add a header:

```
X-Mailyard-Disable-Tracking: true
```

The header is **read and removed**. It is an instruction to Mailyard, not part of the message, and it never reaches the
receiving server. A bare header with no value counts as "yes" - a client that bothered to add it meant something - while
`false`, `0`, `no`
and `off` are honoured, so a template that always emits the header can still say no.

The name is namespaced on purpose. A bare `X-Disable-Tracking` is a name another vendor's tooling may already set for
its own reasons, which would silently switch tracking off here.

## Where the data goes

Open and click events feed **campaign analytics**. The authenticated, project-scoped endpoint returns aggregate counts,
per-variant breakdowns (for A/B tests), per-link click totals, and open/click time series:

```
GET /api/v1/campaigns/{id}/analytics
```

```bash
curl http://localhost:3000/api/v1/campaigns/550e8400-e29b-41d4-a716-446655440000/analytics \
  -H "Authorization: Bearer myk_..." \
```

The response includes `analytics`, optional `variant_analytics`, `links`,
`open_series`, and `click_series`. See [Campaigns](../campaigns/overview.md) for how these surface in the dashboard.

For mail sent outside a campaign, the tallies live on the email row itself and come back with it from
`GET /api/v1/emails/{id}`:

| Field                       | Meaning                                    |
|-----------------------------|--------------------------------------------|
| `tracked`                   | the message went out with tracking applied |
| `opened_at`, `clicked_at`   | first open and first click                 |
| `open_count`, `click_count` | totals, including repeats                  |

`tracked` is the field that makes the rest readable. Without it a zero `open_count`
cannot be told apart from tracking never having been switched on.

A click also sets `opened_at` when the pixel never fired. Plenty of clients block images and load nothing, so a message
somebody demonstrably read would otherwise show as unopened.

## View in browser (web view)

```
GET /tracking/view/{token}
```

Mailyard hosts a "view this email in a browser" page for any sent message. The link is produced by the
`{{ mailyard_web_view_url }}` system variable (and its
`{{ mailyard_mail_web_link }}` alias). The `{token}` is an HMAC-signed, **expiring**
capability bound to the email's opaque UUID — it defaults to a 90-day lifetime, and an invalid or expired token renders
a "link is invalid or has expired" page.

The hosted page:

- Renders the exact HTML that was sent (falling back to the text body when there is no HTML).
- **Strips the open-tracking pixel**, so loading the web view does not inflate open metrics.
- Is served with a restrictive `Content-Security-Policy`, `X-Robots-Tag: noindex,
  nofollow`, `Referrer-Policy: no-referrer`, and no cookies.

See [System Variables](../templates/system-variables.md) for how to add the web-view link to a template.
