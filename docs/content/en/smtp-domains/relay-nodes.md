---
title: "Relay Nodes"
description: "Your own egress machines, delivering straight to recipient mail exchangers"
weight: 50
---

Every other delivery path in Mailyard hands a message to somebody else's mail
server - a tenant's postfix, Amazon SES, a server you pasted credentials for. A
**relay node** is different: it is a machine you run that resolves the recipient's
own mail exchangers and delivers to them from its own address.

That address is the point. A receiver judges mail by the IP that connected, so
adding sending capacity becomes a matter of starting another node rather than
opening another provider account.

A node can be anywhere - another provider, another continent. It holds no database
credentials and nothing reaches into it except your delivery workers, over mutual
TLS.

{{< callout type="warning" title="Enterprise edition" >}}
Relay nodes are not available in the community edition, which delivers through
the SMTP servers a project configures and through the shared pool.
{{< /callout >}}
