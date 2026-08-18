---
title: "Data Deletion"
description: "Bulk erasure of contact and email data"
weight: 20
---

Erasure scoped to the active project. Both endpoints require the project `admin` or
`owner` role - they are destructive and irreversible.

## Delete Contact Data

```
POST /api/v1/data/delete-contacts
```

Erase one address from the [contacts](/docs/contacts/contact-management) list and the
[suppression list](/docs/contacts/suppression-list):

```json
{
    "email": "user@example.com"
}
```

Response:

```json
{
    "deleted": 1,
    "message": "Erased user@example.com from contacts and the suppression list."
}
```

To erase **every** contact in the project, omit the address and confirm explicitly:

```json
{
    "confirm_all": true
}
```

An empty body is refused rather than treated as consent:

Response (`400`):

```json
{
    "error": "pass an email to erase one contact, or confirm_all: true to erase every contact in this project"
}
```

{{< callout type="note" title="This leaves the email log alone" >}}
Removing the record that a message was sent is a separate decision with separate consequences - billing, deliverability
disputes, and your own audit trail. It lives behind the endpoint below so the two are never conflated.
{{< /callout >}}

## Delete Email Logs

```
POST /api/v1/data/delete-email-logs
```

| Field             | Effect                                                            |
|-------------------|-------------------------------------------------------------------|
| `email`           | Erase records where this address is the sender or a recipient     |
| `older_than_days` | Erase records older than N days                                   |
| `confirm_all`     | Required to erase every record when neither of the above is given |

```json
{
    "older_than_days": 30
}
```

Response:

```json
{
    "deleted": 1500,
    "message": "Deleted 1500 email records older than 30 days."
}
```

Address matching is exact. Passing `ob@x.com` deletes nothing even though it is a substring of `bob@x.com` and
`rob@x.com`.

{{< callout type="warning" title="In-flight mail is never deleted" >}}
Records with status `queued`, `scheduled`, or `processing` are exempt however old they are, and however broad the
request. Deleting one would strand work the delivery queue is about to claim. The same rule
the [retention job](/docs/admin/scheduled-jobs) follows.
{{< /callout >}}
