// Mailyard, Copyright (c) 2021-2026 YouSysAdmin

// Package project is the persistence and handler surface for
// tenancy: projects, memberships, and invitations. The model lives
// in internal/models/project, the ProjectStore interface in
// internal/domain/store. Routes are mounted behind requireAuth in
// server/routes.go - role checks happen per handler because these
// routes address the project by path id, not by the
// X-Mailyard-Project-Id header that requireProject resolves for
// resource domains.
package project
