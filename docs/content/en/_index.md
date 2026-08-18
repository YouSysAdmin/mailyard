---
title: "Mailyard Documentation"
description: "What Mailyard does, by feature area"
weight: 0
---

## Sending

| Area                                                   | What is covered                                                                                                                 |
|--------------------------------------------------------|---------------------------------------------------------------------------------------------------------------------------------|
| [Email Sending](/docs/email-sending/single-email)      | Single, template, batch and scheduled sends, attachments, delivery status, the email log, address verification, and the sandbox |
| [Templates](/docs/templates/overview)                  | Versioning, localization, stylesheets, preview and test sends, import and export, system variables                              |
| [Campaigns](/docs/campaigns/overview)                  | Bulk sending over a subscriber list, throttling, A/B testing                                                                    |
| [Subscribers](/docs/subscribers/subscriber-management) | Subscriber records, bulk import, static and dynamic lists                                                                       |

## Delivery infrastructure

| Area                                                          | What is covered                                                                                                                    |
|---------------------------------------------------------------|------------------------------------------------------------------------------------------------------------------------------------|
| [SMTP and Domains](/docs/smtp-domains/smtp-servers)           | Per-project servers, named groups with failover, domain verification, approved sender addresses, relay nodes, SES, bounce handling |
| [Inbound Email](/docs/inbound/overview)                       | The MX listener, receiving mail on a verified domain, and managing what arrives                                                    |
| [Contacts and Suppression](/docs/contacts/contact-management) | Delivery tallies per address, bounce handling, the suppression list, unsubscribe lists                                             |
| [Tracking](/docs/tracking/overview)                           | Open pixel, click redirects, hosted unsubscribe                                                                                    |

## Operating an instance

| Area                                                  | What is covered                                                                                                       |
|-------------------------------------------------------|-----------------------------------------------------------------------------------------------------------------------|
| [Security](/docs/security/authentication)             | Sign-in, API keys, SMTP submission credentials, passkeys, two-factor auth, rate limits, sessions                      |
| [Projects](/docs/projects/overview)                   | Multi-tenancy, roles and permissions, members and invitations, per-project settings                                   |
| [Admin Panel](/docs/admin/user-management)            | Accounts, platform settings and metrics, the shared SMTP pool, scheduled jobs, identity providers, plans, system mail |
| [Webhooks and Events](/docs/webhooks/overview)        | Event types, delivery tracking, the audit log                                                                         |
| [Analytics and Monitoring](/docs/analytics/dashboard) | Dashboard figures, delivery trends, Prometheus metrics, health probes                                                 |
| [Data](/docs/data/data-export-import)                 | Project export and per-address or bulk erasure                                                                        |

## Every endpoint

These pages cover the routes a feature is used through. For the complete list, with request and response schemas, export
the OpenAPI document from the binary you run:

```bash
mailyard export-api-spec --out openapi.yaml
```

{{< callout type="info" title="The binary is the authority" >}}
Where a page and the running instance disagree, the instance is right and the page is out of date. Report the difference
rather than working around it.
{{< /callout >}}
