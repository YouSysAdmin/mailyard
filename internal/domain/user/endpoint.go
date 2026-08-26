// Mailyard, Copyright (c) 2021-2026 YouSysAdmin

package user

import (
	"log/slog"
	"time"

	"github.com/gofiber/fiber/v3"
	"github.com/yousysadmin/mailyard/internal/core/ids"

	"github.com/yousysadmin/mailyard/internal/core/authenticator"
	"github.com/yousysadmin/mailyard/internal/core/env"
	"github.com/yousysadmin/mailyard/internal/core/response"
	"github.com/yousysadmin/mailyard/internal/core/validation"
	"github.com/yousysadmin/mailyard/internal/domain"
	amodel "github.com/yousysadmin/mailyard/internal/models/audit"
	projmodel "github.com/yousysadmin/mailyard/internal/models/project"
	usermodel "github.com/yousysadmin/mailyard/internal/models/user"
)

// Handler owns the /api/users CRUD surface. All routes are mounted
// behind requireAuth + requireAdmin (see server/routes.go), so by the
// time a handler runs the caller is an authenticated admin - the
// only per-handler checks left are the self-lockout guards.
type Handler struct {
	Runtime *env.Runtime
}

// List returns every user, oldest first. Always an array ("users": []),
// never null, so the SPA can .map() without a guard.
func (h *Handler) List(c fiber.Ctx) error {
	users, err := h.Runtime.Store.User.List(c.Context())
	if err != nil {
		return response.Internal(c, err)
	}

	if users == nil {
		users = []*usermodel.User{}
	}

	return response.Success(c, ListResponse{Users: users})
}

// Get returns a single user by id.
func (h *Handler) Get(c fiber.Ctx) error {
	u, err := h.Runtime.Store.User.GetByID(c.Context(), c.Params("id"))
	if err != nil {
		return response.Internal(c, err)
	}

	if u == nil {
		return response.NotFound(c, "user not found")
	}

	return response.Success(c, UserResponse{User: u})
}

// Create adds a user. Duplicate emails are rejected with 409 so the
// SPA can point at the field instead of showing a generic failure.
func (h *Handler) Create(c fiber.Ctx) error {
	in, resp, ok := validation.Bind[createInput](c)
	if !ok {
		return resp
	}

	existing, err := h.Runtime.Store.User.Get(c.Context(), in.Email)
	if err != nil {
		return response.Internal(c, err)
	}

	if existing != nil {
		return response.Conflict(c, "a user with this email already exists")
	}

	// Admin-created accounts are verified by fiat - the admin typed
	// the address on purpose, there is no stranger to gate.
	//
	// AccountLocal even when no password is supplied: an admin adding
	// somebody here is creating an ordinary account on this install,
	// and it is the identity provider that mints AccountOIDC, on its
	// own path. Reading this as "managed elsewhere" would leave the
	// person unable to change their own password afterwards.
	u := &usermodel.User{
		ID:            ids.New(),
		Email:         in.Email,
		AccountType:   usermodel.AccountLocal,
		Admin:         in.Admin,
		EmailVerified: true,
	}
	if in.Password != "" {
		hash, err := authenticator.HashPassword(in.Password)
		if err != nil {
			return response.Internal(c, err)
		}

		u.PasswordHash = hash
	}

	if err := h.Runtime.Store.User.Put(c.Context(), u); err != nil {
		return response.Internal(c, err)
	}

	return response.Created(c, UserResponse{User: u})
}

