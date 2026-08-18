---
title: "Sender Addresses"
description: "Approved From addresses and strict sender mode"
weight: 40
---

A sender address is a From address the project has registered as approved. They serve two purposes: they populate the
From selector in the console so nobody has to retype an address, and - when strict mode is on - they become the only
addresses the project is allowed to send from.

This sits on top of [domain verification](/docs/smtp-domains/domain-verification), which is the harder gate. Every send
already requires the From **domain** to be verified by the sending project. Sender addresses narrow that further, from
"any address at our domain" to "these addresses".

## Registering an Address

```
POST /api/v1/senders
```

```json
{
    "email": "billing@example.com",
    "name": "Acme Billing"
}
```

The domain must already be verified by this project, otherwise the call is `400`. A verified domain covers its
subdomains, so a verified `example.com` is enough to register `noreply@mail.example.com`. Registering the same address
twice answers `409`.

`name` is the display name the console offers alongside the address. It is optional and cosmetic - it does not restrict
what a send may put in the From header unless strict mode is on.

## Listing and Removing

```
GET    /api/v1/senders
DELETE /api/v1/senders/{id}
```

Removing an address takes it out of the console selector, and - under strict mode - stops sends from it. It does nothing
to mail already queued or delivered.

## Strict Sender Mode

Off by default. Turn it on per project:

```
PATCH /api/v1/projects/{id}
{ "strict_senders": true }
```

With it on, every send whose From address is not registered is refused:

Response (`400`):

```json
{
    "error": "sender \"ops@example.com\" is not a registered sender address (strict mode is on for this project)"
}
```

The check runs on **every** surface - the machine API, template sends, campaigns and SMTP submission - because it lives
in the shared validation path rather than in one handler.

{{< callout type="tip" title="When strict mode earns its keep" >}}
Domain verification already stops one project sending as another's domain. Strict mode answers a different question:
which addresses inside *your own* domain may be used. It is worth turning on when several people or integrations hold
send credentials and you want `billing@` and `noreply@` to be the only things that ever appear in a From header, rather
than whatever an integration was configured with.
{{< /callout >}}

{{< callout type="warning" title="Turn it on after registering the addresses" >}}
Enabling strict mode with an empty sender list refuses every send from the project. Register what you send from first,
then switch it on.
{{< /callout >}}
