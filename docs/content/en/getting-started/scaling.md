---
title: "Scaling out"
description: "Run several nodes against one database, with PostgreSQL as the only coordinator"
weight: 60
---

One `mailyard serve` process is the whole system, and for most installations it stays that way. When it stops being
enough, **run more of them**. Several `serve` nodes against one database share the queue with no leader, no
registration and no partitioning to configure - PostgreSQL stays the only piece of infrastructure.

```yaml
services:
    mailyard:
        image: mailyard:latest
        command: [ "serve" ]
        env_file: mailyard.env
        deploy:
            replicas: 3
```

Every node accepts mail, and every node delivers it. A node that starts simply begins claiming, and the section below is
what makes that safe.

## How nodes stay out of each other's way

**Claiming.** A worker takes a batch with a single locking statement:

```sql
UPDATE emails
SET status = 'processing', ... WHERE id IN (
    SELECT id FROM emails
    WHERE status IN ('queued', 'scheduled') AND next_attempt_at <= $1
    ORDER BY next_attempt_at LIMIT $2
    FOR UPDATE SKIP LOCKED
    )
    RETURNING...
```

`SKIP LOCKED` is what makes extra workers cheap. A row another node is holding is invisible to the next claimer, so
nodes take **disjoint** batches instead of contending for the head of the queue, and adding a node adds throughput
rather than adding collisions. Nothing needs to be configured for this.

**Waking.** When a node accepts a send it fires a PostgreSQL `NOTIFY`, and every worker subscribed to that channel looks
at the queue immediately. Without it the node that accepted the mail would be the only one that knew, and every send
would wait out a poll interval tuned for keeping an idle cluster quiet.

Notifications are best effort and nothing depends on them: they are not durable, a reconnecting node misses whatever
fires meanwhile, and the poll loop remains the actual guarantee. A worker also re-checks the queue the moment it (re)
connects, so a backlog that built up while it was down drains on boot rather than on the next tick.

**Crash recovery.** A worker that dies mid-send leaves its rows in `processing`. Any node that polls after
`worker.claim_timeout` (default 5m) returns them to the queue. Keep that value comfortably above your slowest SMTP
delivery, or a slow send is re-queued while it is still running.

## The emails table is partitioned

`emails` is split into one partition per week, by `created_at`. It is the only table that grows per message, and the
reason is retention: removing a week of mail is `DROP TABLE`, not a `DELETE` over millions of rows. The difference at
volume is a long transaction, a WAL burst, a replication lag spike and weeks of autovacuum, versus none of that.

`retention_days` defaults to **30**, which is what the weekly width is sized for - about five partitions are live at a
time and retention drops one a week.

Two things follow that are worth knowing:

- **Partitions are created ahead of time**, four weeks out, by a job that runs hourly on every node that drains the queue, and once at start.
  If it stops running you have a month of warning. There is a default partition as a last resort, and rows landing in it
  are reported as a job failure - not because the rows are lost, but because each one blocks creating the proper
  partition for its week.
- **A partition is only dropped whole when nothing in it is still in flight.** A message scheduled for next month may
  have been created months ago, so its row lives in an old partition. Those partitions are left for the ordinary
  row-by-row delete, which skips in-flight mail exactly as it always did.

{{< callout type="note" title="Setting retention_days to 0" >}}
Zero still means keep forever, and it still works - the partitions simply accumulate. It is no longer the default,
because on a table that gains a row per message "keep everything" is a disk filling while nobody has decided anything.
{{< /callout >}}

## Read replicas

Every node talks to one PostgreSQL primary. Read-only followers can be added, and queries reach them only where the code
says so:

```yaml
database:
    dsn: postgres://mailyard@primary/mailyard
    replica_dsns:
        - postgres://mailyard@replica-a/mailyard
        - postgres://mailyard@replica-b/mailyard
```

Or `MAILYARD_DATABASE_REPLICA_DSNS`, comma separated. Several are round-robined. A follower that will not answer refuses
the boot, the same way a bind it cannot take does - three configured and two working is a thing you want to hear about
at start, not from a latency graph a month later.

### What actually moves

Configuring a follower routes the reads that scan a lot of rows and answer a retrospective question. Everything else
stays on the primary by construction.

| Group                 | Config key           | Default | Queries                                              |
|-----------------------|----------------------|---------|------------------------------------------------------|
| Dashboard aggregation | `analytics`          | on      | summary, daily trend, status breakdown               |
| Delivery log          | `email_log`          | on      | the log listing, status counts, the Prometheus gauge |
| Received mail         | `inbound_log`        | on      | the listing and status counts                        |
| Sandbox               | `sandbox`            | on      | the captured-message list and count                  |
| Bounces               | `bounces`            | on      | the bounce list                                      |
| Contacts              | `contacts`           | on      | the list, its search and count                       |
| Webhook deliveries    | `webhook_deliveries` | on      | the delivery history                                 |
| Audit                 | `audit_log`          | on      | the project trail and the security trail             |
| Suppressions          | `suppressions`       | **off** | the console list and search                          |

