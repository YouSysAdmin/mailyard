---
title: "Passkeys"
description: "Passwordless sign-in with WebAuthn / FIDO2"
weight: 40
---

A passkey signs you into the console with a fingerprint, a face, or a device PIN, and no password at all. It is the
strongest credential Mailyard offers, for one reason worth stating plainly: it cannot be phished. The credential is
bound to the site's origin by the browser, so a convincing copy of the sign-in page at a lookalike domain gets nothing.
A password and an authenticator code can both be typed into that page.

Passkeys are on by default and nothing changes until somebody enrols one.

## Enrolling

**Profile -> Passkeys -> Add a passkey.** You are asked for your account password, then your device asks you to confirm
with whatever it uses - Touch ID, Windows Hello, a security key, your phone.

Name it something you will recognise. If you enrol three, the name is the only thing that tells them apart when one
needs revoking.

The password confirmation is not ceremony. Adding a passkey creates a new way into the account, so a borrowed or
hijacked session must not be enough on its own. Removing one asks for the same reason.

## Signing in

The sign-in page offers **Sign in with a passkey**. There is no email field: the browser knows which passkeys it holds
for this site and shows you the picker. Choose one, confirm on your device, and you are in.

A passkey completes sign-in on its own. If the account also has
[two-factor authentication](/docs/security/two-factor-auth), Mailyard does **not** then ask for a code - and that is
deliberate rather than a shortcut. Enrolment and sign-in both require user verification, so you proved you hold the
authenticator and unlocked it. That is two factors in one gesture, and unlike a password plus a code, neither half can
be relayed to an attacker's page.

Passwords keep working. A passkey is an addition, never a replacement, so losing a device does not lock you out.

## Local accounts only

Passkeys are available on accounts that have a Mailyard password. An account that signs in through an identity provider
cannot enrol one, and the profile page says so.

This is not an oversight. A passkey is a credential your IdP knows nothing about, so an SSO account carrying one would
still be able to sign in after being disabled in the IdP - which is the entire reason an organisation put an IdP in
front of the console. Sign-in for those accounts stays the provider's decision.

## Turning it off

```yaml
auth:
    passkeys_enabled: false
```

Or `MAILYARD_AUTH_PASSKEYS_ENABLED=false`. With it off, the sign-in button disappears and every passkey endpoint
refuses. Existing rows are left alone, so turning it back on restores what people had enrolled.

## What is stored

One row per enrolled authenticator, holding the credential's **public** key, its transports, and a sign counter. The
private half never leaves your device and Mailyard never sees it - there is no secret here to leak.

Deleting the account removes its passkeys with it.

## Resetting somebody's passkeys

For the user who lost the only device holding one:

```
DELETE /api/v1/admin/users/{id}/passkeys
```

Or **Admin - Users - edit - Reset passkeys**. Refused on your own account, for the same
reason [2FA reset](/docs/security/two-factor-auth) is.

## Notes for operators

- **Passkeys need HTTPS**, except on `localhost`, which browsers exempt. Behind a reverse proxy this is a browser rule,
  not a Mailyard one.
- **The relying party comes from the browser's `Origin`**, not from
  `server.public_url`. So a passkey enrolled at one hostname does not work at another, which is what binding to an
  origin means - if the console is reachable at two names, people will need one passkey per name.
- Enrolment, removal and passkey sign-in each land in the
  [security log](/docs/admin/user-management) as `auth.passkey.*`. A rejected assertion is recorded as a failed sign-in,
  the same as a wrong password.
- A sign counter that fails to advance means the credential may have been copied, and Mailyard refuses the sign-in.
  Synced passkeys (iCloud Keychain, Google Password Manager) report a zero counter and never trigger this - it only
  catches hardware keys that count.
