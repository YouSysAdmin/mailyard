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
