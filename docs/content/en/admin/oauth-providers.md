---
title: "Identity Providers"
description: "Configure OpenID Connect single sign-on for the console at runtime"
weight: 60
---

Single sign-on for the operator console, configured while the server runs. Add a provider under **Admin - Identity
Providers** and a "Continue with ..." button appears on the sign-in page immediately - no configuration file, no
restart.

Providers are **platform level**, not per project. A user account is a platform entity that then holds project
memberships, so who may sign in at all is not a tenant's decision to make. Only a platform admin can manage them.

{{< callout type="note" title="Local login stays on" >}}
Identity providers never disable password sign-in. Local login is how you configure the first provider, and how you get
back in when a provider breaks or its certificate expires. Keep at least one local admin account.
{{< /callout >}}

## Adding a Provider

1. **Admin - Identity Providers - Add Provider.**
2. Choose a type. **Google** already knows its own endpoints and needs only a client id and secret. **OpenID Connect**
   works with any compliant provider - Keycloak, authentik, Okta, Auth0, Entra ID, Cognito - and discovers its endpoints
   from the issuer.
3. Paste the **issuer** URL. This is the provider's address, not Mailyard's. If you enter Mailyard's own URL the form
   rejects it, because discovery would then fetch the configuration document from Mailyard itself and fail with an
   unhelpful error later.
4. Paste the **client id** and **client secret** from the provider's console.
5. Copy the **redirect URI** the page shows and register it at the provider.

The redirect URI is derived from `server.public_url`:

```
<server.public_url>/app/api/auth/oauth/<slug>/callback
```

{{< callout type="warning" title="public_url must be set" >}}
Without `server.public_url` the redirect URI has no host and SSO cannot work. The provider has to reach you by your
external name, which the inbound request's `Host`
header may not be behind a proxy.
{{< /callout >}}

## Testing

**Test** on each provider fetches the discovery document and reports what came back. It confirms the issuer is reachable
and that the endpoints resolve.

It cannot confirm the client secret - only a real sign-in exercises that. A green result means "the provider answered",
not "logins will succeed".

## Who Gets In

By default anyone the provider authenticates is admitted, and an account is created on first sign-in. Narrow that with:

| Control                                | Effect                                                                                                                                           |
|----------------------------------------|--------------------------------------------------------------------------------------------------------------------------------------------------|
| **Restrict to email domains**          | Only these domains may sign in. Enter `example.com`, not `@example.com`.                                                                         |
| **Restrict to specific addresses**     | Only these exact addresses. When set, the domain list is ignored.                                                                                |
| **Restrict to groups**                 | Needs a **groups claim** naming the claim your provider sends - `groups` for most, `cognito:groups` for Cognito, sometimes `roles` for Keycloak. |
| **Require a verified email address**   | Rejects a token whose `email_verified` is not true.                                                                                              |
| **Create an account on first sign-in** | Turn off for invite-only SSO: the account must already exist, or the sign-in is refused.                                                         |

Every denial shows the user the same generic message. The specific reason goes to the server log and the security log,
so the failure does not tell an outsider which rule stopped them.

## How Accounts Are Matched

On each sign-in, in order:

1. **The stored identity link** - the pair of provider and the provider's `sub` claim. This is the most stable key and
   survives the user changing their email at the provider.
2. **Email address** - links a provider identity to an account that already exists, which is how an existing local user
   adopts SSO. That user keeps their password unless an admin removes it.
3. **Create** - when the provider allows auto-registration.

New accounts are created as ordinary users. The single exception is the very first account on a brand-new installation,
which becomes an admin so whoever set the provider up can administer it. Satisfying an allowlist is never a privilege
grant.

The identity link is keyed on the pair `(provider, subject)`, not the subject alone, because a subject is only unique
within its issuer. One person can hold identities at several providers, and two providers can hand out the same subject
string.

## Enabled and Hidden

- **Disabled** - not offered, and a direct link is refused as if the provider did not exist.
- **Hidden** - not shown on the sign-in page but still reachable at its start URL directly. Useful for staged rollout or
  a break-glass provider.
- **Incomplete** - an enabled provider missing a client id or secret is never offered, because a button that can only
  fail is worse than no button. The admin page labels it.

