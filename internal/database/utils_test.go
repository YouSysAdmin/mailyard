// Mailyard, Copyright (c) 2021-2026 YouSysAdmin

package database

import "testing"

func TestRebind(t *testing.T) {
	cases := []struct {
		in, want string
	}{
		{`SELECT 1 WHERE a = ? AND b = ?`, `SELECT 1 WHERE a = $1 AND b = $2`},

		// Past ten, so a two-digit index is exercised: a naive
		// implementation that reused a single byte would produce $1 eleven times here.
		{`VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`, `VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11)`},
		{`SELECT COUNT(*) FROM users`, `SELECT COUNT(*) FROM users`},
		{``, ``},
	}
	for _, c := range cases {
		if got := Rebind(c.in); got != c.want {
			t.Errorf("Rebind(%q) = %q, want %q", c.in, got, c.want)
		}
	}
}
