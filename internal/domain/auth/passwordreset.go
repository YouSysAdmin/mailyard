// Mailyard, Copyright (c) 2021-2026 YouSysAdmin

package auth

import (
	"context"
	"log/slog"
	"strings"
	"time"

	"github.com/gofiber/fiber/v3"
	"github.com/yousysadmin/mailyard/internal/core/clientip"
	"github.com/yousysadmin/mailyard/internal/core/ids"

	"github.com/yousysadmin/mailyard/internal/core/authenticator"
	"github.com/yousysadmin/mailyard/internal/core/env"
	"github.com/yousysadmin/mailyard/internal/core/response"
	"github.com/yousysadmin/mailyard/internal/core/safego"
	"github.com/yousysadmin/mailyard/internal/core/systemmail"
	"github.com/yousysadmin/mailyard/internal/core/validation"
	"github.com/yousysadmin/mailyard/internal/domain"
	amodel "github.com/yousysadmin/mailyard/internal/models/audit"
	prmodel "github.com/yousysadmin/mailyard/internal/models/passwordreset"
	usermodel "github.com/yousysadmin/mailyard/internal/models/user"
)

// maxResetsPerHour caps how many links one account can be mailed.
// Without it, anyone who knows an operator's address can flood that
// mailbox from an unauthenticated endpoint.
const maxResetsPerHour = 3

// maxResetsPerHourPerAccount is the ceiling over every address at
// once: the per-address budget is what a stranger spends, this bounds
// the mail the owner receives.
const maxResetsPerHourPerAccount = 15

// PasswordResetRequest mails a single-use reset link.
//
// The response is identical whether or not the address exists, is
// disabled, or is OIDC-only: an unauthenticated endpoint that
// distinguishes them is an account enumeration oracle. The only
// condition that changes the answer is the feature being switched off
// entirely, which is a property of the install rather than of any
// account.
func (h *Handler) PasswordResetRequest(c fiber.Ctx) error {
	if !h.Runtime.Config.Auth.Local.Enabled {
		return response.BadRequest(c, "local login is disabled")
	}

	if !h.Runtime.SystemMail.Enabled() {
		return response.BadRequest(c,
			"password reset is unavailable because this install has no system mail configured, ask an administrator to reset your password")
	}

	in, resp, ok := validation.Bind[resetRequestInput](c)
	if !ok {
		return resp
	}

	// Uniform reply, sent regardless of what the lookup finds.
	accepted := func() error {
		return response.Success(c, MessageResponse{
			Message: "If that address has an account, a reset link is on its way.",
		})
	}

	u, err := h.Runtime.Store.User.Get(c.Context(), in.Email)
	if err != nil {
		return response.Internal(c, err)
	}

	// An account the IdP owns has no password to reset - sending a link
	// would let the holder of that mailbox bypass the provider.
	if u == nil || u.Disabled || !u.ManagesOwnCredentials() {
		slog.Info("auth: password reset requested for an address with no resettable account",
			"client_ip", clientip.From(c))

		return accepted()
	}

	// The rest runs after the answer, so an address with an account
	// costs no more time than one without.
	ip := clientip.From(c)
	safego.Go(h.Runtime.Log, "auth: password reset", func() {
		ctx, cancel := context.WithTimeout(context.Background(), resetIssueTimeout)
		defer cancel()

		if err := h.issueReset(ctx, u, ip); err != nil {
			slog.Error("auth: password reset failed", "user_id", u.ID, "err", err)
		}
	})

	return accepted()
}

// resetIssueTimeout bounds the work a reset request does after it has
// answered.
const resetIssueTimeout = 30 * time.Second

