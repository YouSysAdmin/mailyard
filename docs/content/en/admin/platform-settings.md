---
title: "Platform Settings"
description: "Configure platform-wide settings"
weight: 20
---

Administrators configure platform-wide settings from the dashboard under **Admin -> Platform Settings**, or through
`GET/PUT /api/v1/admin/settings`.

These are the knobs that can change at runtime. Everything in `mailyard.yaml` (listeners, database, TLS, worker sizing)
needs a restart and is not settable here.

## Get Settings

```
GET /api/v1/admin/settings
```

Returns every key in the registry with its definition, its effective value, and whether that value is an administrator
override or the built-in default:

```json
{
    "settings": [
        {
            "key": "retention_days",
            "type": "int",
            "unit": "days",
            "default": "0",
            "value": "30",
            "overridden": true,
            "description": "Days to keep email log rows. 0 keeps them forever.",
            "updated_at": "2026-08-06T02:11:00Z",
            "updated_by": "admin@example.com"
        }
    ]
}
```

## Update Settings

```
PUT /api/v1/admin/settings
```

```json
{
    "settings": [
        {
            "key": "retention_days",
            "value": "60"
        },
        {
            "key": "maintenance_mode",
            "value": "true"
        }
    ]
}
```

Only the listed keys change. The whole batch is validated before anything is written, so a bad value in one entry
rejects the request rather than half-applying it. Writing a value equal to the default removes the override instead of
storing a redundant copy.

An unknown key is rejected. Settings exist only when something reads them, so the registry is the full list of what can
be set.

Both routes require the platform `admin` role.

## Available Settings

| Key                               | Type   | Default | Description                                                                                                                                                                                   |
|-----------------------------------|--------|---------|-----------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------|
| `retention_days`                  | int    | `30`    | Days to keep email log rows. `0` keeps them forever. The emails table is partitioned by week, so whole partitions past this window are dropped rather than deleted row by row.                |
| `email_body_retention_days`       | int    | `0`     | Days to keep rendered HTML and text on an email row. `0` follows `retention_days`.                                                                                                            |
| `email_attachment_retention_days` | int    | `0`     | Days to keep attachment bytes, including the blobs in object storage. `0` follows `retention_days`.                                                                                           |
| `inbound_retention_days`          | int    | `0`     | Days to keep received mail. `0` follows `retention_days`.                                                                                                                                     |
| `sandbox_retention_days`          | int    | `7`     | Days to keep a captured sandbox message. `0` keeps it until the per-project cap pushes it out. A sender may ask for a shorter window per message, never a longer one.                         |
| `sandbox_max_messages`            | int    | `500`   | How many sandbox messages one project keeps. The oldest are dropped past this. `0` is unlimited, which on a project wired into CI means the table grows until the disk says otherwise.        |
| `webhook_delivery_retention_days` | int    | `30`    | Days to keep webhook delivery history. `0` keeps it forever.                                                                                                                                  |
| `audit_log_retention_days`        | int    | `90`    | Days to keep audit log entries. `0` keeps them forever.                                                                                                                                       |
| `tracking_event_retention_days`   | int    | `0`     | Days to keep open and click events. `0` keeps them forever. Per-message and per-link counters are aggregated and always kept.                                                                 |
| `maintenance_mode`                | bool   | `false` | Refuse write requests from everyone but platform admins.                                                                                                                                      |
| `tls_certificate_server`          | string | -       | Name of the [managed certificate](/docs/admin/certificates) the HTTP listener serves. Empty falls through to ACME, then to the self-signed pair.                                              |
| `tls_certificate_submission`      | string | -       | Same, for the SMTP submission listener.                                                                                                                                                       |
| `tls_certificate_inbound`         | string | -       | Same, for the inbound MX listener.                                                                                                                                                            |
| `acme_enabled`                    | bool   | `false` | Order certificates from [Let's Encrypt](/docs/admin/certificates#acme).                                                                                                                       |
| `acme_hosts`                      | list   | -       | Hostnames to issue for. One per line in the console, a JSON array of strings over the API. A name that is not listed falls through to the self-signed pair rather than failing the handshake. |
| `acme_email`                      | string | -       | ACME account contact, where the CA sends expiry warnings. Used when the account is first registered.                                                                                          |
| `acme_directory_url`              | string | -       | A different ACME directory. Empty is Let's Encrypt production - point it at the staging one while working out why issuance fails.                                                             |
| `platform_mail_from`              | string | -       | Address the platform's own mail comes from - invitations, password resets, signup confirmations. Empty turns [platform mail](/docs/admin/system-mail) off.                                    |
| `platform_mail_from_name`         | string | -       | Display name beside that address.                                                                                                                                                             |
| `notification_retention_days`     | int    | `30`    | Days to keep notifications that have been read. Unread ones are always kept.                                                                                                                  |
| `bounce_alert_percent`            | int    | `10`    | Bounce rate over the last hour that raises a project alert. `0` turns the alert off.                                                                                                          |
| `bounce_alert_min_volume`         | int    | `20`    | How many sends must finish in the hour before the bounce rate is judged.                                                                                                                      |
| `relay_nodes_auto_approve`        | bool   | `false` | Let a relay node start delivering as soon as it enrols, without an admin approving it.                                                                                                        |
| `user_project_creation`           | bool   | `false` | Let any signed-in account create a project. Off, only platform administrators can, and everybody else joins by [invitation](/docs/projects/members-and-invitations). Administrators are never subject to it. |

{{< callout type="warning" title="Leave auto-approve off unless nodes are created automatically" >}}
A relay node in the sending pool receives the **content** of real messages to deliver, and every node enrols with the
same `relay_nodes.auto_register_token`. With approval in the way, a leaked token gets somebody a pending row an admin
will not recognise. Without it, it gets them a copy of everybody's mail. Turn it on only where nodes come and go on
their own, such as an autoscaling group.
{{< /callout >}}

{{< callout type="note" title="Nothing expires by default" >}}
Every retention window except the webhook delivery log starts at `0`, which means keep forever. An install that never
visits this page never loses data - and never reclaims space either. Set the windows deliberately.
{{< /callout >}}

### Retention behavior

- Content windows are **clamped** to `retention_days`. Keeping a body longer than the row that owns it is meaningless,
  so a body window longer than the metadata window is silently reduced to it.
- Attachment blobs are deleted from object storage **before** the database rows that hold their keys. A blob that will
  not delete is logged and skipped rather than blocking the sweep - the result is an orphaned object, not a stuck job.
- Emails still in flight (`queued`, `scheduled`, `processing`) are **never** purged, however old. Deleting one would
  strand work the delivery queue is about to claim.

The sweep runs as the `retention-cleanup` [scheduled job](/docs/admin/scheduled-jobs).

## Maintenance Mode

With `maintenance_mode` on, every mutating request (`POST`, `PUT`, `PATCH`, `DELETE`) on both the console API and the
machine API returns:

```
503 Service Unavailable
{ "error": "the platform is in maintenance mode, writes are temporarily disabled" }
```

Reads stay open so the console remains usable and an incident stays diagnosable, and platform admins are exempt
entirely - somebody has to be able to switch it back off.

The delivery worker and campaign runner keep draining whatever is already queued. Maintenance mode stops new work
arriving, it does not pause the pipeline.
