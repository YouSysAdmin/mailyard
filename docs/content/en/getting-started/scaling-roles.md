---
title: "Splitting the roles"
description: "Run the API and the delivery worker as separate nodes"
weight: 61
---

`serve` runs both halves in one process, and several `serve` nodes against one
database already share the queue exactly as described in
[Scaling out](/docs/getting-started/scaling). What is on this page is running the
two halves on separate MACHINES, not the ability to run more than one node.

## The three commands

| Command           | HTTP        | SMTP listeners | Delivery queue | Campaigns | Maintenance jobs |
|-------------------|-------------|----------------|----------------|-----------|------------------|
| `mailyard serve`  | full        | yes            | yes            | yes       | yes              |
| `mailyard api`    | full        | yes            | no             | no        | no               |
| `mailyard worker` | probes only | no             | yes            | yes       | yes              |

`mailyard sender` is an alias for `mailyard worker`.

A role is a subcommand rather than a config key on purpose. The point of splitting roles is to put more machines behind
one queue, and a config key would mean every one of those machines needs its own config file differing in a single line.
Instead every node ships the **same** `../../../mailyard.yaml` (or the same
`MAILYARD_*` environment) and the role is a word in argv, which is already per-container in every orchestrator.

```yaml
services:
    api:
        image: mailyard:latest
        command: [ "api" ]
        env_file: mailyard.env
        ports: [ "3000:3000" ]

    worker:
        image: mailyard:latest
        command: [ "worker" ]
        env_file: mailyard.env
        deploy:
            replicas: 3
```

Add worker replicas freely. There is no leader, no registration and no partitioning to configure - a worker that starts
simply begins claiming.

## What each role does

**`api`** accepts mail and hands it to the database. `POST /api/v1/emails/send`, the console, the tracking endpoints,
the SMTP relay and the inbound MX listener all live here. It never delivers anything.

**`worker`** drains the queue, runs campaigns, and performs the scheduled sweeps (`retention-cleanup`, `bounce-alert`).
It binds `server.addr` but serves only
`/healthz`, `/readyz` and `/metrics` - enough for an orchestrator probe and a Prometheus scrape, without putting the
console on a machine that has no reason to expose it.

Note the consequence: **an `api`-only deployment never sends anything.** Mail piles up in the queue with status `queued`
until a worker exists.

The `settings-refresh` job runs on **every** role, including `api`. It is not maintenance - it is how a node's settings
cache learns about a change written on another node.

