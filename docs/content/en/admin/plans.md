---
title: "Plans & Quotas"
description: "Configure plans with sending limits and resource quotas"
weight: 70
---

A plan is a named set of limits attached to a **project**. Administrators create plans and assign them, and the sending
path enforces them on every message.

{{< callout type="info" title="Plans attach to projects, not to accounts" >}}
There is no per-user plan. A person's limits are whatever the project they are sending from carries, which is what makes
one account able to sit in a generous project and a restricted one at the same time.
{{< /callout >}}

A value of `0` means **unlimited** for every limit below.

## Plan Fields

| Field                | Default | What it caps                                    |
|----------------------|---------|-------------------------------------------------|
| `name`               | —       | Required, unique                                |
| `description`        | —       | Free text                                       |
| `is_default`         | `false` | The plan used by any project with none assigned |
| `hourly_email_limit` | `0`     | Emails accepted per rolling hour                |
| `daily_email_limit`  | `0`     | Emails accepted per rolling day                 |
| `max_api_keys`       | `0`     | API keys per project                            |
| `max_smtp_servers`   | `0`     | SMTP servers per project                        |
| `max_domains`        | `0`     | Verified domains per project                    |
| `max_subscribers`    | `0`     | Subscribers per project                         |

The two send limits are checked when a message is accepted and refused with HTTP `429`
(or `452` over SMTP submission). The resource caps are checked at create time, so you find out when adding the key or
the domain, not later. Counts come from the primary tables rather than from counters, so nothing can drift out of step
with reality.

## Creating a Plan

```
POST /api/v1/admin/plans
```

```json
{
    "name": "Pro",
    "description": "For growing teams",
    "hourly_email_limit": 1000,
    "daily_email_limit": 10000,
    "max_api_keys": 20,
    "max_domains": 10,
    "max_smtp_servers": 5,
    "max_subscribers": 50000
}
```

## Listing Plans

```
GET /api/v1/admin/plans
```

Returns `{"plans": [...]}`. There is no fetch-one route - the list is short and the console renders from it.

## Updating a Plan

```
PATCH /api/v1/admin/plans/{id}
```

Takes the same body as create. Setting `is_default` here is how a plan becomes the default, and the previous default is
unset in the same write.

## Deleting a Plan

```
DELETE /api/v1/admin/plans/{id}
```

Answers `204`. Projects that carried the plan are moved to no plan in the same transaction, so they fall back to the
default rather than pointing at a plan that no longer exists.

## Assigning a Plan to a Project

```
PATCH /api/v1/admin/projects/{id}/plan
```

```json
{
    "plan_id": "3f2504e0-4f89-11d3-9a0c-0305e82c3301"
}
```

Platform admin only. An empty `plan_id` clears the assignment, which puts the project back on the default plan. An
unknown plan id is `404` and nothing is written.

## Reading Limits and Consumption

```
GET /api/v1/usage
```

Available to any project member, scoped by the usual project header. It reports the effective plan together with current
consumption, which is what the console renders on the usage screen and what tells you how close a project is to its
ceiling.