// Update patches a user. Self-edits are limited to email + password:
// changing your own admin / disabled flags is refused so
// an admin can't demote or lock out the account they're driving with.
func (h *Handler) Update(c fiber.Ctx) error {
	in, resp, ok := validation.Bind[updateInput](c)
	if !ok {
		return resp
	}

	u, err := h.Runtime.Store.User.GetByID(c.Context(), c.Params("id"))
	if err != nil {
		return response.Internal(c, err)
	}

	if u == nil {
		return response.NotFound(c, "user not found")
	}

	if isSelf(c, u.ID) && (in.Admin != nil || in.Disabled != nil) {
		return response.Forbidden(c, "cannot change your own admin or disabled flags")
	}

	if in.Email != "" && in.Email != u.Email {
		other, err := h.Runtime.Store.User.Get(c.Context(), in.Email)
		if err != nil {
			return response.Internal(c, err)
		}

		if other != nil {
			return response.Conflict(c, "a user with this email already exists")
		}

		u.Email = in.Email
	}

	if in.Password != "" {
		hash, err := authenticator.HashPassword(in.Password)
		if err != nil {
			return response.Internal(c, err)
		}

		u.PasswordHash = hash
	}

	if in.Admin != nil {
		u.Admin = *in.Admin
	}

	if in.Disabled != nil {
		u.Disabled = *in.Disabled
	}

	if in.EmailVerified != nil {
		u.EmailVerified = *in.EmailVerified
	}

	// Put, deliberately: this is the administrator's editor for the whole
	// row, every column it writes is a field on that form, and
	// last-writer-wins between two administrators editing one account is
	// what an editor means. The paths that changed to targeted UPDATEs
	// are the ones that touch one fact as a side effect of doing
	// something else - a password reset, a second-factor enrolment -
	// where rewriting `disabled` was never anybody's intention. The
	// remaining exposure here is narrow and known: an admin saving this
	// form at the instant the account owner enrols a second factor writes
	// the pre-enrolment totp columns back.
	if err := h.Runtime.Store.User.Put(c.Context(), u); err != nil {
		return response.Internal(c, err)
	}

	// An administrator setting a password evicts whoever was using the
	// old one, exactly as the two self-service paths do.
	//
	// It did not, and this is the path an admin reaches for when an
	// account is compromised: the new password was written and the
	// intruder's cookie stayed live for the rest of the session TTL,
	// along with any reset link they had already been mailed. Disabling
	// the account WAS immediate (stampSession re-reads the row per
	// request) so the two controls behaved differently in the one
	// situation where they are used together.
	if in.Password != "" {
		if _, err := h.Runtime.Store.Session.RevokeAllForUser(c.Context(), u.ID); err != nil {
			slog.Warn("users: revoking sessions after an admin password set failed",
				"user_id", u.ID, "err", err)
		}

		h.Runtime.Sessions.InvalidateAll()
		if err := h.Runtime.Store.PasswordReset.InvalidateForUser(c.Context(), u.ID, time.Now().UTC()); err != nil {
			slog.Warn("users: invalidating reset tokens after an admin password set failed",
				"user_id", u.ID, "err", err)
		}
	}

	return response.Success(c, UserResponse{User: u})
}

// Delete removes a user by id. Deleting yourself is refused (lockout
// guard) - deleting a missing id is a 404 so a double-click in the UI
// surfaces as "already gone" rather than silent success.
func (h *Handler) Delete(c fiber.Ctx) error {
	id := c.Params("id")
	if isSelf(c, id) {
		return response.Forbidden(c, "cannot delete your own account")
	}

	u, err := h.Runtime.Store.User.GetByID(c.Context(), id)
	if err != nil {
		return response.Internal(c, err)
	}

	if u == nil {
		return response.NotFound(c, "user not found")
	}

	if err := h.Runtime.Store.User.Delete(c.Context(), id); err != nil {
		return response.Internal(c, err)
	}

	return response.NoContent(c)
}

// ResetTOTP removes a user's second factor, for the operator whose
// user lost their phone. Refused on your own account: the self-service
// path (profile, 2FA disable) proves possession with a code, and this
// endpoint deliberately does not - allowing it on yourself would turn
// any hijacked admin session into a 2FA bypass for that admin.
func (h *Handler) ResetTOTP(c fiber.Ctx) error {
	id := c.Params("id")
	if isSelf(c, id) {
		return response.Forbidden(c, "disable your own 2FA from your profile, with a code")
	}

	u, err := h.Runtime.Store.User.GetByID(c.Context(), id)
	if err != nil {
		return response.Internal(c, err)
	}

	if u == nil {
		return response.NotFound(c, "user not found")
	}

	if !u.TOTPEnabled && u.TOTPSecret == "" {
		return response.BadRequest(c, "two-factor auth is not enabled for this user")
	}

	u.TOTPSecret = ""
	u.TOTPEnabled = false

	// SetTOTP, not Put. Clearing somebody's second factor is not a
	// statement about the rest of their account, and Put would have
	// written every other column from a read taken moments earlier -
	// including `disabled`, which is exactly what an administrator may be
	// setting at the same time on a compromised account.
	if err := h.Runtime.Store.User.SetTOTP(c.Context(), u.ID, "", false); err != nil {
		return response.Internal(c, err)
	}

	if err := h.Runtime.Store.User.DeleteRecoveryCodes(c.Context(), u.ID); err != nil {
		return response.Internal(c, err)
	}

	rc := domain.GetRequestContext(c)
	ev := &amodel.Event{
		Type:   amodel.TypeTOTPReset,
		Status: fiber.StatusOK,
		Detail: "2FA reset by admin for " + u.Email,
	}
	if rc != nil && rc.User != nil {
		ev.ActorID = rc.User.ID
		ev.ActorEmail = rc.User.Email
	}

	h.Runtime.Audit.Security(c, ev)

	return response.Success(c, UserResponse{User: u})
}

