// Mailyard, Copyright (c) 2021-2026 YouSysAdmin

package cli

import (
	"context"
	"database/sql"
	"fmt"
	"unicode/utf8"

	"github.com/spf13/cobra"

	"github.com/yousysadmin/mailyard/internal/core/crypto"
	"github.com/yousysadmin/mailyard/internal/core/env"
	"github.com/yousysadmin/mailyard/internal/core/ids"
	"github.com/yousysadmin/mailyard/internal/database"
	"github.com/yousysadmin/mailyard/internal/database/postgres"
)

// newRekeyCmd re-encrypts every sealed column under a new encryption
// key, for an installation whose key leaked or has to be rotated.
//
// Offline, like set-password: it opens the database with the current
// key from the config and rewrites the rows in one transaction. The
// operator then puts the new key in the config and starts the nodes.
func newRekeyCmd() *cobra.Command {
	var newKey string
	var stdin, forgetTracking bool

	cmd := &cobra.Command{
		Use:   "rekey",
		Short: "Re-encrypt every sealed column under a new encryption key",
		Long: "Re-encrypt every sealed column under a new database.crypto.encryption_key.\n\n" +
			"Stop every node first: a node still running writes new rows under the old\n" +
			"key while this runs and cannot read the rows it rewrote. The rows are rewritten\n" +
			"in one transaction, so a failure leaves the database as it was. Afterwards put\n" +
			"the new key in the config of every node and start them again.\n\n" +
			"Unsubscribe links in mail already delivered never expire, so the tracking key\n" +
			"derived from the old encryption key is kept, sealed, to go on verifying them.\n" +
			"Web view, open and click links signed under it stop working. Pass\n" +
			"--forget-tracking when the old key was used to forge unsubscribe links.\n\n" +
			"Reads the new key from the terminal without echoing it, or from stdin with\n" +
			"--stdin so it can be piped and kept out of shell history.",
		Args: noArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			configPath, _ := cmd.Flags().GetString("config")
			cfg, err := env.Load(configPath)
			if err != nil {
				return fmt.Errorf("load config: %w", err)
			}

			if err := cfg.Validate(); err != nil {
				return fmt.Errorf("config invalid: %w", err)
			}

			current := crypto.New(cfg.Database.Crypto.EncryptionKey)

			db, _, err := openDatabase(&cfg.Database, current, false)
			if err != nil {
				return fmt.Errorf("open database: %w", err)
			}

			defer func() { _ = db.Close() }()

			// Exact, not "at least": a binary older than the schema
			// does not know every sealed column and would leave the
			// rest under the old key while reporting success. Before
			// the prompt, so the wrong binary is refused before a
			// secret is typed into it.
			if err := postgres.RequireExactSchema(db.DB()); err != nil {
				return err
			}

			newKey, err = resolveSecret(cmd.ErrOrStderr(), "New encryption key", newKey, stdin)
			if err != nil {
				return err
			}

			// The floor Validate holds the configured key to.
			if utf8.RuneCountInString(newKey) < 32 {
				return usage("the new key must be at least 32 characters (generate with `openssl rand -hex 32`)")
			}

			if newKey == cfg.Database.Crypto.EncryptionKey {
				return usage("the new key is the current one")
			}

			retire := crypto.DeriveKey(cfg.Database.Crypto.EncryptionKey, crypto.KeyTracking)
			if forgetTracking {
				retire = ""
			}

			counts, err := rekeyAll(cmd.Context(), db.DB(), current, crypto.New(newKey), retire)
			if err != nil {
				return err
			}

			for _, c := range sealedColumns {
				_, _ = fmt.Fprintf(cmd.ErrOrStderr(), "%-24s %-22s %d row(s)\n", c.table, c.column, counts[c.table+"."+c.column])
			}

			if forgetTracking {
				_, _ = fmt.Fprintln(cmd.ErrOrStderr(), "\nno retired tracking key was kept: unsubscribe links signed under a retired encryption key are refused")
			}

			_, _ = fmt.Fprintln(cmd.ErrOrStderr(),
				"\nevery sealed row now reads under the new key: set database.crypto.encryption_key to it on every node and start them")

			return nil
		},
	}

	cmd.Flags().StringVar(&newKey, "key", "", "the new key (avoid: lands in shell history, prefer --stdin)")
	cmd.Flags().BoolVar(&stdin, "stdin", false, "read the new key from stdin")
	cmd.Flags().BoolVar(&forgetTracking, "forget-tracking", false,
		"do not keep the old tracking key, so unsubscribe links delivered so far stop working")

	return cmd
}

// sealedColumn is one column the crypto service seals, with the two
// statements that read it for update and write it back. keys is how
// many leading columns the select returns to address the row.
type sealedColumn struct {
	table, column string
	keys          int
	selectSQL     string
	updateSQL     string
}

