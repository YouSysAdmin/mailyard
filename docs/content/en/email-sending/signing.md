---
title: "Signed Mail"
description: "Sign outgoing mail as its sender address with S/MIME or PGP"
weight: 45
---

Mail from a registered sender address can carry a signature made with that address's own key. DKIM already proves the
sending domain to the receiving server. This proves the address to the person reading: their mail client shows a seal
beside a message whose signature checks against a key it knows, and warns on one that does not. It is the one layer of
phishing defence the reader sees for themselves, and it survives forwarding.

The signature is placed inside the message, as RFC 1847 `multipart/signed`, and DKIM then covers the signed body. Two
kinds are offered behind one switch, because they are read by different clients.

|                                                | S/MIME                                                                              | PGP                                                             |
|------------------------------------------------|-------------------------------------------------------------------------------------|-----------------------------------------------------------------|
| Verified without plugins by                    | Outlook desktop and web, Apple Mail, iOS Mail, Google Workspace with S/MIME enabled | Thunderbird                                                     |
| Verified with a plugin by                      |                                                                                     | Outlook with gpg4win, Apple Mail with GPG Suite, K-9, FairEmail |
| Shown by Gmail web and consumer Outlook web as | a seal, when the issuer is trusted                                                  | an attachment named `signature.asc`                             |
| Key comes from                                 | a certificate authority, public or your own                                         | generated here, or imported                                     |
| How the reader learns the key                  | the certificate rides inside the signature                                          | an `Autocrypt` header and an attached key file                  |
| Expires                                        | yes, usually in one to three years                                                  | usually not                                                     |

Pick by where your recipients read. For a corporate audience on Outlook or Google Workspace, S/MIME is the one that
shows a seal. For engineers and security teams, PGP is the one they already verify.

## Giving an address a key

Under **Domains**, in the **Sender Addresses** card, every address has a **Signing** button. The key belongs to the
address, not to the domain, because a mail client compares the key's identity against the From address: a PGP user id or
an S/MIME certificate for `billing@example.com` is what verifies mail from `billing@example.com`.

### S/MIME

An S/MIME certificate is **imported, never generated here**. A self-signed certificate shows as untrusted in every
client, which is worse than no signature at all. Get one from a public authority that issues mail certificates, or from
your organisation's own authority if your recipients' machines trust it.

The dialog takes the `.p12` or `.pfx` file the authority hands over, with its password, or a PEM certificate chain plus
an unencrypted PEM private key. A password-protected PEM key goes in as a `.p12` file instead.

The certificate is checked at import, because every one of these would otherwise be found by a recipient:

- it must name the sender address, in a `rfc822Name` subject alternative name or the legacy `emailAddress` attribute,
- it must be issued for email protection (the `emailProtection` extended key usage), so a TLS certificate is refused,
- it must be a mailbox certificate, not a certificate authority (`CA:TRUE`),
- it must not be expired or not yet valid,
- the private key must match it, and be RSA or ECDSA.

### PGP

**Generate key** mints an Ed25519 signing key with a Curve25519 encryption subkey and the address as its single user id.
**Import** takes an armored private key block, opened with its passphrase, as long as one of its user ids is the
address. The passphrase is removed on import and the key is stored encrypted at rest, like every secret here.

With **Attach the public key** on, which is the default, every message carries an `Autocrypt` header and an
`OpenPGP_0x....asc` file inside the signed body. Thunderbird and other Autocrypt clients learn the key from the first
message and verify every one after it. Turn it off for a key you distribute another way, since it adds a few kilobytes
to every message, campaigns included.

## What goes out

With a key stored and **Sign outgoing mail** on, every message from the address is signed: API sends, template sends,
campaigns and mail submitted over the SMTP relay. The text parts are written quoted-printable inside the signed body,
because a bare line ending in a space is something a relay may strip and the signature would then break.

A caller may decline for one message with `disable_signing: true` on `POST /api/v1/emails/send` or `send-template`, and
a campaign carries the same flag, shown as a checkbox under its From address whenever that address holds a key. There is
no way to turn signing on from a request: whether an address signs is decided where its key is.

{{< callout type="warning" title="Mail submitted over SMTP is re-signed, not passed through" >}}
A message submitted over the SMTP relay is parsed and rebuilt before delivery, which already breaks any signature the
submitting client put on it. With a key on the sender address the server signs the rebuilt message itself. For a shared
address that is the better arrangement anyway: one key, held here, rather than one on every workstation that sends as
the address.
{{< /callout >}}

The email log shows which signature a message carried in its **Signature** row, and the `.eml` download is signed the
same way. A sandbox capture is signed as a delivery would be, so the captured record shows the shape a recipient's
client sees.

## When a certificate runs out

Mail from an address whose certificate has expired still goes out. Only the signature shows as invalid, in every client,
which is exactly the signal the feature exists to give and now given about your own mail. So the platform sweeps for
keys running out and mails the project's owners, and its alert address if one is set, once a day from thirty days ahead.
Import the renewed certificate under the same **Signing** button: **Replace** removes the old key and takes the new one.

A key can be switched off without being removed, which keeps it for later and sends unsigned meanwhile.

## The API

Under `/api/v1/senders/{id}`, with the `senders` permissions:

| Method   | Path                  | Does                                                                                                                                                                                                                                                                                                                          |
|----------|-----------------------|-------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------|
| `POST`   | `/signing`            | Generate or import the key. `{"kind": "pgp"}` generates, `{"kind": "pgp", "private_key": "...", "passphrase": "..."}` imports, `{"kind": "smime", "pkcs12": "<base64>", "passphrase": "..."}` or `{"kind": "smime", "certificate": "...", "private_key": "..."}` stores a certificate. `409` when the sender already has one. |
| `PATCH`  | `/signing`            | `{"sign": false}` or `{"attach_key": false}`. A field left out keeps its value. `attach_key` is PGP only.                                                                                                                                                                                                                     |
| `DELETE` | `/signing`            | Remove the key.                                                                                                                                                                                                                                                                                                               |
| `GET`    | `/signing/public-key` | The armored public key or the PEM chain, for publishing wherever recipients look keys up.                                                                                                                                                                                                                                     |

A sender in `GET /api/v1/senders/` carries a `signing` object describing its key, the fingerprint, algorithm, subject,
expiry and the two switches, and never the key material.

{{< callout type="tip" title="Encryption is a separate question" >}}
A signature proves who sent the mail. Encrypting it to the recipient needs the recipient's key, which this does not
hold. Signed mail is readable by everyone and verifiable by anyone with the sender's public key.
{{< /callout >}}