// ResetPasskeys clears every passkey on an account, for the operator
// whose user lost the device that held the only one.
//
// Refused on your own account, exactly as ResetTOTP is: removing a
// passkey from the profile asks for the account password, and this
// endpoint deliberately does not, so allowing it on yourself would let
// a hijacked admin session strip the phishing-resistant factor off the
// admin it hijacked.
func (h *Handler) ResetPasskeys(c fiber.Ctx) error {
	id := c.Params("id")
	if isSelf(c, id) {
		return response.Forbidden(c, "remove your own passkeys from your profile, with your password")
	}

	u, err := h.Runtime.Store.User.GetByID(c.Context(), id)
	if err != nil {
		return response.Internal(c, err)
	}

	if u == nil {
		return response.NotFound(c, "user not found")
	}

	n, err := h.Runtime.Store.Passkey.DeleteAllForUser(c.Context(), u.ID)
	if err != nil {
		return response.Internal(c, err)
	}

	if n == 0 {
		return response.BadRequest(c, "this user has no passkeys")
	}

	rc := domain.GetRequestContext(c)
	ev := &amodel.Event{
		Type:   amodel.TypePasskeyReset,
		Status: fiber.StatusOK,
		Detail: "passkeys reset by admin for " + u.Email,
	}
	if rc != nil && rc.User != nil {
		ev.ActorID = rc.User.ID
		ev.ActorEmail = rc.User.Email
	}

	h.Runtime.Audit.Security(c, ev)

	return response.Success(c, PasskeyResetResponse{Removed: n})
}

// RevokeSessions kills every session the user holds. Allowed on
// yourself - it is explicit, and signing yourself out everywhere is a
// legitimate thing to want.
func (h *Handler) RevokeSessions(c fiber.Ctx) error {
	id := c.Params("id")
	u, err := h.Runtime.Store.User.GetByID(c.Context(), id)
	if err != nil {
		return response.Internal(c, err)
	}

	if u == nil {
		return response.NotFound(c, "user not found")
	}

	n, err := h.Runtime.Store.Session.RevokeAllForUser(c.Context(), id)
	if err != nil {
		return response.Internal(c, err)
	}

	// The blanket invalidation makes this node consistent immediately.
	// Other nodes converge within the cache TTL.
	h.Runtime.Sessions.InvalidateAll()

	rc := domain.GetRequestContext(c)
	ev := &amodel.Event{
		Type:   amodel.TypeSessionRevoked,
		Status: fiber.StatusOK,
		Detail: "all sessions revoked by admin for " + u.Email,
	}
	if rc != nil && rc.User != nil {
		ev.ActorID = rc.User.ID
		ev.ActorEmail = rc.User.Email
	}

	h.Runtime.Audit.Security(c, ev)

	return response.Success(c, RevokedResponse{Revoked: n})
}

// Projects lists every project the user is a member of, so an
// admin can see what an account touches before disabling or deleting it.
func (h *Handler) Projects(c fiber.Ctx) error {
	id := c.Params("id")
	u, err := h.Runtime.Store.User.GetByID(c.Context(), id)
	if err != nil {
		return response.Internal(c, err)
	}

	if u == nil {
		return response.NotFound(c, "user not found")
	}

	proj, err := h.Runtime.Store.Project.ListForUser(c.Context(), id)
	if err != nil {
		return response.Internal(c, err)
	}

	if proj == nil {
		proj = []*projmodel.Project{}
	}

	return response.Success(c, ProjectsResponse{Projects: proj})
}

// isSelf reports whether id is the authenticated caller. False when
// there's no resolved user (auth disabled) - with no session there's
// no account to lock out.
func isSelf(c fiber.Ctx, id string) bool {
	rc := domain.GetRequestContext(c)

	return rc != nil && rc.User != nil && rc.User.ID == id
}
