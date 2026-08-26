---
title: "Session Management"
description: "Revoking console sign-ins"
weight: 70
---

Every console sign-in creates a tracked row, so a live cookie can be revoked before it expires. API keys are not
sessions - revoke those on the
[API Keys](/docs/security/api-keys) page.

## Seeing and revoking your own

**Profile - Active Sessions.** Each row shows the browser, the address it signed in from and when it was last used, with
the current one marked. Revoke one, or sign out everywhere else in a single action.

## Revoking somebody else's

**Admin - Users - edit - Revoke all sessions.** Allowed on your own account too:
signing yourself out everywhere is a legitimate thing to want.

## What else ends a session

| Action                                   | Effect                                     |
|------------------------------------------|--------------------------------------------|
| Signing out                              | That one session                           |
| Changing the password                    | Every OTHER session, the current one stays |
| Completing a password reset              | Every session for the account              |
| Expiry (`auth.session_ttl`, default 12h) | The token and the row both lapse           |

## Revocation timing

A verified session is cached in memory for 15 seconds, so authenticating a request costs no database read.

- **Single node**: immediate. The node serving the revoke drops its own entry.
- **Multiple nodes**: usable elsewhere for up to 15 seconds, until those entries go stale.

## Retention

Expired sessions are deleted by the [retention job](/docs/admin/scheduled-jobs). Revoked ones are kept until their
natural expiry, so a recent "signed out everywhere" is still visible in the list.

## The cookie

The session rides in `mailyard_session`: `HttpOnly`, `Path=/`, `SameSite=Strict`, and
`Secure` when `server.public_url` starts with `https://`.

`Secure` is decided by that setting rather than by the scheme of the request, so a reverse proxy that terminates TLS and
speaks plain HTTP upstream still gets the flag. The cost is that the two have to agree:

{{< callout type="warning" title="public_url must match the scheme you actually use" >}}
Set `public_url` to `https://…` and then open the console over `http://`, and sign-in appears to fail: the login returns
200 and sets the cookie, the browser refuses to store a `Secure` cookie on a plain connection, and every request after
it is unauthenticated. Nothing in the response says so.
{{< /callout >}}

`SameSite=Strict` is load-bearing rather than a default: it is what makes accepting this cookie on `/api/v1` safe,
because a browser never attaches it to a cross-site request. Strict draws a site boundary, not an origin one, so a
mutating request that carries the cookie from another origin - a sibling subdomain, say - is refused with 403 unless
that origin is `server.public_url`, the host the request arrived on, or listed in `cors.allowed_origins`. Together the
two mean cookie authentication only works from Mailyard's own origin, so a third-party browser app needs an
[API key](/docs/security/api-keys).

### Safari cannot hold it on a bare IP address

Reaching the console at `http://192.168.1.10:3000` or `http://127.0.0.1:3000` works in Chrome and Firefox and fails in
Safari: it accepts the `Set-Cookie` and then sends nothing back, so sign-in bounces straight to the login page with
`authentication required`. An IP literal has no registrable domain, and WebKit needs one.

Give the installation a hostname - `localhost` is enough for a local instance, a DNS name or a `/etc/hosts` entry for
anything else - and set `public_url` to match.
