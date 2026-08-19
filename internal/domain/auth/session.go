// Mailyard, Copyright (c) 2021-2026 YouSysAdmin

package auth

import (
	"log/slog"
	"time"

	"github.com/gofiber/fiber/v3"
	"github.com/yousysadmin/mailyard/internal/core/clientip"
	"github.com/yousysadmin/mailyard/internal/core/ids"
	"github.com/yousysadmin/mailyard/internal/core/safetext"

	"github.com/yousysadmin/mailyard/internal/core/authenticator"
	"github.com/yousysadmin/mailyard/internal/core/crypto"
	"github.com/yousysadmin/mailyard/internal/core/response"
	"github.com/yousysadmin/mailyard/internal/domain"
	amodel "github.com/yousysadmin/mailyard/internal/models/audit"
	sessmodel "github.com/yousysadmin/mailyard/internal/models/session"
	usermodel "github.com/yousysadmin/mailyard/internal/models/user"
)

// maxUserAgent bounds what a client can persist through the sessions
// table. Browsers send well under this - the cap is there so a hostile
// client cannot store kilobytes per sign-in.
const maxUserAgent = 400

// startSession mints a tracked session and its JWT, and sets the
// cookie. Shared by password login and the OIDC callback so both
// produce a revocable session with identical bookkeeping.
//
// The session row is written before the cookie is set. If the write
// fails the sign-in fails: handing out a token whose session row does
// not exist would produce a cookie the auth middleware rejects on the
// very next request.
// startSession mints a session row and the cookie that names it.
// authProviderID records how the user authenticated, empty for a
// local password login, and is what project SSO enforcement
// compares against later.
func (h *Handler) startSession(c fiber.Ctx, u *usermodel.User, authProviderID string) error {
	ttl := sessionTTL(h.Runtime)
	now := time.Now().UTC()

	// Through safetext: a byte cut here split multi-byte runes, and a
	// UA that arrived as invalid UTF-8 needed no cut at all - either
	// way Postgres refused the session INSERT with 22021 and a correct
	// password answered 500.
	ua := safetext.Clamp(c.Get(fiber.HeaderUserAgent), maxUserAgent)

	sess := &sessmodel.Session{
		ID:         ids.New(),
		UserID:     u.ID,
		UserAgent:  ua,
		IP:         clientip.From(c),
		CreatedAt:  now,
		LastSeenAt: now,
		ExpiresAt:  now.Add(ttl),

		AuthProviderID: authProviderID,
	}
	if err := h.Runtime.Store.Session.Put(c.Context(), sess); err != nil {
		return err
	}

	tok, err := authenticator.CreateToken(
		crypto.DeriveKey(h.Runtime.Config.Auth.JWTSecret, crypto.KeySession),
		u.ID, u.Email, sess.ID, ttl,
	)
	if err != nil {
		return err
	}

	c.Cookie(buildSessionCookie(h.Runtime, tok, ttl))

	return nil
}

// ListSessions returns the caller's active sign-ins, marking the one
// making the request.
func (h *Handler) ListSessions(c fiber.Ctx) error {
	rc := domain.GetRequestContext(c)
	if rc == nil || rc.User == nil {
		return response.Unauthorized(c, "not authenticated")
	}

	sessions, err := h.Runtime.Store.Session.ListForUser(c.Context(), rc.User.ID, time.Now().UTC())
	if err != nil {
		return response.Internal(c, err)
	}

	if sessions == nil {
		sessions = []*sessmodel.Session{}
	}

	current := rc.SessionID
	for _, s := range sessions {
		s.Current = s.ID == current
	}

	return response.Success(c, SessionListResponse{Sessions: sessions})
}

// RevokeSession kills one of the caller's sessions. Revoking the
// current one is allowed and behaves like signing out.
func (h *Handler) RevokeSession(c fiber.Ctx) error {
	rc := domain.GetRequestContext(c)
	if rc == nil || rc.User == nil {
		return response.Unauthorized(c, "not authenticated")
	}

	id := c.Params("id")
	// Scoped by user id, so a session belonging to somebody else is
	// reported as missing rather than refused.
	ok, err := h.Runtime.Store.Session.Revoke(c.Context(), rc.User.ID, id)
	if err != nil {
		return response.Internal(c, err)
	}

	if !ok {
		return response.NotFound(c, "session not found")
	}

	h.Runtime.Sessions.Invalidate(id)
	h.Runtime.Audit.Security(c, &amodel.Event{
		Type:       amodel.TypeSessionRevoked,
		ActorID:    rc.User.ID,
		ActorEmail: rc.User.Email,
		Status:     fiber.StatusOK,
		Detail:     "revoked session " + id,
	})
	if id == rc.SessionID {
		c.Cookie(buildSessionCookie(h.Runtime, "", -time.Hour))
	}

	slog.Info("auth: session revoked", "user_id", rc.User.ID, "session_id", id)

	return response.Success(c, RevokedResponse{Revoked: 1})
}

// RevokeOtherSessions signs the caller out everywhere except here.
func (h *Handler) RevokeOtherSessions(c fiber.Ctx) error {
	rc := domain.GetRequestContext(c)
	if rc == nil || rc.User == nil {
		return response.Unauthorized(c, "not authenticated")
	}

	// Collect the ids first so the local cache can be cleared - after
	// the update they are indistinguishable from long-revoked rows.
	existing, err := h.Runtime.Store.Session.ListForUser(c.Context(), rc.User.ID, time.Now().UTC())
	if err != nil {
		return response.Internal(c, err)
	}

	n, err := h.Runtime.Store.Session.RevokeOthers(c.Context(), rc.User.ID, rc.SessionID)
	if err != nil {
		return response.Internal(c, err)
	}

	for _, s := range existing {
		if s.ID != rc.SessionID {
			h.Runtime.Sessions.Invalidate(s.ID)
		}
	}

	h.Runtime.Audit.Security(c, &amodel.Event{
		Type:       amodel.TypeSessionRevoked,
		ActorID:    rc.User.ID,
		ActorEmail: rc.User.Email,
		Status:     fiber.StatusOK,
		Detail:     "revoked all other sessions",
	})
	slog.Info("auth: other sessions revoked", "user_id", rc.User.ID, "count", n)

	return response.Success(c, RevokedResponse{Revoked: n})
}
