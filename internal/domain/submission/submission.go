// Mailyard, Copyright (c) 2021-2026 YouSysAdmin

// Package submission implements the SMTP submission listener:
// applications speak plain SMTP to Mailyard instead of the HTTP API.
// AUTH PLAIN accepts either credential type:
//
//   - an SMTP submission credential (internal/domain/smtpcredential),
//     username "smtp_..." plus its password. This is the credential
//     to hand a legacy application - it unlocks submission and
//     nothing else.
//   - an API key with scope send as the password, username ignored.
//     Submission access then shares the key lifecycle - scopes, IP
//     allowlists, revocation - with the machine HTTP surface.
//
// Accepted messages are parsed and handed to the outbound email
// pipeline exactly like POST /api/v1/emails/send.
//
// The name comes from RFC 6409, and 587 is the submission port. Do not
// confuse it with a relay node: this is mail coming in from an
// application we authenticate, while a relay node sends mail out to a
// stranger's MX.
package submission
