// Mailyard, Copyright (c) 2021-2026 YouSysAdmin

package project

import (
	"context"
	"database/sql"
	"errors"
	"strings"

	"github.com/yousysadmin/mailyard/internal/database"
)

// AlertRecipients answers who hears about things, for core/alertmail.
//
// It lives here, in the domain that owns membership and ownership,
// rather than as three closures in serve.go. Two of these questions are
// about who is responsible for a project, which is what this package
// decides everywhere else - and the disabled-account and empty-address
// rules would otherwise be written a second time by whoever wired the
// notifier.
//
// It embeds database.Base like every other store rather than holding a
// *sql.DB of its own. That is not ceremony: the guards that check every
// query against the real schema and refuse dynamic SQL work by following
// the Base helpers, so a query issued around them is a query nothing
// verifies.
type AlertRecipients struct {
	database.Base
}

// NewAlertRecipients builds the resolver.
func NewAlertRecipients(db *sql.DB) *AlertRecipients {
	return &AlertRecipients{Base: database.NewBase(db)}
}

// ProjectAlert is the project's OWNERS plus its alert address.
//
// Owners, not members: an alert says something about the project's access
// or its data, and the audience for that is whoever is accountable for
// it. There can be several, and the list needs no separate upkeep because
// it IS the ownership flag the rest of the code enforces.
//
// The alert address is additive. If it replaced the owners, a project
// could route every warning to a mailbox nobody reads while the people
// accountable never hear about any of it.
//
// Disabled accounts are skipped: mail to somebody who cannot sign in to
// act on it is noise, and on an install behind an IdP it may be an
// address that no longer exists.
func (s *AlertRecipients) ProjectAlert(ctx context.Context, projectID string) ([]string, error) {
	rows, err := s.Query(ctx, `
        SELECT u.email
        FROM project_members m
        JOIN users u ON u.id = m.user_id
        WHERE m.project_id = ? AND m.owner = TRUE
          AND u.disabled = FALSE AND u.email <> ''`, projectID)
	if err != nil {
		return nil, err
	}

	defer func() { _ = rows.Close() }()

	seen := map[string]bool{}
	var out []string
	for rows.Next() {
		var addr string
		if err := rows.Scan(&addr); err != nil {
			return nil, err
		}

		addRecipient(&out, seen, addr)
	}

	if err := rows.Err(); err != nil {
		return nil, err
	}

	var extra string
	err = s.QueryRow(ctx,
		`SELECT alert_email FROM projects WHERE id = ?`, projectID).Scan(&extra)
	if err != nil && !errors.Is(err, sql.ErrNoRows) {
		return nil, err
	}

	addRecipient(&out, seen, extra)

	return out, nil
}

// PlatformAdmins is every enabled administrator of the installation -
// the same audience the certificate expiry sweep mails.
func (s *AlertRecipients) PlatformAdmins(ctx context.Context) ([]string, error) {
	rows, err := s.Query(ctx, `
        SELECT email FROM users
        WHERE admin = TRUE AND disabled = FALSE AND email <> ''`)
	if err != nil {
		return nil, err
	}

	defer func() { _ = rows.Close() }()
	seen := map[string]bool{}
	var out []string
	for rows.Next() {
		var addr string
		if err := rows.Scan(&addr); err != nil {
			return nil, err
		}

		addRecipient(&out, seen, addr)
	}

	return out, rows.Err()
}

// UserEmail is one account's address, empty when the account is disabled
// or gone.
//
// Empty rather than an error: an account event for a user who has since
// been removed is ordinary, and the caller reads no audience as nothing
// to do.
func (s *AlertRecipients) UserEmail(ctx context.Context, userID string) (string, error) {
	var addr string
	err := s.QueryRow(ctx, `
        SELECT email FROM users
        WHERE id = ? AND disabled = FALSE`, userID).Scan(&addr)
	if errors.Is(err, sql.ErrNoRows) {
		return "", nil
	}

	if err != nil {
		return "", err
	}

	return strings.TrimSpace(addr), nil
}

// addRecipient appends an address once, lowercased. An owner who is also
// the alert address must not get two copies.
func addRecipient(out *[]string, seen map[string]bool, addr string) {
	addr = strings.ToLower(strings.TrimSpace(addr))
	if addr == "" || seen[addr] {
		return
	}

	seen[addr] = true
	*out = append(*out, addr)
}
