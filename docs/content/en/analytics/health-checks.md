---
title: "Health Checks"
description: "Liveness and readiness probes"
weight: 40
---

Two probes, both open and both cheap. Neither requires a credential - a probe target that needs one is not a probe
target.

## Liveness

```
GET /healthz
```

```json
{
    "status": "ok"
}
```

**Checks nothing on purpose.** If the HTTP server can answer, the process is alive. Putting a dependency check here
would turn a database blip into a restart loop, which is the one response that cannot help.

## Readiness

```
GET /readyz
```

```json
{
    "status": "ok",
    "checks": {
        "database": "ok"
    }
}
```

Pings the database with a two-second timeout. On failure it returns **503** with the failing check named, so the
instance leaves the load balancer's rotation without being killed:

```json
{
    "status": "unavailable",
    "checks": {
        "database": "unreachable: dial tcp ...: connect: connection refused"
    }
}
```

The database is the only hard dependency, which is why the probe checks it and nothing else: there is nothing else that
can be unreachable while the process still answers. The worker, the scheduler and system mail are all in-process, and
every node serves these same probes.

{{< callout type="note" title="Why the split matters" >}}
Liveness answers "is this process wedged, restart it?". Readiness answers "can this instance serve traffic right now?".
Conflating them means a shared database outage restarts every replica instead of just draining them until the database
returns.
{{< /callout >}}

## Kubernetes

```yaml
livenessProbe:
    httpGet:
        path: /healthz
        port: 3000
    initialDelaySeconds: 5
    periodSeconds: 10

readinessProbe:
    httpGet:
        path: /readyz
        port: 3000
    initialDelaySeconds: 10
    periodSeconds: 15
```

## Docker Compose

```yaml
healthcheck:
    test: [ "CMD", "curl", "-f", "http://localhost:3000/healthz" ]
    interval: 30s
    timeout: 10s
    retries: 3
```

## Application info

There is no `/api/v1/info` endpoint. The running version is shown in the console footer and, when metrics are enabled,
on the [Prometheus endpoint](/docs/admin/platform-metrics).
