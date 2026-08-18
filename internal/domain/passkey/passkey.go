// Mailyard, Copyright (c) 2021-2026 YouSysAdmin

// Package passkey persists enrolled WebAuthn credentials.
//
// The ceremonies themselves live in internal/core/passkey and the
// handlers in internal/domain/auth, because a passkey is a way to
// sign in and the security audit trail is written where sign-ins are.
// This package is only the table.
package passkey
