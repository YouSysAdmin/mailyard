// Mailyard, Copyright (c) 2021-2026 YouSysAdmin

// Package smtpcredential is the persistence and handler surface for
// SMTP relay credentials. Console routes live behind requireAuth +
// requireProject in server/routes.go. The auth-side lookup
// (GetByUsername + hash compare) is consumed by the relay backend.
package smtpcredential
