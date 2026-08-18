// Mailyard, Copyright (c) 2021-2026 YouSysAdmin

// Package migrations embeds the schema as an FS the database backend
// hands to goose.SetBaseFS.
//
// It lives at the repository root, beside the SQL, because go:embed
// cannot reach outside its own package directory.
//
// One file per table, named for the table - not a chronological log of
// ALTERs, because the question these files answer is "what does the
// emails table look like".
//
// # Adding a change
//
// An applied file is FROZEN. goose never looks at a version it has
// run, so editing one changes what a fresh install gets and nothing on
// an existing one: same version number, different schema, no error
// anywhere. A change to an existing table is a new file at the next
// number (00047_emails_add_foo.sql).
//
// The SQL is PostgreSQL. A second engine would get its own directory
// and its own FS rather than conditionals in here.
package migrations

import "embed"

// FS is the migrations filesystem; goose walks it for *.sql files.
//
//go:embed *.sql
var FS embed.FS
