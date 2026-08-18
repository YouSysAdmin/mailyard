// Mailyard, Copyright (c) 2021-2026 YouSysAdmin

//go:build !enterprise

package openapi

// This build registers no node-facing routes, so the document describes
// one group rather than two.
const enrolDescription = ""
