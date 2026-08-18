---
title: "A/B Testing"
description: "Test email variants with split audience campaigns"
weight: 30
---

An A/B test splits one campaign's audience across up to five variants, each of which can change the subject line, the
template, or both. Everything else about the send stays identical, which is what makes the comparison mean anything.

## Configuring one

Variants are part of the campaign, so they are set at create time on a `draft`:

```bash
curl -X POST http://localhost:3000/api/v1/campaigns \
  -H "Authorization: Bearer myk_..." \
  -H "Content-Type: application/json" \
  -d '{
    "name": "April dispatch - subject test",
    "from_email": "news@example.com",
    "template_id": "0198f6a1-3c7e-7b21-9f4d-2a5c8e0b1d33",
    "list_id": "0198f6a2-7b19-7d02-9c31-4e8f1a6b3c22",
    "ab_test_enabled": true,
    "ab_variants": [
      { "name": "question", "subject": "Ready for April?",        "split_percentage": 50 },
      { "name": "direct",   "subject": "Your April dispatch",     "split_percentage": 50 }
    ]
  }'
```

## What a variant may change

| Field | Notes |
|---|---|
| `name` | **Required**, 1-50 characters. It is the label in the results, so make it describe the arm |
| `split_percentage` | **Required**, 1-100 |
| `subject` | An alternative subject line. Rendered against the same data as the body |
| `template_id` | An entirely different template for this arm |

`subject` is the usual test and the cheap one. `template_id` swaps the whole body, which tests layout or offer rather
than wording — worth knowing that it makes the two arms differ in more ways than one, so a result tells you which
template won and not why.

A variant that sets neither is a control: it sends exactly what the campaign would have sent anyway, which is a
legitimate arm to include.

## The rules

- **At least two variants.** One arm is not a test.
- **At most five.**
- **The splits may not exceed 100.**

{{< callout type="info" title="They do not have to add up to 100" >}}
Only exceeding 100 is refused. Splits of 10 and 10 are accepted, and the **remaining 80% goes to the last variant** —
whatever is left over always does.

So a 10/10 split is not a small test on a large list. It is a 10/90 split with a misleading configuration. If you want a
holdout, write it as an explicit arm.
{{< /callout >}}

## How the split is made

At fan-out, the resolved audience is shuffled and then sliced in the order the variants are listed. The shuffle is what
makes the arms comparable — without it the split would follow whatever order subscribers happened to be created in,
which correlates with how long they have been a customer.

Every recipient gets exactly one variant, assigned once, before any mail is queued. A variant whose share rounds down to
zero recipients still gets one, so a tiny list does not silently drop an arm.

## Reading the results

Per-variant numbers are on the **campaign** route, not the analytics one:

```
GET /api/v1/campaigns/{id}
```

```json
{
  "stats": { "sent": 4600, "failed": 50, "skipped": 50 },
  "stats_by_variant": {
    "question": { "sent": 2300, "failed": 25 },
    "direct":   { "sent": 2300, "failed": 25 }
  },
  "engagement": { "opened": 939, "clicked": 217 }
}
```

{{< callout type="warning" title="`stats_by_variant` counts delivery, not engagement" >}}
It breaks down the **message statuses** per arm — `sent`, `failed`, `skipped`, `queued`, `pending` — and nothing else.
`engagement` sits beside it and is campaign-wide, so it cannot answer which arm did better.

Which is the whole question an A/B test asks. To answer it, export the per-recipient rows and group them yourself.
{{< /callout >}}

```
GET /api/v1/campaigns/{id}/messages?limit=200
```

Each row carries `variant` alongside `opened_at` and `clicked_at` — first-event stamps, so a row either has one or does
not. Counting non-null stamps per variant, over rows whose status is `sent`, gives you the open and click rate per arm.

`GET /api/v1/campaigns/{id}/analytics` is a third readout — per-link click tallies and daily series for charting — and
it is not broken down by variant either.