## A Provider Without Discovery

Almost every provider publishes `/.well-known/openid-configuration`. For one that does not, open **advanced settings**
and set the authorization, token, and userinfo URLs by hand.

{{< callout type="warning" title="Manual endpoints verify less" >}}
Without a discovery document there are no published signing keys, so the ID token cannot be verified locally. Identity
is read from the userinfo endpoint instead, using the access token that came directly from the token endpoint over TLS.
That is a weaker check than a verified ID token. Prefer discovery whenever the provider supports it.
{{< /callout >}}

## Security Properties

- The **client secret** is encrypted at rest through the crypto service and never returned by the API - the console
  shows only whether one is set. Editing another field leaves the stored secret untouched.
- The flow uses **PKCE**, a **nonce** bound to the ID token, and a **state** parameter carried in a short-lived signed
  cookie, all checked on callback.
- The state cookie also records which provider the round-trip started at, so a code issued by one provider cannot be
  replayed against another provider's callback.
- The ID token's **signature, audience, expiry, and nonce** are all verified before any claim is trusted.
- Access and refresh tokens from the provider are **not stored**. Mailyard needs the identity at sign-in and nothing
  afterwards, so keeping them would be a liability with no consumer.

## API

Platform admin, on the console surface.

```
GET    /api/v1/admin/oauth-providers
POST   /api/v1/admin/oauth-providers
GET    /api/v1/admin/oauth-providers/{id}
PATCH  /api/v1/admin/oauth-providers/{id}
DELETE /api/v1/admin/oauth-providers/{id}
POST   /api/v1/admin/oauth-providers/{id}/test
```

```bash
curl -X POST http://localhost:3000/api/v1/admin/oauth-providers \
  -H "Authorization: Bearer mya_..." \
  -H "Content-Type: application/json" \
  -d '{
    "name": "Company SSO",
    "type": "oidc",
    "issuer": "https://accounts.example.com",
    "client_id": "mailyard",
    "client_secret": "...",
    "allowed_domains": ["example.com"],
    "require_email_verified": true
  }'
```

List fields (`scopes`, `allowed_domains`, `allowed_emails`, `allowed_groups`) are JSON arrays, not comma-separated
strings. `client_secret` is write-only: omit it on a `PATCH`
to keep the stored value.

The slug is derived from the name when you do not supply one. It appears in the sign-in URL, so changing it changes the
redirect URI and the provider has to be updated to match.

The sign-in page reads its provider list from an open endpoint, which is returns only each provider's name, slug, type,
and start URL - never client ids, issuers, or allowlists.

## Projects Do Not Pin a Provider

A provider is installation-wide, and that is the whole of it: signing in proves who somebody is, and it never decides
what they may reach.

A project used to be able to name a provider, require it, and admit anyone from a matching email domain automatically.
All three are gone. They gave a project control over the *sign-in*, which is not a project's to govern - and the
consequences showed it. The requirement was only checked for the project named in the request header, so the routes that
address a project by path went unchecked. A stale project id in the browser could leave a member unable to load their
own project list. And somebody belonging to two projects that required different providers could not satisfy both,
because a session records one provider.

What replaces it, in the order a person meets it:

1. **Who may sign in at all** is decided here, by this provider's allowlists - see
   [Who Gets In](#who-gets-in). This is the control that matters for a provider with a global user base like Google:
   without an email or domain rule, every Google account on earth satisfies it.
2. **Whether an account is created** is *Create an account on first sign-in*.
3. **Which projects they can reach** comes from an invitation, sent by an owner or a member holding `members:write` -
   see
   [Members and invitations](/docs/projects/members-and-invitations). The invitation also carries the role, so joining
   through SSO never hands out permissions on its own.

An installation that must not accept passwords at all sets `auth.local.enabled: false`. Password sign-in is then refused
outright and the form disappears from the login page. That is deliberately installation-wide rather than per project:
authentication belongs to the platform, so one install cannot have one project mandate a provider while another accepts
passwords.

Group claims are an admission check and nothing more. A group never maps to a role - satisfying an allowlist is not a
privilege grant, and roles come from a person deciding, which is what the invitation is.
