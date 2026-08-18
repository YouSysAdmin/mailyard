---
title: "Scheduled Jobs"
description: "View and manage scheduled background jobs"
weight: 50
---

Mailyard runs scheduled background jobs for maintenance tasks. All times are UTC.

## List Jobs

```
GET /api/v1/admin/jobs
```

Response:

```json
{
    "jobs": [
        {
            "name": "retention-cleanup",
            "schedule": "0 3 * * *",
            "running": false,
            "last_run_at": "2026-08-06T03:00:00Z",
            "last_error": "",
            "next_run_at": "2026-08-07T03:00:00Z",
            "last_duration_ms": 412
        }
    ]
}
```

## Run a Job Now

```
POST /api/v1/admin/jobs/{name}/run
```

Runs the job immediately, out of band, and returns the refreshed job list. A job that is already in flight is rejected
rather than started twice. Both routes require the platform
`admin` role.

Each entry carries `name`, which is what the run route takes, and `schedule`, which is display only — see the
scheduling model below.

The rest is state, and all of it belongs to **the node that answered the request**: `running` says whether it is in
flight there, `last_run_at` and `last_duration_ms` describe that node's previous run, and `next_run_at` is when it will
go again. Both timestamps are null until the job has run once since that process started, so a freshly restarted node
reports nulls rather than history.

`last_error` is empty when the last run succeeded, and holds the failure message otherwise. It is the only place a
failed sweep surfaces — a job that errors is logged and retried on its next tick, and nothing raises an alert about it.

## Built-in Jobs

| Job                 | Schedule           | Description                                                                                                                                                                                                       |
|---------------------|--------------------|-------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------|
| `retention-cleanup` | Daily at 03:00 UTC | Purges expired email logs, received mail, attachment blobs, webhook deliveries, and tracking events according to the [retention settings](/docs/admin/platform-settings). Also drops spent password reset tokens. |
| `bounce-alert`      | Every 15 minutes   | Judges the recent bounce rate and raises a notification when it crosses the configured threshold. A rate is a property of a window, so it cannot be evaluated from the delivery path.                             |
| `settings-refresh`  | Every 5 minutes    | Reloads platform settings from the database so a node that did not serve the write converges.                                                                                                                     |

## Scheduling Model

Schedules come in two shapes: **daily at a fixed UTC time**, and **every N**. There is no cron-expression parser - the
`schedule` field renders cron syntax where it maps purely so the value is familiar to read.

Jobs do not backfill. A node that is down at 03:00 does not run the sweep late, it runs it at 03:00 the next day.

{{< callout type="warning" title="Every node runs every job it registers" >}}
There is no leader election. Each node runs its full job set on its own schedule. That is safe for the built-in jobs
because they are all delete-by-age statements, which converge rather than conflict - but any job added later must be
idempotent and tolerate concurrent execution.
{{< /callout >}}

Which jobs a node registers depends on its role. The maintenance sweeps -
`retention-cleanup` and `bounce-alert` - are registered by `serve` and `worker` nodes only. `settings-refresh` is
registered by **every** role including `api`, because it is not maintenance: it is how a node's settings cache learns
about a change written somewhere else. So `GET /api/v1/admin/jobs` on an `api` node lists one job, and that is correct.

A panicking job is contained: the failure is recorded as that job's `last_error` and the scheduler keeps running.
