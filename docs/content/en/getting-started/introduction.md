---
title: "Introduction"
description: "What is Mailyard and why use it"
weight: 10
---

**Mailyard** is a self-hosted email delivery platform that gives developers full control over their email
infrastructure. It provides a developer-friendly REST API for sending emails, managing templates, tracking delivery, and
monitoring analytics — all without relying on third-party services like SendGrid or Mailgun.

## What is in it

**Getting mail out.** A REST API for single, [templated](/docs/email-sending/template-email) and
[batch](/docs/email-sending/batch-email) sends, with [scheduling](/docs/email-sending/scheduled-email), automatic
retries and a [log](/docs/email-sending/email-log) of everything that went. An
[SMTP submission](/docs/security/smtp-submission) listener for applications that speak SMTP rather than HTTP.

**Deciding how it leaves.** [Servers grouped into pools](/docs/smtp-domains/server-groups) with failover inside one
attempt, a [shared pool](/docs/admin/shared-servers) an administrator maintains for projects with no server of
their own, [Amazon SES](/docs/smtp-domains/aws-ses) as a provider rather than as a relay, and
[relay nodes](/docs/smtp-domains/relay-nodes) that put egress on machines elsewhere.

**Content.** [Templates](/docs/templates/overview) that are versioned and localized, with a draft you can preview and
activate independently of what is live, and [stylesheets](/docs/templates/stylesheets) inlined at render time because
mail clients discard a style block.

**Audience.** [Subscribers](/docs/subscribers/subscriber-management) with custom fields,
[static lists and rule-based segments](/docs/subscribers/subscriber-lists), and
[campaigns](/docs/campaigns/overview) that render per person, throttle, pause and
[split-test](/docs/campaigns/ab-testing).

**Reputation.** [Domain verification](/docs/smtp-domains/domain-verification) with SPF, DKIM and DMARC, refusal to send
from a domain this project has not proved it owns, [bounce](/docs/contacts/bounce-handling) intake from return paths and
feedback loops, and [suppression](/docs/contacts/suppression-list) that is scoped where it should be scoped.

**Knowing what happened.** [Open and click tracking](/docs/tracking/overview),
[webhooks](/docs/webhooks/overview) with signatures and a delivery log,
[analytics](/docs/analytics/email-analytics), and [Prometheus metrics](/docs/analytics/prometheus-metrics).

**Mail coming in.** An [MX listener](/docs/inbound/overview) for verified domains, checking SPF, DKIM and DMARC at
ingest, and a [sandbox](/docs/email-sending/sandbox) that captures rather than delivers — decided by the credential, so
a test suite cannot mail a real customer by getting one flag wrong.

**Who may do what.** [API keys](/docs/security/api-keys), sessions, [passkeys](/docs/security/passkeys),
[TOTP](/docs/security/two-factor-auth), [OIDC](/docs/admin/oauth-providers), and
[per-project roles](/docs/projects/members-and-invitations) each project writes for itself out of a fixed permission
catalogue — there are no built-in roles to work around.

**Running it.** A console for all of the above, an [admin area](/docs/admin/platform-settings) for platform settings,
users, the shared pool and [certificates](/docs/admin/certificates) including an internal CA, and
[scaling](/docs/getting-started/scaling) by putting more nodes behind one queue.
| **Data portability**       | Project export and bulk erasure                                                                                    |
| **Prometheus Metrics**     | Built-in observability for production monitoring                                                                   |

## API Reference

The binary writes its own OpenAPI description:

```bash
mailyard export-api-spec --out openapi.yaml
```

Feed it to Swagger UI, Postman, or a client generator. These docs are served at `/docs`
on the same instance.

## Architecture

One process is the whole system. The API, the console, the SMTP listeners and the delivery worker all run inside it, and
PostgreSQL is the only thing it talks to.

```
┌─────────────┐     ┌──────────────────────────────┐     ┌──────────────┐
│  Your App   │────▶│  mailyard serve              │────▶│  PostgreSQL  │
│  HTTP / SMTP│     │  api + console + smtp in     │◀────│  data +      │
└─────────────┘     │  delivery worker         out │     │  queue       │
                    └───────────────┬──────────────┘     └──────────────┘
                                    │
                                    ▼
                            ┌──────────────┐
                            │  SMTP Server │
                            └──────────────┘
```

The queue lives in the `emails` table. When one process is no longer enough, run several against the same database:
every node claims from that one queue, and each claim takes a disjoint batch. See
[Scaling out](/docs/getting-started/scaling).

## Next Steps

- [Installation](/docs/getting-started/installation) — Deploy Mailyard with Docker or from source
- [Configuration](/docs/getting-started/configuration) — Configure environment variables
- [Quick Start](/docs/getting-started/quickstart) — Send your first email in minutes
