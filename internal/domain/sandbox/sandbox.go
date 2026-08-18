// Mailyard, Copyright (c) 2021-2026 YouSysAdmin

// Package sandbox captures mail instead of delivering it.
//
// A developer points an application at a credential marked sandbox.
// Everything it sends is stored in full and nothing leaves the
// building, readable in the console down to the raw bytes.
//
// The decision is on the CREDENTIAL, never in the request. Swapping
// credentials is the whole value here - the application is identical
// between sandbox and production - where a field in the payload gets
// left true on a production deploy. So a sandbox credential may not
// opt OUT, and the attempt is refused rather than ignored. An ordinary
// one may opt IN per message, the direction that cannot leak real
// mail.
//
// Capture happens before email.Service.Validate, and has to: that path
// wants a verified sender, an enabled server, a clean suppression list
// and plan headroom, and a developer sending from test@localhost has
// none of them.
//
// Sandbox mail is throwaway. Every message gets an expiry and every
// project a cap, both enforced with plain deletes.
package sandbox