// issueReset spends the mail budget, stores a token and mails the link.
func (h *Handler) issueReset(ctx context.Context, u *usermodel.User, ip string) error {
	now := time.Now().UTC()
	ips, err := h.Runtime.Store.PasswordReset.RecentRequestIPs(ctx, u.ID, now.Add(-time.Hour))
	if err != nil {
		return err
	}

	if budgetSpent(ips, ip, maxResetsPerHour, maxResetsPerHourPerAccount) {
		slog.Warn("auth: password reset throttled", "user_id", u.ID, "client_ip", ip)

		return nil
	}

	plaintext, hash, err := prmodel.Generate()
	if err != nil {
		return err
	}

	tok := &prmodel.Token{
		ID:        ids.New(),
		UserID:    u.ID,
		TokenHash: hash,
		ExpiresAt: now.Add(prmodel.TTL),
		CreatedAt: now,
		RequestIP: ip,
	}
	if err := h.Runtime.Store.PasswordReset.Put(ctx, tok); err != nil {
		return err
	}

	link := strings.TrimRight(h.Runtime.Config.Server.PublicURL, "/") +
		env.ConsolePath + "/reset-password?token=" + plaintext
	subject, htmlBody, textBody := systemmail.PasswordReset(link, int(prmodel.TTL.Minutes()))
	h.Runtime.SystemMail.SendAsync([]string{u.Email}, subject, htmlBody, textBody)

	slog.Info("auth: password reset requested", "user_id", u.ID, "client_ip", ip)

	return nil
}

// PasswordResetConfirm redeems a token and sets the new password.
func (h *Handler) PasswordResetConfirm(c fiber.Ctx) error {
	if !h.Runtime.Config.Auth.Local.Enabled {
		return response.BadRequest(c, "local login is disabled")
	}

	in, resp, ok := validation.Bind[resetConfirmInput](c)
	if !ok {
		return resp
	}

	tok, err := h.Runtime.Store.PasswordReset.GetByHash(c.Context(), prmodel.Hash(in.Token))
	if err != nil {
		return response.Internal(c, err)
	}

	now := time.Now().UTC()
	// One message for every failure leg: expired, already spent, and
	// never existed are indistinguishable to the caller.
	if tok == nil || !tok.Usable(now) {
		slog.Warn("auth: password reset token rejected", "client_ip", clientip.From(c))

		return response.BadRequest(c, "this reset link is invalid or has expired, request a new one")
	}

	u, err := h.Runtime.Store.User.GetByID(c.Context(), tok.UserID)
	if err != nil {
		return response.Internal(c, err)
	}

	if u == nil || u.Disabled {
		return response.BadRequest(c, "this reset link is invalid or has expired, request a new one")
	}

	// Burn the token before changing anything. The check above is a
	// read, so two redemptions of the same link both pass it - the
	// conditional UPDATE is what actually makes the link single use,
	// and it only counts if the loser stops here. Doing it afterwards
	// also meant a failed write was logged and ignored, leaving a
	// spent link usable for the rest of its 30 minutes.
	claimed, err := h.Runtime.Store.PasswordReset.MarkUsed(c.Context(), tok.ID, now)
	if err != nil {
		return response.Internal(c, err)
	}

	if !claimed {
		slog.Warn("auth: password reset token already spent", "token_id", tok.ID, "client_ip", clientip.From(c))

		return response.BadRequest(c, "this reset link is invalid or has expired, request a new one")
	}

	hash, err := authenticator.HashPassword(in.Password)
	if err != nil {
		return response.Internal(c, err)
	}

	// SetPassword, not Put: Put writes the whole row from a User read
	// before this request did anything, so an administrator disabling the
	// account in between had that undone - and the account walked back in
	// through its own password reset.
	if err := h.Runtime.Store.User.SetPassword(c.Context(), u.ID, hash); err != nil {
		return response.Internal(c, err)
	}

	u.PasswordHash = hash

	// A password change that leaves old sessions alive is not a
	// password change - whoever prompted the reset may be holding a
	// live cookie.
	if _, err := h.Runtime.Store.Session.RevokeAllForUser(c.Context(), u.ID); err != nil {
		slog.Warn("auth: revoking sessions after reset failed", "user_id", u.ID, "err", err)
	}

	h.Runtime.Sessions.InvalidateAll()

	// Any other link mailed earlier dies with this one.
	if err := h.Runtime.Store.PasswordReset.InvalidateForUser(c.Context(), u.ID, now); err != nil {
		slog.Warn("auth: invalidating sibling reset tokens failed", "user_id", u.ID, "err", err)
	}

	slog.Info("auth: password reset completed", "user_id", u.ID, "client_ip", clientip.From(c))
	h.Runtime.Audit.Security(c, &amodel.Event{
		Type:       amodel.TypePasswordResetOK,
		ActorID:    u.ID,
		ActorEmail: u.Email,
		Status:     fiber.StatusOK,
	})

	return response.Success(c, MessageResponse{
		Message: "Password updated. You can sign in with it now.",
	})
}

