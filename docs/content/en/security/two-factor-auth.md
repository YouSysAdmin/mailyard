---
title: "Two-Factor Authentication"
description: "TOTP for console sign-in"
weight: 50
---

A TOTP code on top of the password, for signing in to the console. It does not apply to API keys - those are bearer
tokens and carry no second factor.

Local accounts only. An account an identity provider owns manages its second factor there.

## Turning it on

**Profile - Two-Factor Authentication - Set up 2FA.** Scan the QR code with an authenticator app, then enter a code to
confirm. From then on sign-in asks for a code after the password.

A code is single use: presenting the same one twice is refused, even inside the 90-second skew window.

## Recovery codes

Turning 2FA on also hands you **ten recovery codes**, shown once. Each one signs you in a single time in place of the
authenticator code - type it into the same field at sign-in. Keep them somewhere that is not the phone.

**Profile - Two-Factor Authentication** shows how many are left. **Generate new codes** asks for your password and
replaces the whole set, so a printout you no longer trust stops working the moment you do.

Using a recovery code is recorded in the security log and mailed to the account, because it means either the phone is
gone or somebody else holds the codes. Turning 2FA off, or an administrator resetting it, deletes the codes with it.

## Lockout

Five wrong codes in a row - authenticator or recovery - lock the second factor for fifteen minutes. During the lockout
every code is refused, a right one included, and the response is the same as for a wrong code.

## Turning it off

**Profile - Two-Factor Authentication - Disable.** It asks for a current code, which is what proves you still hold the
authenticator.

## Resetting it for somebody else

For the user who lost their phone:

```
DELETE /api/v1/admin/users/{id}/2fa
```

```bash
curl -X DELETE http://localhost:3000/api/v1/admin/users/b8493407-52ae-4d51-8396-9c8864977976/2fa \
  -H "Authorization: Bearer mya_..."
```

Or **Admin - Users - edit - Reset 2FA**.

Refused on your own account: disabling your own asks for a code, and this route does not, so allowing it on yourself
would turn a hijacked admin session into a way to strip the second factor off that same admin. Another administrator
can, and the security log records who did.
