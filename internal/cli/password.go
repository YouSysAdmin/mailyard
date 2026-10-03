// Mailyard, Copyright (c) 2021-2026 YouSysAdmin

package cli

import (
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/spf13/cobra"
	"golang.org/x/term"

	"github.com/yousysadmin/mailyard/internal/core/authenticator"
	"github.com/yousysadmin/mailyard/internal/core/crypto"
	"github.com/yousysadmin/mailyard/internal/core/env"
	"github.com/yousysadmin/mailyard/internal/domain/store"
)

// newSetPasswordCmd builds `mailyard set-password`, the way back into
// an installation whose bootstrap password is lost. That password is
// printed exactly once, at first start, and the forgot-password flow
// needs system_mail, which is off by default.
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

			if email == "" {
				email = cfg.Auth.Local.Email
			}

			email = strings.TrimSpace(strings.ToLower(email))
			if email == "" {
				return usage("no email given and auth.local.email is empty, pass --email")
			}

			password, err = resolvePassword(cmd.ErrOrStderr(), password, stdin)
			if err != nil {
				return err
			}

			password, err = newPassword(password)
			if err != nil {
				return err
			}

			// A one-shot command against a database somebody else
			// migrates. Applying the schema from here would make a
			// password reset a schema change.
			db, st, err := openDatabase(&cfg.Database, crypto.New(cfg.Database.Crypto.EncryptionKey), false)
			if err != nil {
				return fmt.Errorf("open db: %w", err)
			}

			defer func() { _ = db.Close() }()

			hash, err := authenticator.HashPassword(password)
			if err != nil {
				return err
			}

			return applyPassword(context.Background(), st, email, hash, cmd.ErrOrStderr())
		},
	}
	cmd.Flags().StringVar(&email, "email", "", "user to update (defaults to auth.local.email)")
	cmd.Flags().StringVar(&password, "password", "", "new password (avoid: lands in shell history, prefer --stdin)")
	cmd.Flags().BoolVar(&stdin, "stdin", false, "read the password from stdin")

	return cmd
}

// newPassword is the password set-password stores, or why it refuses.
//
// Trimmed because sign-in trims what it is given: a stored password
// carrying an edge space is one nobody can ever sign in with.
func newPassword(raw string) (string, error) {
	password := strings.TrimSpace(raw)

	// The same floor the API sets, counted the same way, so the
	// offline path cannot leave a weaker password behind.
	if utf8.RuneCountInString(password) < 12 {
		return "", usage("password must be at least 12 characters")
	}

	// bcrypt refuses anything longer outright. Say so here rather than
	// letting HashPassword fail with a library error after the operator
	// has typed it twice.
	if len(password) > 72 {
		return "", usage("password must be at most 72 bytes (bcrypt's limit)")
	}

	return password, nil
}

// applyPassword writes hash as the account's password, enables the
// account, and ends its sessions and reset links, reporting each step
// to out.
func applyPassword(ctx context.Context, st *store.Store, email, hash string, out io.Writer) error {
	u, err := st.User.Get(ctx, email)
	if err != nil {
		return fmt.Errorf("look up %s: %w", email, err)
	}

	if u == nil {
		return fmt.Errorf("no user with email %s", email)
	}

	// A targeted write: Put would rewrite the whole row from a stale
	// read.
	if err := st.User.SetPassword(ctx, u.ID, hash); err != nil {
		return fmt.Errorf("save: %w", err)
	}

	_, _ = fmt.Fprintf(out, "password updated for %s\n", email)

	// A disabled account cannot sign in whatever its password is, and
	// somebody running this is trying to get back in. Put writes the
	// whole row, so it carries the new hash too.
	if u.Disabled {
		u.Disabled = false
		u.PasswordHash = hash
		if err := st.User.Put(ctx, u); err != nil {
			return fmt.Errorf("enable: %w", err)
		}

		_, _ = fmt.Fprintf(out, "account enabled\n")
	}

	if u.TOTPEnabled {
		// Deliberately not cleared: a password reset is not a reason
		// to drop the second factor, and doing it silently would turn
		// file access into a 2FA bypass.
		_, _ = fmt.Fprintf(out,
			"note: two-factor auth is still enabled on this account, you will be asked for a code\n")
	}

	// Like every other password change: end the old sessions and any
	// outstanding reset link.
	if err := st.PasswordReset.InvalidateForUser(ctx, u.ID, time.Now().UTC()); err != nil {
		return fmt.Errorf("invalidate reset links: %w", err)
	}

	n, err := st.Session.RevokeAllForUser(ctx, u.ID)
	if err != nil {
		return fmt.Errorf("revoke sessions: %w", err)
	}

	_, _ = fmt.Fprintf(out, "revoked %d session(s) and every outstanding reset link\n", n)

	return nil
}

// resolvePassword gets the new password from whichever source the
// operator chose, preferring the ones that keep it out of history.
func resolvePassword(prompt io.Writer, flagValue string, fromStdin bool) (string, error) {
	return resolveSecret(prompt, "New password", flagValue, fromStdin)
}

// resolveSecret is resolvePassword for any secret typed at the
// terminal, labelled for the prompt.
func resolveSecret(prompt io.Writer, label, flagValue string, fromStdin bool) (string, error) {
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
		return "", usage("stdin is not a terminal, pass --stdin to read the password from a pipe")
	}

	_, _ = fmt.Fprint(prompt, label+": ")
	first, err := term.ReadPassword(fd)
	_, _ = fmt.Fprintln(prompt)
	if err != nil {
		return "", err
	}

	_, _ = fmt.Fprint(prompt, "Repeat: ")
	second, err := term.ReadPassword(fd)
	_, _ = fmt.Fprintln(prompt)
	if err != nil {
		return "", err
	}

	if string(first) != string(second) {
		return "", errors.New("the two entries do not match")
	}

	return string(first), nil
}
