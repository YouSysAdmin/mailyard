// Mailyard, Copyright (c) 2021-2026 YouSysAdmin

// Package database declares the abstraction every persistence backend
// implements.
// The interface stays narrow - concrete backends layer richer typed methods on top.
// Per-domain stores read the *sql.DB through this interface so they don't bind to a specific dialect.
package database

import (
	"database/sql"
)

// Database is the backend handle the runtime holds.
//
// It stays an interface with one implementation so core/env does not
// import the postgres package (and with it the pgx driver) just to
// name a field. It is deliberately tiny: there is one dialect, and a
// query that needs to ask which one it runs on is a query that should
// not exist.
type Database interface {
	// Close releases backend resources. Idempotent.
	Close() error

	// DB returns the underlying *sql.DB so domain stores can issue
	// queries directly.
	// Connection pool tuning is the backend's responsibility -
	// callers should not mutate pool settings.
	DB() *sql.DB
}
