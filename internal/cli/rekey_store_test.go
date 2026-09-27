// Mailyard, Copyright (c) 2021-2026 YouSysAdmin

package cli

import (
	"testing"

	"github.com/yousysadmin/mailyard/internal/core/crypto"
	"github.com/yousysadmin/mailyard/internal/database"
	"github.com/yousysadmin/mailyard/internal/database/dbtest"
)

// Every sealed column opens under the new key after a rekey and none
// under the old one, and empty columns stay empty.
func TestRekeyRewritesEverySealedColumn(t *testing.T) {
	db := dbtest.Open(t)
	dbtest.Migrate(t, db)
	old := crypto.New("old-key-old-key-old-key-old-key-old")
	fresh := crypto.New("new-key-new-key-new-key-new-key-new")

	seal := func(plain string) string {
		t.Helper()
		s, err := old.Encrypt(plain)
		if err != nil {
			t.Fatal(err)
		}

		return s
	}

	exec := func(q string, args ...any) {
		t.Helper()
		//sqlconst:allow test helper, every query is a literal at its call site
		if _, err := db.ExecContext(t.Context(), database.Rebind(q), args...); err != nil {
			t.Fatalf("%s: %v", q, err)
		}
	}

	exec(`INSERT INTO projects (id, name, slug, default_language, created_at)
	      VALUES ('e66e7a4d-9e6c-4884-869a-cf9ffcf22181', 'One', 'one', 'en', now())`)
	exec(`INSERT INTO users (id, email, totp_secret, created_at)
	      VALUES ('1ce81574-b36f-4f3e-8e6e-dabcaaf8f5b1', 'a@example.com', ?, now()),
	             ('2ce81574-b36f-4f3e-8e6e-dabcaaf8f5b2', 'b@example.com', '', now())`, seal("totp-a"))
	exec(`INSERT INTO domains (id, project_id, domain, verification_token, dkim_private_key, dkim_next_private_key)
	      VALUES ('8d2c5b6a-1e4f-4a7b-9c3d-2e1f0a9b8c7d', 'e66e7a4d-9e6c-4884-869a-cf9ffcf22181', 'example.com', 'tok', ?, ?)`,
		seal("dkim-pem"), seal("next-pem"))

	counts, err := rekeyAll(t.Context(), db, old, fresh)
	if err != nil {
		t.Fatal(err)
	}

	for col, want := range map[string]int{
		"users.totp_secret": 1, "domains.dkim_private_key": 1, "domains.dkim_next_private_key": 1, "webhooks.secret": 0,
	} {
		if counts[col] != want {
			t.Errorf("%s: rewrote %d rows, want %d", col, counts[col], want)
		}
	}

	read := func(q string, args ...any) string {
		t.Helper()
		var s string
		//sqlconst:allow test helper, every query is a literal at its call site
		if err := db.QueryRowContext(t.Context(), database.Rebind(q), args...).Scan(&s); err != nil {
			t.Fatal(err)
		}

		return s
	}

	for q, want := range map[string]string{
		`SELECT totp_secret FROM users WHERE email = 'a@example.com'`:            "totp-a",
		`SELECT dkim_private_key FROM domains WHERE domain = 'example.com'`:      "dkim-pem",
		`SELECT dkim_next_private_key FROM domains WHERE domain = 'example.com'`: "next-pem",
	} {
		sealed := read(q)
		if got, err := fresh.Decrypt(sealed); err != nil || got != want {
			t.Errorf("%s under the new key: %q, %v", q, got, err)
		}

		if _, err := old.Decrypt(sealed); err == nil {
			t.Errorf("%s still opens under the old key", q)
		}
	}

	if got := read(`SELECT totp_secret FROM users WHERE email = 'b@example.com'`); got != "" {
		t.Errorf("an empty column was rewritten to %q", got)
	}
}
