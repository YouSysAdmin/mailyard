---
title: "Email Sandbox"
description: "Capture mail from a staging application instead of delivering it"
weight: 90
---

The sandbox captures mail instead of sending it. A staging application points at Mailyard with a **sandbox credential**,
and every message it produces is stored in full - text part, HTML part, headers, raw bytes, attachments - and delivered
to nobody.

It exists for the moment before a feature is real: a signup flow being wired up, a password reset being reworded, a
template being checked against a dozen locales. All of that wants to see the message. None of it wants a customer to
receive one.

## The credential decides, not the code

A sandbox credential is an ordinary API key or SMTP submission credential with one flag set at creation. That is the
whole configuration.

```
SMTP_HOST=mail.example.com
SMTP_PORT=587
SMTP_USER=smtp_a1b2c3...        # <- the only line that differs
SMTP_PASS=...
```

The application is byte for byte the same as production, which is what makes testing it worth anything. The
alternative - a flag in the request body - eventually gets left `true` on a production deploy, or `false` on staging,
and neither mistake is visible until it is expensive.

The switch is **one-way**. A sandbox credential cannot ask to send for real:

```json
{
    "from": "...",
    "to": [
        "..."
    ],
    "sandbox": false
}
```

```
400  this api key is sandbox-only, so sandbox cannot be set to false -
     use a key without the sandbox flag to send for real
```

Refused rather than quietly ignored, so nobody walks away believing a message went out. An ordinary credential may still
opt **in** for a single message, which is the direction that cannot leak real mail.

## Creating a sandbox credential

Either kind works, and both are created the same way as their live counterparts:

- **Developers -> API Keys -> Create**, tick **Sandbox key**
- **Developers -> SMTP Submission -> Create**, tick **Sandbox credential**

The flag cannot be changed afterwards. Create a second credential rather than flipping one, so that no single edit can
turn every test into a real send.

## What is not checked

A sandbox message skips the entire delivery pipeline. It is captured **before**
validation, which means none of the following apply:

| Normally required                                  | In the sandbox           |
|----------------------------------------------------|--------------------------|
| A verified sender domain                           | Any `From` address works |
| An enabled SMTP server or the shared pool          | None needed              |
| The suppression list                               | Ignored                  |
| The plan's hourly and daily limits                 | Not consumed             |
| A registered sender address under `strict_senders` | Not enforced             |

That is the point rather than a shortcut. A developer testing a signup flow in a project with nothing configured yet
fails every one of those checks, and refusing them would be refusing the feature.

The consequence worth knowing: **a sandbox message proves nothing about deliverability**. It says what your application
composed. Whether a real receiver would accept it is a question only a real send answers.

## Reading a captured message

**Developers -> Email Sandbox**. Each message opens on five views:

- **HTML** - rendered in a fully sandboxed frame, with no scripts and no access to the console's origin. It is your
  application's markup, and it is being displayed inside the tool you are signed in to.
- **Text** - the plain text alternative, if the message carries one.
- **Headers** - everything parsed out, including your own `X-` headers.
- **Attachments** - name, type and size, each downloadable.
- **Raw** - the wire bytes exactly as submitted.

The detail page shows the **SMTP envelope** separately from the `From` and `To`
headers. They routinely differ - a `Bcc` recipient appears in the envelope and in no header at all - and that difference
is usually the thing being debugged.

## How long a message is kept

Two limits, and the second is the one that matters in practice.

| Setting                  | Default | Meaning                                        |
|--------------------------|---------|------------------------------------------------|
| `sandbox_retention_days` | `7`     | Days before a captured message expires         |
| `sandbox_max_messages`   | `500`   | How many a project keeps, oldest dropped first |

Both are platform settings, under **Admin -> Settings**.

The day window alone does not bound anything useful: a test suite can write ten thousand messages in a morning, and a
seven day window does nothing about that until day seven. The per-project cap is what actually holds the table down, and
it is applied on every capture.

### Shortening the window for one message

A sender may ask for **less** retention, never more:

```json
{
    "from": "noreply@example.com",
    "to": [
        "test@example.com"
    ],
    "subject": "CI run 4821",
    "text": "...",
    "sandbox_retention_days": 1
}
```

Over SMTP the same thing rides on a header:

```
X-Mailyard-Sandbox-Retention: 1
```

A value longer than the platform window is clamped down to it rather than refused, so no application can pin a project's
sandbox open against the operator's setting.

Changing `sandbox_retention_days` governs **new** messages. What is already captured keeps the expiry it was given, so a
settings change cannot delete a message somebody is in the middle of reading.

## Sending into the sandbox

### Over SMTP

Exactly as [SMTP Submission](/docs/security/smtp-submission) describes, with a sandbox credential. The server answers
`250` - the message was accepted, by a credential whose meaning is "accept and keep", so anything else would make every
test look like a failure to the library that sent it.

An ordinary credential can opt in for one message:

```
X-Mailyard-Sandbox: true
```

Neither control header is stripped from the stored message, unlike on the sending path. The sandbox exists to show
exactly what went on the wire, and quietly editing that would hide the header you are trying to confirm you set.

### Over HTTP

`POST /api/v1/emails/send` and `POST /api/v1/emails/send-template` both capture.

With a **sandbox key** the request is the ordinary one - that is the entire point. Nothing in the body says sandbox, so
the application under test is untouched and only the credential it was given differs:

