---
title: "Email Analytics"
description: "Detailed email analytics and trends"
weight: 20
---

The delivery trend over a date range.

```
GET /api/v1/analytics?from=2026-07-01&to=2026-07-31&status=sent
```

Also available as `GET /api/v1/analytics` with an API key holding `analytics:read`.

| Parameter | Default             | Description                                  |
|-----------|---------------------|----------------------------------------------|
| `from`    | 30 days before `to` | Start date, `YYYY-MM-DD`, inclusive          |
| `to`      | today               | End date, `YYYY-MM-DD`, inclusive            |
| `status`  | -                   | Narrow `daily_counts` to one delivery status |

```json
{
    "daily_counts": [
        {
            "date": "2026-07-01",
            "count": 153
        },
        {
            "date": "2026-07-02",
            "count": 0
        },
        {
            "date": "2026-07-03",
            "count": 205
        }
    ],
    "status_breakdown": {
        "sent": 14890,
        "failed": 230,
        "queued": 12
    },
    "from": "2026-07-01",
    "to": "2026-07-31"
}
```

Both dates are inclusive: a range of `2026-07-01` to `2026-07-01` covers that whole day.

{{< callout type="note" title="Every day is present" >}}
`daily_counts` includes days with **zero** emails. A chart fed only the days that had traffic silently rescales its
x-axis, which makes a quiet week look like a busy one.
{{< /callout >}}

`status_breakdown` always covers every status in the range, regardless of the `status`
filter - the filter narrows the trend line, not the summary beside it.

## Limits

A range may not exceed **366 days**. Beyond that the response stops being a chart and starts being a denial of service
dressed as a date picker. Longer periods should be aggregated by whatever consumes this.

Bad input is refused rather than silently coerced:

```json
{
    "error": "from must be a date in YYYY-MM-DD form"
}
{
    "error": "from must be before to"
}
{
    "error": "the range must not exceed 366 days"
}
{
    "error": "unknown status bogus"
}
```
