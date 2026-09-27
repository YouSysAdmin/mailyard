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
	"github.com/yousysadmin/mailyard/internal/database"
)

// newRekeyCmd re-encrypts every sealed column under a new encryption
// key, for an installation whose key leaked or has to be rotated.
//
// Offline, like set-password: it opens the database with the current
// key from the config and rewrites the rows in one transaction. The
// operator then puts the new key in the config and starts the nodes.
func newRekeyCmd() *cobra.Command {
	var newKey string
	var stdin bool

	cmd := &cobra.Command{
		Use:   "rekey",
		Short: "Re-encrypt every sealed column under a new encryption key",
		Long: "Re-encrypt every sealed column under a new database.crypto.encryption_key.\n\n" +
			"Stop every node first: a node still running writes new rows under the old\n" +
			"key while this runs and cannot read the rows it rewrote. The rows are rewritten\n" +
			"in one transaction, so a failure leaves the database as it was. Afterwards put\n" +
			"the new key in the config of every node and start them again.\n\n" +
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

			current := crypto.New(cfg.Database.Crypto.EncryptionKey)
			db, _, err := openDatabase(&cfg.Database, current, false)
			if err != nil {
				return fmt.Errorf("open database: %w", err)
			}

			defer func() { _ = db.Close() }()

			counts, err := rekeyAll(cmd.Context(), db.DB(), current, crypto.New(newKey))
			if err != nil {
				return err
			}

			for _, c := range sealedColumns {
				_, _ = fmt.Fprintf(cmd.ErrOrStderr(), "%-24s %-22s %d row(s)\n", c.table, c.column, counts[c.table+"."+c.column])
			}

			_, _ = fmt.Fprintln(cmd.ErrOrStderr(),
				"\nevery sealed row now reads under the new key: set database.crypto.encryption_key to it on every node and start them")

			return nil
		},
	}

	cmd.Flags().StringVar(&newKey, "key", "", "the new key (avoid: lands in shell history, prefer --stdin)")
	cmd.Flags().BoolVar(&stdin, "stdin", false, "read the new key from stdin")

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
}

// rekeyAll rewrites every sealed column from current to fresh in one
// transaction and reports the rows rewritten per column.
func rekeyAll(ctx context.Context, db *sql.DB, current, fresh *crypto.Service) (map[string]int, error) {
	tx, err := db.BeginTx(ctx, nil)
	if err != nil {
		return nil, err
	}

	defer func() { _ = tx.Rollback() }()

	counts := map[string]int{}
	for _, col := range sealedColumns {
		n, err := rekeyColumn(ctx, tx, col, current, fresh)
		if err != nil {
			return nil, fmt.Errorf("%s.%s: %w", col.table, col.column, err)
		}

		counts[col.table+"."+col.column] = n
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