```bash
curl -X POST https://mail.example.com/api/v1/emails/send \
  -H "Authorization: Bearer myk_your_sandbox_key" \
  -H "Content-Type: application/json" \
  -d '{
    "from": "noreply@example.com",
    "to": ["customer@example.com"],
    "subject": "Order #42 confirmed",
    "html": "<p>Thanks for your order.</p>"
  }'
```

An **ordinary key** can opt one message in with a body field. This is the ad-hoc case

- a single call you want to inspect - not the way to run a test suite:

```bash
curl -X POST https://mail.example.com/api/v1/emails/send \
  -H "Authorization: Bearer myk_your_key" \
  -H "Content-Type: application/json" \
  -d '{
    "from": "noreply@example.com",
    "to": ["customer@example.com"],
    "subject": "Order #42 confirmed",
    "html": "<p>Thanks for your order.</p>",
    "sandbox": true
  }'
```

There is no `X-Mailyard-Sandbox` header on this surface. That header belongs to SMTP submission above, where a message
has headers and no JSON body to put a field in.

The response is a `201` carrying the sandbox row rather than an email row:

```json
{
    "sandboxed": true,
    "sandbox_email": {
        "id": "3f7c...",
        "source": "api",
        "sender": "noreply@example.com",
        "recipients": [
            "test@example.com"
        ],
        "subject": "Welcome",
        "size": 1284,
        "expires_at": "2026-08-15T09:12:00Z",
        "received_at": "2026-08-08T09:12:00Z"
    }
}
```

Deliberately not shaped like a send: the id belongs to the sandbox, and looking it up under `/api/v1/emails` returns
`404`. A response that invited that mistake would be worse than one that plainly says what happened.

A template send is **rendered first** and then captured, so what you read is what the template produced for that data -
placeholders resolved, template attachments included.

{{< callout type="note" title="Batch is not supported" >}}
`POST /api/v1/emails/batch` refuses a sandbox key with a `400` rather than falling through to a real send. Send the
items individually.
{{< /callout >}}

## Reading it from the API

The sandbox has a console API under `/api/v1/sandbox`, session-authenticated like the rest of the console:

| Route                                      | Purpose                                       |
|--------------------------------------------|-----------------------------------------------|
| `GET /api/v1/sandbox`                      | Page of captured messages, newest first       |
| `GET /api/v1/sandbox/info`                 | Connection details and the retention settings |
| `GET /api/v1/sandbox/:id`                  | One message, parsed                           |
| `GET /api/v1/sandbox/:id/raw`              | The wire bytes as `text/plain`                |
| `GET /api/v1/sandbox/:id/attachments/:idx` | One attachment                                |
| `DELETE /api/v1/sandbox/:id`               | Delete one message                            |
| `POST /api/v1/sandbox/clear`               | Empty the project's sandbox                   |

## What a sandbox credential may do

A sandbox key is judged on the **sandbox** resource, not on emails, and that is what makes it safe to hand out.

**Ticking the flag is the whole thing.** The key carries `sandbox:read`, `sandbox:write`
and `sandbox:delete` by itself - send, read back what was sent, clear between runs - so a key created with an empty
permission list and `"sandbox": true` works. Add permissions only if the application under test needs something beyond
sending.

| Permissions                     | carried by the flag, plus whatever else you tick |
|---------------------------------|--------------------------------------------------|
| `POST /emails/send`             | captured                                         |
| `POST /emails/send-template`    | captured                                         |
| `/sandbox`                      | read, clear, delete                              |
| everything else under `/emails` | **403**                                          |
| every other resource            | only if you granted it                           |

The delivery log, one message's detail, batch and retry all describe or touch real mail, so a credential whose whole
purpose is that its mail is not real is turned away from them by name.

{{< callout type="note" title="This used to require emails:write, and that was a hole" >}}
A sandbox key needed `emails:write` to reach the send route - which also grants
`POST /emails/{id}/retry`. Retry re-queues an **existing** message, so it honoured no sandbox flag and never could:there
is nothing to capture, the message was already composed and addressed to a real recipient. A credential handed out
precisely so it could not send real mail could put a real failed message back on the queue.

Narrowing the resource removes that by construction. A sandbox key now holds no permission on emails at all, so there is
nothing on that surface for it to spend.
{{< /callout >}}

## Who can see it

Anybody holding `sandbox:read`, and that is worth granting on its own. A role holding the sandbox and nothing else - no
email log, no contacts, no templates, no domains, no export - is exactly what a contractor wiring up a signup flow
needs. See
[Roles](/docs/projects/overview#roles).

The sandbox screen recognises that shape and becomes the whole console for whoever carries it, rather than sitting
behind a navigation of links that all answer `403`.

Handing a contractor a sandbox-only role and a sandbox credential gives them everything they need to build against
Mailyard and no view of a single message the company actually sent.

## What it is not

- **Not a preview.** [Template preview](/docs/templates/overview) renders a template without any of your application in
  the loop. The sandbox is the other half: your application composed the message, and this is what it produced.
- **Not a deliverability check.** Nothing here touched DNS, SPF, DKIM or a receiving server.
- **Not an inbox.** [Inbound Email](/docs/inbound/overview) receives mail addressed to a domain you verified, from
  anyone. The sandbox holds mail your own application submitted.