All nine do nothing until `replica_dsns` is set, so on an installation with no follower the defaults cost exactly
nothing.

### How the defaults were chosen

One question, asked of each console page: **does it write this list and then immediately read it back?** That, not the
size of the table, is what makes replication lag visible to a person. Everywhere the answer is no, the group is on.

Only `suppressions` answers yes. Adding a block and removing one both reload the list on the spot, so a follower that is
behind returns a list without the address you just blocked - and "is this address blocked" is the entire question that
page exists to answer. A wrong answer there is not a cosmetic delay.

Turn it on if your lag is small and the list is large, which it usually is:

```yaml
database:
    replica_reads:
        suppressions: true
```

Or `MAILYARD_DATABASE_REPLICA_READS_SUPPRESSIONS=true`.

{{< callout type="note" title="The sandbox looks risky and is not" >}}
It seems like the obvious one to turn off - send a test, look for it. But the console never writes those rows: an
application under test does, over SMTP or the API, and nobody watches the page while a suite runs. The gap between
capture and somebody looking is minutes, or never.
{{< /callout >}}

Fetching one record by id is **not** in the table, anywhere. A detail page is usually opened right after the write that
created the row, so those stay on the primary. So does anything that is a single indexed lookup rather than a scan -
there is nothing to gain by moving work that was already free.

### Reads that can never move

A query runs on a follower only where the code asked for it explicitly, and no setting above can change that. Reads that
must be current stay on the primary:

- the queue claim, and every transaction
- the quota count that decides whether a send is accepted
- session and 2FA resolution
- the suppression check before a message is queued
- the Message-ID and content-hash lookups that decide whether inbound mail is a duplicate
- uniqueness probes before an insert
- reading back a row the same request just wrote

Get any of those from a follower and the symptom is not an error. It is a message sent past its plan limit, the same
inbound message stored twice, or somebody logging in and being told "authentication required" for reasons nobody can
reproduce. So the default is the primary, and each redirect to a follower is a decision somebody made about one query.

{{< callout type="warning" title="LISTEN/NOTIFY does not work on a follower" >}}
Cross-node wake-ups travel over PostgreSQL `LISTEN`/`NOTIFY`, and notifications are **not** carried in the WAL. A
`LISTEN` on a standby is accepted and then never fires. Mailyard therefore builds that connection on the primary DSN
only. Nothing to configure - it is stated here because pointing the whole application at a follower through a pooler
would break wake-ups silently, and the queue would fall back to its poll interval with no error anywhere.
{{< /callout >}}

### Trying it locally

`../../../compose.yaml` carries an optional streaming replica:

```bash
task dev-replica    # adds one on :5433, without restarting the primary
task run-replica    # runs Mailyard with it configured
```

It is a real physical standby, which is the point - it is genuinely read-only, and `LISTEN`/`NOTIFY` genuinely does not
reach it, so the two properties worth testing against behave the way production would.

### What this actually buys

Reporting and log browsing: the dashboard aggregates, the delivery trend, and paging through emails, bounces and
suppressions. Those are the queries that touch the most rows.

It does **not** help the sending path. At a million messages a day the write volume - one insert plus a couple of status
updates per message - lands entirely on the primary, and no number of followers changes that. If the primary is the
thing that is struggling, replicas are the wrong lever.

## What is still per-node

Be honest with yourself about these before scaling out:

- **Rate limits** (`ratelimit.*`, and the SMTP listeners' per-IP limits) are counted per process. Three nodes mean
  three times the configured limit. Divide the values by the node count, or put the real limit at your load balancer.
- **Worker settings** are per node, as noted in
  [Configuration](/docs/getting-started/configuration). Three nodes at concurrency 4 deliver twelve at a time.
- **Live console updates** (the SSE stream) only carry events raised on the node the browser is connected to, so a live
  view is partial. The durable record - the email log, the notifications table - is always complete, and a reload shows
  everything.
- **Caches converge, they are not shared.** Session revocations propagate within 15 seconds, platform settings within 5
  minutes. A revoked session may survive briefly on another node.
- **Scheduled jobs run on every worker**, with no leader election. Every job is an idempotent delete-by-age sweep, which
  is what makes that safe - keep it that way if you add one.

## Sizing

The queue is not the bottleneck and will not be for a long time: claiming is one locking statement that PostgreSQL
serves thousands of times a second, while delivery waits on remote SMTP servers that accept messages orders of magnitude
more slowly. Scale on **delivery** concurrency, not on queue throughput.

Watch `mailyard_emails_by_status{status="queued"}` (see
[Prometheus metrics](/docs/analytics/prometheus-metrics)). A backlog that grows across a normal sending window means
more delivery capacity - more workers, or more concurrency per worker - not a faster queue.
