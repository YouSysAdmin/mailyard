// Mailyard, Copyright (c) 2021-2026 YouSysAdmin

// Package template is the persistence and handler surface for email
// templates, their versions, and per-language localizations. Version
// and localization queries join through the templates table on
// project_id so tenant scoping never depends on the caller passing
// a consistent id pair. Routes live behind requireAuth +
// requireProject in server/routes.go. Import / export lives in
// transfer.go.
package template
