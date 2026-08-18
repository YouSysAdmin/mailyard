// Mailyard, Copyright (c) 2021-2026 YouSysAdmin

// Package smtpserver is the persistence and handler surface for
// per-project SMTP servers: the store encrypts passwords at rest
// (core/crypto) and decrypts on read, endpoints live behind
// requireAuth + requireProject in server/routes.go. The model is in
// internal/models/smtpserver, the interface in internal/domain/store.
package smtpserver