// sealedColumns is every column New seals. A column added to the
// product is added here, or a rekey leaves it unreadable.
var sealedColumns = []sealedColumn{
	{"users", "totp_secret", 1,
		`SELECT id, totp_secret FROM users WHERE totp_secret <> '' FOR UPDATE`,
		`UPDATE users SET totp_secret = ? WHERE id = ?`},
	{"certificates", "data", 2,
		`SELECT scope, name, data FROM certificates WHERE data <> '' FOR UPDATE`,
		`UPDATE certificates SET data = ? WHERE scope = ? AND name = ?`},
	{"domains", "dkim_private_key", 1,
		`SELECT id, dkim_private_key FROM domains WHERE dkim_private_key <> '' FOR UPDATE`,
		`UPDATE domains SET dkim_private_key = ? WHERE id = ?`},
	{"domains", "dkim_next_private_key", 1,
		`SELECT id, dkim_next_private_key FROM domains WHERE dkim_next_private_key <> '' FOR UPDATE`,
		`UPDATE domains SET dkim_next_private_key = ? WHERE id = ?`},
	{"oauth_providers", "client_secret", 1,
		`SELECT id, client_secret FROM oauth_providers WHERE client_secret <> '' FOR UPDATE`,
		`UPDATE oauth_providers SET client_secret = ? WHERE id = ?`},
	{"smtp_servers", "password", 1,
		`SELECT id, password FROM smtp_servers WHERE password <> '' FOR UPDATE`,
		`UPDATE smtp_servers SET password = ? WHERE id = ?`},
	{"shared_smtp_servers", "password", 1,
		`SELECT id, password FROM shared_smtp_servers WHERE password <> '' FOR UPDATE`,
		`UPDATE shared_smtp_servers SET password = ? WHERE id = ?`},
	{"webhooks", "secret", 1,
		`SELECT id, secret FROM webhooks WHERE secret <> '' FOR UPDATE`,
		`UPDATE webhooks SET secret = ? WHERE id = ?`},
	{"tracking_keys", "key", 1,
		`SELECT id, key FROM tracking_keys FOR UPDATE`,
		`UPDATE tracking_keys SET key = ? WHERE id = ?`},
	{"sender_signing_keys", "private_key", 1,
		`SELECT sender_id, private_key FROM sender_signing_keys WHERE private_key <> '' FOR UPDATE`,
		`UPDATE sender_signing_keys SET private_key = ? WHERE sender_id = ?`},
}

// rekeyAll rewrites every sealed column from current to fresh in one
// transaction and reports the rows rewritten per column. A non-empty
// retire is stored, sealed under fresh, as a tracking key that goes on
// verifying unsubscribe links.
func rekeyAll(ctx context.Context, db *sql.DB, current, fresh *crypto.Service, retire string) (map[string]int, error) {
	tx, err := db.BeginTx(ctx, nil)
	if err != nil {
		return nil, err
	}

	defer func() { _ = tx.Rollback() }()

	counts := map[string]int{}
	i
	for _, col := range sealedColumns {
		n, err := rekeyColumn(ctx, tx, col, current, fresh)
		if err != nil {
			return nil, fmt.Errorf("%s.%s: %w", col.table, col.column, err)
		}

		counts[col.table+"."+col.column] = n
	}

	// Forgetting forgets every retired key, not only the one retiring
	// now: after a leak the rows sealed under the leaked key are as
	// exposed as the key itself.
	if retire == "" {
		//sqlconst:allow a literal with no placeholders
		if _, err := tx.ExecContext(ctx, `DELETE FROM tracking_keys`); err != nil {
			return nil, fmt.Errorf("tracking_keys: %w", err)
		}
	}

	// After the loop, so the row is sealed once, under fresh.
	if retire != "" {
		sealed, err := fresh.Encrypt(retire)
		if err != nil {
			return nil, err
		}

		//sqlconst:allow a literal, Rebind only turns its placeholders into $n
		if _, err := tx.ExecContext(ctx, database.Rebind(`INSERT INTO tracking_keys (id, key) VALUES (?, ?)`),
			ids.New(), sealed); err != nil {
			return nil, fmt.Errorf("tracking_keys: %w", err)
		}
	}

	return counts, tx.Commit()
}

func rekeyColumn(ctx context.Context, tx *sql.Tx, col sealedColumn, current, fresh *crypto.Service) (int, error) {
	//sqlconst:allow every statement is a literal in sealedColumns, no runtime data reaches it
	rows, err := tx.QueryContext(ctx, col.selectSQL)
	if err != nil {
		return 0, err
	}

	defer func() { _ = rows.Close() }()

	type row struct {
		keys   []any
		sealed string
	}

	var pending []row

	for rows.Next() {
		r := row{keys: make([]any, col.keys)}
		dest := make([]any, 0, col.keys+1)
		for i := range r.keys {
			r.keys[i] = new(string)
			dest = append(dest, r.keys[i])
		}

		dest = append(dest, &r.sealed)
		if err := rows.Scan(dest...); err != nil {
			return 0, err
		}

		pending = append(pending, r)
	}

	if err := rows.Err(); err != nil {
		return 0, err
	}

	_ = rows.Close()
	for _, r := range pending {
		plain, err := current.Decrypt(r.sealed)
		if err != nil {
			return 0, fmt.Errorf("does not open under the current key: %w", err)
		}

		resealed, err := fresh.Encrypt(plain)
		if err != nil {
			return 0, err
		}

		args := append([]any{resealed}, r.keys...)
		//sqlconst:allow every statement is a literal in sealedColumns, no runtime data reaches it
		if _, err := tx.ExecContext(ctx, database.Rebind(col.updateSQL), args...); err != nil {
			return 0, err
		}
	}

	return len(pending), nil
}
