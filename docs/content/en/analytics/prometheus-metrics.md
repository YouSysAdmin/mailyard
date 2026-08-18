---
title: "Prometheus Metrics"
description: "Production monitoring with Prometheus"
weight: 30
---

```
GET /metrics
```

Opt in with `metrics.enabled`. The route is not registered at all when it is off, so a disabled installation answers
`404` rather than an empty scrape.

```bash
MAILYARD_METRICS_ENABLED=true
MAILYARD_METRICS_TOKEN=a-long-random-string
```

{{< callout type="warning" title="Set a token unless the port is private" >}}
`metrics.token` gates the endpoint behind `Authorization: Bearer <token>`. Leave it empty only when nothing outside your
network can route to the process — a scrape reveals your sending volume, queue depth and failure rates to anyone who
asks for it.

The comparison is constant-time, like every other credential check here, so it does not leak the length of a matching
prefix.
{{< /callout >}}

The endpoint lives at the root, not under `/api/v1`, and it is one of the few things a
[worker node](/docs/getting-started/scaling) serves — a node that only sends is the one whose metrics matter most.

## Counters

| Metric | Labels | Counts |
|---|---|---|
| `mailyard_emails_accepted_total` | — | Sends accepted into the queue, from every surface: the API, the console, SMTP submission, campaigns |
| `mailyard_emails_finalized_total` | `status` | Messages reaching a terminal state — `sent`, `failed` or `suppressed` |
| `mailyard_sandbox_captures_total` | — | Messages captured into a [sandbox](/docs/email-sending/sandbox) instead of delivered |
| `mailyard_inbound_received_total` | — | [Inbound](/docs/inbound/overview) messages stored for a verified domain |
| `mailyard_webhook_deliveries_total` | `status` | [Webhook](/docs/webhooks/overview) delivery attempts by outcome |

Sandbox captures are counted separately rather than as a label on accepted mail. They never enter the queue, and folding
them in would make a CI run that sends ten thousand test messages look like sending volume.

## Gauges

These are sampled **at scrape time** rather than tracked continuously, so they always reflect the moment you asked.

| Metric | Labels | Reports |
|---|---|---|
| `mailyard_emails_by_status` | `status` | Current email rows per status, across all projects |
| `mailyard_email_partitions` | — | Daily partitions on the emails table |
| `mailyard_email_partitions_ceiling` | — | The count past which concurrent queue claims start failing |

`mailyard_emails_by_status{status="queued"}` is your queue depth, and the one to alert on: sending capacity is fine
until it is not, and this is where that shows first.

The partition gauges are published as a **pair** on purpose. The ceiling is overridable per installation, so an alert
written against a literal number is wrong on any install that raised it. Write the ratio instead and it stays correct
wherever it is deployed:

```
mailyard_email_partitions / mailyard_email_partitions_ceiling > 0.8
```

That one is worth having. Crossing the ceiling does not degrade gradually — concurrent claims begin failing outright
with "out of shared memory", and the fix is a [retention window](/docs/admin/platform-settings) or a higher
`max_locks_per_transaction`, neither of which is instant.

{{< callout type="info" title="A gauge collector cannot hang your scrape" >}}
The partition sample is bounded by a five second timeout, and a collector that errors publishes nothing rather than
failing the response.

That matters because `/metrics` is exactly what an operator reads when the database is in trouble. An unbounded catalog
query would hang the one endpoint that could tell them why.
{{< /callout >}}

The standard Go runtime and process collectors come along as well, so `go_goroutines`, `go_memstats_*` and
`process_resident_memory_bytes` are available without any extra configuration.

## Scraping

```yaml
scrape_configs:
  - job_name: mailyard
    metrics_path: /metrics
    authorization:
      credentials: a-long-random-string
    static_configs:
      - targets: ['mailyard:3000']
```

Every node exposes its own numbers — the counters are per process and are not aggregated for you. Scrape each node and
sum in Prometheus with `sum(rate(...))`, which is what you want anyway: a per-node breakdown is how you notice one
worker doing nothing.

## Worth graphing

- **Accepted against finalized.** Divergence means the queue is filling faster than it drains.
- **`mailyard_emails_finalized_total{status="failed"}` as a share of the total.** A step change usually means one SMTP
  route broke, not that mail generally got worse.
- **Queue depth over time**, with an alert on sustained growth rather than on a single spike.
- **Webhook failures.** These fail quietly by design and nothing in the console shouts about them.
- **Partition ratio**, as above.

For per-project numbers rather than per-process ones, use the [dashboard](/docs/analytics/dashboard) and the
[analytics API](/docs/analytics/email-analytics) — Prometheus metrics are deliberately installation-wide and carry no
project label, because a label with unbounded cardinality is how a metrics database falls over.
