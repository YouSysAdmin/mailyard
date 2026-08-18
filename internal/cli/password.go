// Mailyard, Copyright (c) 2021-2026 YouSysAdmin

package cli

import (
	"context"
	"errors"
	"fmt"
	"os"
	"strings"

	"github.com/spf13/cobra"
	"golang.org/x/term"

	"github.com/yousysadmin/mailyard/internal/core/authenticator"
	"github.com/yousysadmin/mailyard/internal/core/crypto"
	"github.com/yousysadmin/mailyard/internal/core/env"
)

// newSetPasswordCmd builds `mailyard set-password`.
//
// Until this existed there was no supported way back into an installation
// whose bootstrap password had been lost. That password is printed
// exactly once, at first start, and everything else assumed you still
// had it: the forgot-password flow needs system_mail, which is off by
// default, and there was no other operator entry point. Losing one
// line of terminal scrollback meant losing the installation.
//
// It runs offline, against the database the config names, so it works
// whether the server is up.
func newSetPasswordCmd() *cobra.Command {
	var email, password string
	var stdin bool

	cmd := &cobra.Command{
		Use:   "set-password",
		Short: "Set a user's password",
		Long: "Set a user's password directly in the database.\n\n" +
			"Use this to recover an installation whose bootstrap password was lost.\n" +
			"Reads the password from the terminal without echoing it, or from stdin\n" +
			"with --stdin so it can be piped and kept out of shell history.",
		RunE: func(cmd *cobra.Command, _ []string) error {
			configPath, _ := cmd.Flags().GetString("config")
			cfg, err := env.Load(configPath)
			if err != nil {
				return fmt.Errorf("load config: %w", err)
			}

			if err := cfg.Validate(); err != nil {
				return fmt.Errorf("config invalid: %w", err)
			}

			if email == "" {
				email = cfg.Auth.Local.Email
			}

			email = strings.TrimSpace(strings.ToLower(email))
			if email == "" {
				return errors.New("no email given and auth.local.email is empty, pass --email")
			}

			password, err = resolvePassword(password, stdin)
			if err != nil {
				return err
			}

			if len(password) < 8 {
				return errors.New("password must be at least 8 characters")
			}

			// bcrypt refuses anything longer outright. Say so here
			// rather than letting HashPassword fail with a library
			// error after the operator has typed it twice.
			if len(password) > 72 {
				return errors.New("password must be at most 72 bytes (bcrypt's limit)")
			}

			// A one-shot command against a database somebody else
			// migrates. Applying the schema from here would make a
			// password reset a schema change.
			db, st, err := openDatabase(&cfg.Database, crypto.New(cfg.Database.Crypto.EncryptionKey), false)
			if err != nil {
				return fmt.Errorf("open db: %w", err)
			}

			defer func() { _ = db.Close() }()

			ctx := context.Background()
			u, err := st.User.Get(ctx, email)
			if err != nil {
				return fmt.Errorf("look up %s: %w", email, err)
			}

			if u == nil {
				return fmt.Errorf("no user with email %s", email)
			}

			hash, err := authenticator.HashPassword(password)
			if err != nil {
				return err
			}

			u.PasswordHash = hash

			// A disabled account cannot sign in whatever its password
			// is, and somebody running this is trying to get back in.
			u.Disabled = false
			if err := st.User.Put(ctx, u); err != nil {
				return fmt.Errorf("save: %w", err)
			}

			fmt.Fprintf(os.Stderr, "password updated for %s\n", email)
			if u.TOTPEnabled {
				// Deliberately not cleared: a password reset is not a
				// reason to drop the second factor, and doing it
				// silently would turn file access into a 2FA bypass.
				fmt.Fprintf(os.Stderr,
					"note: two-factor auth is still enabled on this account, you will be asked for a code\n")
			}

			// Existing sessions keep working - the JWT is signed and its
			// session row is untouched. Say so, because somebody
			// resetting a password usually expects the opposite.
			fmt.Fprintf(os.Stderr,
				"note: existing sessions are NOT revoked, sign them out from the console if that matters\n")

			return nil
		},
	}
	cmd.Flags().StringVar(&email, "email", "", "user to update (defaults to auth.local.email)")
	cmd.Flags().StringVar(&password, "password", "", "new password (avoid: lands in shell history, prefer --stdin)")
	cmd.Flags().BoolVar(&stdin, "stdin", false, "read the password from stdin")

	return cmd
}

// resolvePassword gets the new password from whichever source the
// operator chose, preferring the ones that keep it out of history.
func resolvePassword(flagValue string, fromStdin bool) (string, error) {
	if flagValue != "" {
		return flagValue, nil
	}

	if fromStdin {
		var b []byte
		buf := make([]byte, 1024)
		for {
			n, err := os.Stdin.Read(buf)
			b = append(b, buf[:n]...)
			if err != nil || n == 0 {
				break
			}
		}

		return strings.TrimRight(string(b), "\r\n"), nil
	}

	fd := int(os.Stdin.Fd())
	if !term.IsTerminal(fd) {
		return "", errors.New("stdin is not a terminal, pass --stdin to read the password from a pipe")
	}

	fmt.Fprint(os.Stderr, "New password: ")
	first, err := term.ReadPassword(fd)
	fmt.Fprintln(os.Stderr)
	if err != nil {
		return "", err
	}

	fmt.Fprint(os.Stderr, "Repeat: ")
	second, err := term.ReadPassword(fd)
	fmt.Fprintln(os.Stderr)
	if err != nil {
		return "", err
	}

	if string(first) != string(second) {
		return "", errors.New("passwords do not match")
	}

	return string(first), nil
}
