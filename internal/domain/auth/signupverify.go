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

	"github.com/yousysadmin/mailyard/internal/core/env"
	"github.com/yousysadmin/mailyard/internal/core/response"
	"github.com/yousysadmin/mailyard/internal/core/systemmail"
	"github.com/yousysadmin/mailyard/internal/core/validation"
	amodel "github.com/yousysadmin/mailyard/internal/models/audit"
	svmodel "github.com/yousysadmin/mailyard/internal/models/signupverify"
	usermodel "github.com/yousysadmin/mailyard/internal/models/user"
)

// maxVerifyMailsPerHour caps how many verification links one account
// can be mailed. Same reasoning as maxResetsPerHour: the resend
// endpoint is unauthenticated and must not be a mail cannon.
const maxVerifyMailsPerHour = 3

// sendVerificationMail mints a token and mails the confirm link.
// Shared by Register and VerifyEmailResend so the two cannot drift on
// TTL or link shape.
func (h *Handler) sendVerificationMail(ctx context.Context, u *usermodel.User, clientIP string) error {
	plaintext, hash, err := svmodel.Generate()
	if err != nil {
		return err
	}

	now := time.Now().UTC()
	tok := &svmodel.Token{
		ID:        ids.New(),
		UserID:    u.ID,
		TokenHash: hash,
		ExpiresAt: now.Add(svmodel.TTL),
		CreatedAt: now,
		RequestIP: clientIP,
	}
	if err := h.Runtime.Store.SignupVerify.Put(ctx, tok); err != nil {
		return err
	}

	link := strings.TrimRight(h.Runtime.Config.Server.PublicURL, "/") +
		env.ConsolePath + "/verify-email?token=" + plaintext
	subject, htmlBody, textBody := systemmail.SignupVerification(link, int(svmodel.TTL.Hours()))
	// Async for the same reason as password reset: the caller must not
	// learn from response timing whether a mail was dispatched.
	h.Runtime.SystemMail.SendAsync([]string{u.Email}, subject, htmlBody, textBody)

	return nil
}

// VerifyEmailConfirm redeems a verification token, marks the account
// verified, and signs it in - clicking the link proves the mailbox,
// which is a stronger claim than the password typed minutes earlier.
func (h *Handler) VerifyEmailConfirm(c fiber.Ctx) error {
	if !h.Runtime.Config.Auth.Local.Enabled {
		return response.BadRequest(c, "local login is disabled")
	}

	in, resp, ok := validation.Bind[verifyConfirmInput](c)
	if !ok {
		return resp
	}

	tok, err := h.Runtime.Store.SignupVerify.GetByHash(c.Context(), svmodel.Hash(in.Token))
	if err != nil {
		return response.Internal(c, err)
	}

	now := time.Now().UTC()

	// One message for every failure leg, exactly like password reset:
	// expired, already spent, and never existed are indistinguishable.
	if tok == nil || !tok.Usable(now) {
		slog.Warn("auth: signup verification token rejected", "client_ip", clientip.From(c))

		return response.BadRequest(c, "this verification link is invalid or has expired, request a new one from the sign-in page")
	}

	u, err := h.Runtime.Store.User.GetByID(c.Context(), tok.UserID)
	if err != nil {
		return response.Internal(c, err)
	}

	if u == nil || u.Disabled {
		return response.BadRequest(c, "this verification link is invalid or has expired, request a new one from the sign-in page")
	}

	// Burn the token before changing anything - the conditional UPDATE
	// is what makes the link single use, and it only counts if the
	// loser of a race stops here.
	claimed, err := h.Runtime.Store.SignupVerify.MarkUsed(c.Context(), tok.ID, now)
	if err != nil {
		return response.Internal(c, err)
	}

	if !claimed {
		slog.Warn("auth: signup verification token already spent", "token_id", tok.ID, "client_ip", clientip.From(c))

		return response.BadRequest(c, "this verification link is invalid or has expired, request a new one from the sign-in page")
	}

	if err := h.Runtime.Store.User.MarkEmailVerified(c.Context(), u.ID); err != nil {
		return response.Internal(c, err)
	}

	u.EmailVerified = true

	// Any other link mailed earlier dies with this one.
	if err := h.Runtime.Store.SignupVerify.InvalidateForUser(c.Context(), u.ID, now); err != nil {
		slog.Warn("auth: invalidating sibling verification tokens failed", "user_id", u.ID, "err", err)
	}

	slog.Info("auth: signup email verified", "user_id", u.ID)
	h.Runtime.Audit.Security(c, &amodel.Event{
		Type:       amodel.TypeEmailVerified,
		ActorID:    u.ID,
		ActorEmail: u.Email,
		Status:     fiber.StatusOK,
	})

	if err := h.startSession(c, u, ""); err != nil {
		return response.Internal(c, err)
	}

	if err := h.Runtime.Store.User.TouchLastLogin(c.Context(), u.Email); err != nil {
		slog.Warn("auth: touch last login failed", "email", u.Email, "err", err)
	}

	return response.Success(c, UserResponse{User: u})
}

// VerifyEmailResend mails a fresh verification link.
//
// The response is identical whether the address is unknown, disabled,
// or already verified - an unauthenticated endpoint that
// distinguishes them is an account enumeration oracle.
func (h *Handler) VerifyEmailResend(c fiber.Ctx) error {
	if !h.Runtime.Config.Auth.Local.Enabled {
		return response.BadRequest(c, "local login is disabled")
	}

	if !h.Runtime.SystemMail.Enabled() {
		return response.BadRequest(c,
			"email verification is unavailable because this install has no system mail configured, ask an administrator to verify your account")
	}

	in, resp, ok := validation.Bind[verifyResendInput](c)
	if !ok {
		return resp
	}

	accepted := func() error {
		return response.Success(c, MessageResponse{
			Message: "If that address has an unverified account, a new link is on its way.",
		})
	}

	u, err := h.Runtime.Store.User.Get(c.Context(), in.Email)
	if err != nil {
		return response.Internal(c, err)
	}

	if u == nil || u.Disabled || u.EmailVerified {
		slog.Info("auth: verification resend for an address with no unverified account", "client_ip", clientip.From(c))

		return accepted()
	}

	now := time.Now().UTC()
	recent, err := h.Runtime.Store.SignupVerify.CountRecentForUser(c.Context(), u.ID, now.Add(-time.Hour))
	if err != nil {
		return response.Internal(c, err)
	}

	if recent >= maxVerifyMailsPerHour {
		slog.Warn("auth: verification resend throttled", "user_id", u.ID, "client_ip", clientip.From(c))

		return accepted()
	}

	if err := h.sendVerificationMail(c.Context(), u, clientip.From(c)); err != nil {
		return response.Internal(c, err)
	}

	slog.Info("auth: verification link resent", "user_id", u.ID, "client_ip", clientip.From(c))

	return accepted()
}