// ChangePassword is the signed-in change: prove the current password,
// set a new one.
//
// It did not exist. The only way to a new password was the mailed
// reset link, which needs system mail configured, so on an install
// without it nobody could ever change their own password - and the
// profile page said as much, telling every user to ask an
// administrator.
//
// Refused for an account an identity provider owns: there is no local
// password to change, and writing one would create a way in the IdP
// knows nothing about.
func (h *Handler) ChangePassword(c fiber.Ctx) error {
	rc := domain.GetRequestContext(c)
	if rc == nil || rc.User == nil {
		return response.Unauthorized(c, "not authenticated")
	}

	in, resp, ok := validation.Bind[changePasswordInput](c)
	if !ok {
		return resp
	}

	// Read the row rather than trusting the session copy: the hash is
	// what the current password is checked against.
	u, err := h.Runtime.Store.User.GetByID(c.Context(), rc.User.ID)
	if err != nil {
		return response.Internal(c, err)
	}

	if u == nil {
		return response.Unauthorized(c, "not authenticated")
	}

	if !u.ManagesOwnCredentials() {
		return response.Forbidden(c,
			"this account signs in through an identity provider, change the password there")
	}

	if refusal, refused := h.refuseReauth(c, u, in.CurrentPassword, amodel.TypePasswordChanged, "a password change"); refused {
		return refusal
	}

	hash, err := authenticator.HashPassword(in.Password)
	if err != nil {
		return response.Internal(c, err)
	}

	// SetPassword, not Put: Put writes the whole row from a User read
	// before this request did anything, so an administrator disabling the
	// account in between had that undone - and the account walked back in
	// through its own password reset.
	if err := h.Runtime.Store.User.SetPassword(c.Context(), u.ID, hash); err != nil {
		return response.Internal(c, err)
	}

	u.PasswordHash = hash

	// Every other session goes, for the same reason a reset takes
	// them: a password change is worthless while a stolen cookie is
	// still live. The caller's own session is re-issued below so they
	// are not signed out of the page they are standing on.
	if _, err := h.Runtime.Store.Session.RevokeAllForUser(c.Context(), u.ID); err != nil {
		slog.Warn("auth: revoking sessions after password change failed", "user_id", u.ID, "err", err)
	}

	h.Runtime.Sessions.InvalidateAll()

	// Outstanding RESET links die too, for the same reason the sessions
	// do. The store documents InvalidateForUser as called "whenever the
	// password changes by any other route" and this route did not call
	// it - so somebody who changed their password because they suspected
	// their mailbox was compromised left a link the attacker had already
	// taken redeemable for the rest of its window, which sets the
	// password again and undoes the change.
	if err := h.Runtime.Store.PasswordReset.InvalidateForUser(c.Context(), u.ID, time.Now().UTC()); err != nil {
		slog.Warn("auth: invalidating reset tokens after password change failed", "user_id", u.ID, "err", err)
	}

	if err := h.startSession(c, u, ""); err != nil {
		return response.Internal(c, err)
	}

	slog.Info("auth: password changed", "user_id", u.ID, "client_ip", clientip.From(c))
	h.Runtime.Audit.Security(c, &amodel.Event{
		Type:       amodel.TypePasswordChanged,
		ActorID:    u.ID,
		ActorEmail: u.Email,
		Status:     fiber.StatusOK,
	})

	return response.Success(c, MessageResponse{
		Message: "Password updated. Other sessions have been signed out.",
	})
}

// mailBudgetSpent decides whether one more link may be mailed for the
// account: the calling address has its own budget, and the account a
// larger one over all addresses. An IPv6 address is judged by its /64,
// the way the login lock judges it.
func (h *Handler) mailBudgetSpent(c fiber.Ctx, userID string, now time.Time, perIP, perAccount int,
	recent func(context.Context, string, time.Time) ([]string, error),
) (bool, error) {
	ips, err := recent(c.Context(), userID, now.Add(-time.Hour))
	if err != nil {
		return false, err
	}

	return budgetSpent(ips, clientip.From(c), perIP, perAccount), nil
}

// budgetSpent is mailBudgetSpent over the addresses already charged.
func budgetSpent(ips []string, caller string, perIP, perAccount int) bool {
	if len(ips) >= perAccount {
		return true
	}

	key, fromCaller := addressKey(caller), 0
	for _, ip := range ips {
		if addressKey(ip) == key {
			fromCaller++
		}
	}

	return fromCaller >= perIP
}
