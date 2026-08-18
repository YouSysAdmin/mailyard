---
title: "Email Verification"
description: "Validate an email address before sending"
weight: 80
---

Check whether an email address is valid and likely deliverable before you send to it. Verification runs a series of
cheap-to-expensive checks (syntax, your suppression/bounce history, disposable/role detection, and an MX lookup) and
caches results to avoid repeated lookups.

```
POST /api/v1/emails/verify
```

Needs `emails:read`. Disabled by default — set `email_verify.enabled` to turn it on. The MX check makes outbound DNS
queries, which an operator on a locked-down network should opt into rather than discover.

{{< callout type="note" >}}
No SMTP probe is performed, so mailbox existence is never confirmed — `checks.smtp` is always reported as
`"skipped"`. A `valid` result means the address is syntactically correct and the domain accepts mail, not that the
specific mailbox exists.
{{< /callout >}}

## Request

```bash
curl -X POST http://localhost:3000/api/v1/emails/verify \
  -H "Authorization: Bearer myk_..." \
  -H "Content-Type: application/json" \
  -d '{"email": "j.okafor@acme-industrial.example"}'
```

One field, `email`, required. It is checked for a valid address shape by the request validator before anything else
runs, so a malformed string is a `400` and never reaches a DNS lookup.

Add `?fresh=true` to skip the cache and recompute. Use it when you have just fixed a domain's DNS and want the answer
now — not in a loop over a list, which is what the cache exists to protect your resolver from.

## What it checks

The intrinsic checks run cheapest first and stop as soon as the answer is settled:

1. **Syntax.** The address is parsed. A malformed one is `invalid` at score 0, and nothing else runs.
2. **Disposable domain.** Matched against a set of known throwaway providers. A hit is `disposable` at score 10, without
   a DNS lookup.
3. **Role account.** The local part is matched against role names such as `info`, `admin` or `support`. This does not
   short-circuit — it is recorded and applied at the end.
4. **Mail exchangers.** The domain's MX records are resolved, falling back to A and AAAA, since RFC 5321 treats a
   domain with only an address record as having an implicit MX. No mail servers means `invalid` at score 0.

Whatever survives that is `risky` at 60 if it was a role account, and `valid` at 90 otherwise.

**Your project's own history is applied last, on top of the result.** Suppression and hard-bounce state are re-read on
every call — never cached — and either one forces the verdict to `invalid` at score 0, with a reason naming which. So a
cached `valid` cannot outlive a suppression you added a second ago.

## Cache behavior

- The **intrinsic** result (syntax, disposable, role, MX) is cached in process memory per address, and MX answers are
  cached per domain (`email_verify.cache_ttl`, `email_verify.mx_cache_ttl`). The same address and domain are not
  re-checked on every call. On a multi-node deployment each node keeps its own cache.
- The **per-project** flags (`suppressed`, `previously_bounced`) are always re-evaluated on each request and layered
  onto the (possibly cached) intrinsic result, so they reflect your current state even on a cache hit.
- `cached` is `true` in the response when the intrinsic result came from the cache.
- Pass `?fresh=true` to bypass the cache and recompute the intrinsic result.

## Response

```json
{
  "verification": {
    "email": "support@acme-industrial.example",
    "status": "risky",
    "score": 60,
    "reason": "this looks like a role account rather than a person",
    "checks": {
      "syntax": true,
      "mx": true,
      "disposable": false,
      "role_account": true,
      "smtp": "skipped"
    },
    "mailbox_verified": false,
    "suppressed": false,
    "previously_bounced": false,
    "cached": true,
    "checked_at": "2026-05-31T12:00:00Z"
  }
}
```

{{< callout type="warning" title="A transient DNS failure is `unknown`, not `invalid`" >}}
If the domain's servers cannot be resolved right now - a timeout or SERVFAIL rather than a definitive "no such domain" -
the verdict is `unknown` with a score of 50, and the result is **not cached**. Treating a bad minute on your resolver as
proof that an address is dead would suppress real customers, and caching it would make that stick for a day.
{{< /callout >}}

### The verdict

`status` and `score` always agree, so branch on whichever suits your code:

| `status` | `score` | Reached when |
|---|---|---|
| `valid` | 90 | Syntax is good, the domain takes mail, the local part is not a role |
| `risky` | 60 | The same, but the local part looks like a role rather than a person |
| `unknown` | 50 | The lookup itself failed. Not evidence of anything |
| `disposable` | 10 | The domain is a known throwaway provider |
| `invalid` | 0 | Bad syntax, no mail servers, suppressed, or hard-bounced for you |

`reason` carries a sentence explaining anything that is not plainly valid, and is omitted when there is nothing to
explain.

### The rest of the body

`email` is the address as it was normalized — trimmed and lowercased — which is what the verdict applies to.
`checked_at` is when the intrinsic result was computed, so on a cache hit it is older than the request. `cached` says
which of the two you got.

`suppressed` and `previously_bounced` are your project's own facts, always fresh. `mailbox_verified` is permanently
`false` and exists precisely so nothing can read a `valid` verdict as proof a mailbox is there.

`checks` breaks out what was actually established: `syntax`, `mx`, `disposable` and `role_account` as booleans, plus
`smtp`, which is the string `"skipped"` and always will be.

## Refusals

| Status | When |
|---|---|
| `400` | `email` is missing or not a valid address |
| `400` | Verification is disabled on this install — the message names `email_verify.enabled` |

There is no rate limit of its own on this route. The MX cache is what keeps a loop over a large list from becoming a
loop over your resolver, so leave `fresh` alone unless you have a reason.
