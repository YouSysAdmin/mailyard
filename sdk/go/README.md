# Mailyard Go client

The Go client for the Mailyard API (`/api/v1`).

It comes in two halves. The `mailyard` package is hand-written and
small - sending, batching, typed errors, cursor paging - because those
are the calls most integrations make and a signature somebody chose
beats one somebody derived. `mailyard/api` is **generated** from the
route metadata and covers every endpoint, because the product surface
is two hundred routes and hand-writing that is how a client falls
behind its server. Reach it with `c.API()`.

```
go get github.com/yousysadmin/mailyard/sdk/go
```

A separate module from the server on purpose: importing a client must not drag in
Fiber, pgx, the AWS SDK and everything else a mail server needs. This module has **no
dependencies at all** - it is `net/http` and `encoding/json`.

## Sending

```go
package main

import (
    "context"
    "log"

    mailyard "github.com/yousysadmin/mailyard/sdk/go"
)

func main() {
    c := mailyard.New("https://mail.example.com", "myk_...",
        mailyard.WithUserAgent("billing-service/1.4"))

    res, err := c.Send(context.Background(), mailyard.SendRequest{
        From:    "billing@example.com",
        To:      []string{"customer@example.net"},
        Subject: "Your invoice",
        HTML:    "<p>Attached.</p>",
    })
    if err != nil {
        log.Fatal(err)
    }
    log.Printf("queued %s, %d recipients suppressed", res.Email.ID, len(res.Suppressed))
}
```

The key decides the project, so there is no header to set and no project id to pass.

Delivery is asynchronous: `Send` returns once the message is stored. Poll `Status` with
the returned id - it is cheaper than `GetEmail`, which carries the bodies.

## Errors

Refusals come back as `*mailyard.Error` carrying the HTTP status, so you can branch
without matching on message text:

```go
if _, err := c.Send(ctx, req); err != nil {
    switch {
    case mailyard.IsOverQuota(err):
        // The plan's limit for this window. Back off, do not drop the message.
    case mailyard.IsUnauthorized(err):
        // The key was rejected, or lacks the scope this route needs.
    default:
        var apiErr *mailyard.Error
        if errors.As(err, &apiErr) {
            for _, f := range apiErr.Fields {
                log.Printf("%s: %s", f.Field, f.Message)
            }
        }
    }
}
```

`IsNotFound` is also the answer to "that id is not mine": a resource in another project
reads as missing rather than forbidden, so the API never confirms an id exists.

## Suppressed recipients are not failures

A send to a blocked address succeeds for everyone else and reports who was dropped:

```go
res, err := c.Send(ctx, req)
if err != nil {
    return err
}
for _, addr := range res.Suppressed {
    log.Printf("not sent to %s: suppressed", addr)
}
```

`res.Suppressed` is always a slice, never nil, so an empty one needs no special case.

## Batches

One bad item does not sink the rest, so check the report rather than assuming:

```go
out, err := c.SendBatch(ctx, mailyard.BatchRequest{
    From:         "news@example.com",
    TemplateName: "monthly",
    Items:        items,
})
if err != nil {
    return err
}
for _, r := range out.Results {
    if r.Error != "" {
        log.Printf("item %d refused: %s", r.Index, r.Error)
    }
}
```

A key flagged **sandbox** is refused by `SendBatch` outright rather than delivering for
real - send the items individually if you need to exercise that key.

## Paging

Two kinds, matching the API. Lists bounded by what a person made take `limit`/`offset`.
Lists that grow per message - suppressions, bounces, webhook deliveries - are cursor
paged and carry no total:

```go
var cursor string
for {
    page, next, err := c.ListSuppressions(ctx, mailyard.SuppressionFilter{
        Limit: 100, Cursor: cursor,
    })
    if err != nil {
        return err
    }
    // ... use page
    if next == "" {
        break
    }
    cursor = next
}
```

## Everything else

`c.API()` returns the generated client: one method per route, sharing
this client's base URL, credential and `http.Client`.

```go
c := mailyard.New("https://mail.example.com", "myk_...")

tpl, err := c.API().CreateTemplate(ctx, api.TemplateCreateInput{
    Name: "welcome", Subject: "Hello", HTML: "<p>Hi</p>",
})

page, err := c.API().ListTemplates(ctx, api.Query("limit", "50"))
```

Query parameters are options rather than typed fields, because the
route metadata does not record them and inventing them would be
describing an API nobody wrote. Refusals are `*api.Error`, carrying the
HTTP status like the ergonomic half.

A **platform** credential (`mya_...`) works through the same client and
reaches the `/admin` methods. A project key is refused there however
wide its permissions are - admin is a different credential, not a
permission.

Regenerate after changing a route or a wire type:

```bash
task sdk-gen
```

`TestSDKCoversEveryV1Route` fails the build in both directions if you
forget - a route with no method, or a method calling a route that no
longer exists.

## Other languages

There is no client for them, and you do not need to wait for one. The binary writes an
OpenAPI description of itself, generated from the types its handlers return:

```bash
mailyard export-api-spec --out openapi.yaml
```

Feed that to your generator of choice. Because it is generated rather than transcribed,
it cannot describe a body the API does not actually send.

## Keeping up with the API

`TestSDKCoversEveryV1Route` in the server module parses these files and the router, and
fails the build when either grows a route the other does not know about - in both
directions. A method calling a path that no longer exists would otherwise compile
perfectly and fail in production.
