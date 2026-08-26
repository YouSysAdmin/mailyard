// Mailyard, Copyright (c) 2021-2026 YouSysAdmin

package auth

import (
	"encoding/base64"
	"encoding/json/v2"
	"fmt"
	"log/slog"
	"net/url"
	"strings"
	"time"

	"github.com/gofiber/fiber/v3"
	"github.com/yousysadmin/mailyard/internal/core/clientip"
	"github.com/yousysadmin/mailyard/internal/core/ids"

	"github.com/yousysadmin/mailyard/internal/core/authenticator"
	"github.com/yousysadmin/mailyard/internal/core/crypto"
	corepasskey "github.com/yousysadmin/mailyard/internal/core/passkey"
	"github.com/yousysadmin/mailyard/internal/core/response"
	"github.com/yousysadmin/mailyard/internal/core/validation"
	"github.com/yousysadmin/mailyard/internal/domain"
	amodel "github.com/yousysadmin/mailyard/internal/models/audit"
	pkmodel "github.com/yousysadmin/mailyard/internal/models/passkey"
	usermodel "github.com/yousysadmin/mailyard/internal/models/user"
)

const (
	// Separate cookies for the two ceremonies. One name would let a
	// half-finished enrolment be handed to the login verifier, which
	// checks a different set of expectations.
	passkeyRegCookie   = "mailyard_passkey_reg"
	passkeyLoginCookie = "mailyard_passkey_login"

	// A ceremony is a few seconds of a person touching a sensor. Five
	// minutes is generous and still short enough that a stolen cookie
	// is rarely worth anything.
	passkeyCeremonyTTL = 5 * time.Minute

	// maxPasskeyName bounds the label. Display only, but it is stored
	// and echoed back.
	maxPasskeyName = 60

	// maxPasskeysPerUser stops an account accumulating credentials
	// without limit. Nobody legitimately carries twenty.
	maxPasskeysPerUser = 20
)

// passkeysAvailable reports whether the feature is on at all.
//
// Local auth is a hard requirement, not an accident of ordering: a
// passkey is a way into an account that the identity provider knows
// nothing about, so offering one on an SSO-backed install would mean
// disabling somebody in the IdP no longer locks them out.
func (h *Handler) passkeysAvailable() bool {
	cfg := h.Runtime.Config.Auth

	return !cfg.Disabled && cfg.PasskeysEnabled && cfg.Local.Enabled
}

// ceremonySealer seals the WebAuthn ceremony cookie under its own
// HKDF subkey. Never the at-rest service: that one degrades to plain
// base64 when no encryption key is set, and a forgeable challenge is
// a different class of problem from an unencrypted column.
func (h *Handler) ceremonySealer() *crypto.Service {
	return crypto.NewFor(h.Runtime.Config.Auth.JWTSecret, crypto.KeyPasskey)
}

// webauthnService builds the relying party from the browser's Origin,
// checked against server.public_url when there is one.
//
// The Origin is what the browser signs into the client data, so
// deriving the RP from it keeps the check equal to what the
// authenticator actually bound. Taken on its own word, though, the
// header would make the relying party whatever the caller declared,
// and the library's origin and rpIdHash checks would compare the
// assertion against the very value the request supplied. So the host
// has to be public_url's, or a subdomain of it: the name under which
// the passkey was enrolled. With no public_url configured the header
// stands alone, which is the development case.
func (h *Handler) webauthnService(c fiber.Ctx) (*corepasskey.Service, error) {
	origin := c.Get(fiber.HeaderOrigin)
	if origin == "" {
		return nil, fmt.Errorf("passkeys need a browser that sends an Origin header")
	}

	u, err := url.Parse(origin)
	if err != nil || u.Hostname() == "" {
		return nil, fmt.Errorf("the Origin header is not a usable origin")
	}

	if want := h.Runtime.Config.TLSHost(); want != "" && !sameOrUnder(u.Hostname(), want) {
		return nil, fmt.Errorf("the Origin %q is not the configured server.public_url host %q", u.Hostname(), want)
	}

	return corepasskey.New(u.Hostname(), origin)
}

// sameOrUnder reports whether host is name itself or a subdomain of it,
// compared case-insensitively.
func sameOrUnder(host, name string) bool {
	host, name = strings.ToLower(host), strings.ToLower(name)

	return host == name || strings.HasSuffix(host, "."+name)
}

func (h *Handler) setCeremony(c fiber.Ctx, name string, sess *corepasskey.SessionData) error {
	sealer := h.ceremonySealer()
	if !sealer.Enabled() {
		return fmt.Errorf("no auth.jwt_secret configured")
	}

	b, err := json.Marshal(sess)
	if err != nil {
		return err
	}

	enc, err := sealer.Encrypt(string(b))
	if err != nil {
		return err
	}

	c.Cookie(&fiber.Cookie{
		Name:     name,
		Value:    enc,
		Path:     "/",
		HTTPOnly: true,
		SameSite: "Strict",
		Secure:   cookieSecure(c, h.Runtime),
		Expires:  time.Now().Add(passkeyCeremonyTTL),
	})

	return nil
}

// takeCeremony reads the ceremony cookie and CLEARS it, so a
// challenge is answerable exactly once. Leaving it in place would let
// the same challenge be replayed for as long as the cookie lived.
func (h *Handler) takeCeremony(c fiber.Ctx, name string) (*corepasskey.SessionData, error) {
	raw := c.Cookies(name)
	c.Cookie(&fiber.Cookie{
		Name: name, Value: "", Path: "/", HTTPOnly: true,
		SameSite: "Strict", Secure: cookieSecure(c, h.Runtime),
		Expires: time.Unix(0, 0), MaxAge: -1,
	})
	if raw == "" {
		return nil, fmt.Errorf("no ceremony in progress")
	}

	plain, err := h.ceremonySealer().Decrypt(raw)
	if err != nil {
		return nil, err
	}

	var sd corepasskey.SessionData
	if err := json.Unmarshal([]byte(plain), &sd); err != nil {
		return nil, err
	}

	return &sd, nil
}

// passkeySelf loads the caller's account and refuses the ones that
// cannot hold a passkey.
//
// Returns (user, resp, ok) rather than (user, error), mirroring
// validation.Bind. The response.* helpers write the status and return
// nil, so a single error return is always nil and every caller
// carries on past the refusal into a nil user. That is not
// hypothetical - it is what a lone error return produces here, and the
// SSO-only leg answered 500 from a recovered nil dereference instead
// of 403.
func (h *Handler) passkeySelf(c fiber.Ctx) (*usermodel.User, error, bool) {
	if !h.passkeysAvailable() {
		return nil, response.Forbidden(c, "passkeys are not enabled on this install"), false
	}

	rc := domain.GetRequestContext(c)
	if rc == nil || rc.User == nil {
		return nil, response.Unauthorized(c, "not authenticated"), false
	}

	u, err := h.Runtime.Store.User.GetByID(c.Context(), rc.User.ID)
	if err != nil {
		return nil, response.Internal(c, err), false
	}

	if u == nil {
		return nil, response.Unauthorized(c, "not authenticated"), false
	}

	// One question, one answer. Testing PasswordHash == "" directly is
	// the derivation account_type replaced, and it disagrees with the
	// other two gates the moment a row has both a hash and an identity
	// provider.
	if !u.ManagesOwnCredentials() {
		return nil, response.Forbidden(c,
			"passkeys are available on local accounts only, this account signs in through an identity provider"), false
	}

	return u, nil, true
}

// webauthnUser assembles the library's view of an account: the handle
// plus every credential already enrolled.
//
// The handle is the account id. See the migration for why there is no
// separate column for it.
func (h *Handler) webauthnUser(c fiber.Ctx, u *usermodel.User) (*corepasskey.User, []*pkmodel.Passkey, error) {
	rows, err := h.Runtime.Store.Passkey.ListForUser(c.Context(), u.ID)
	if err != nil {
		return nil, nil, err
	}

	creds := make([]corepasskey.Credential, 0, len(rows))
	for _, r := range rows {
		cred, err := corepasskey.DecodeCredential(r.Credential)
		if err != nil {
			// One unreadable row must not make the account
			// unusable - skip it and say so.
			slog.Error("auth: stored passkey is unreadable", "passkey_id", r.ID, "err", err)
			continue
		}

		creds = append(creds, *cred)
	}

	return &corepasskey.User{Handle: []byte(u.ID), Name: u.Email, Display: u.Email, Creds: creds}, rows, nil
}

// PasskeyList returns the caller's enrolled passkeys.
func (h *Handler) PasskeyList(c fiber.Ctx) error {
	u, resp, ok := h.passkeySelf(c)
	if !ok {
		return resp
	}

	rows, err := h.Runtime.Store.Passkey.ListForUser(c.Context(), u.ID)
	if err != nil {
		return response.Internal(c, err)
	}

	if rows == nil {
		rows = []*pkmodel.Passkey{}
	}

	return response.Success(c, PasskeyListResponse{Passkeys: rows})
}

// PasskeyRegisterBegin starts enrolment.
func (h *Handler) PasskeyRegisterBegin(c fiber.Ctx) error {
	u, resp, ok := h.passkeySelf(c)
	if !ok {
		return resp
	}

	in, bindResp, bindOK := validation.Bind[passkeyReauthInput](c)
	if !bindOK {
		return bindResp
	}

	if !authenticator.VerifyPassword(u.PasswordHash, in.Password) {
		h.Runtime.Audit.Security(c, &amodel.Event{
			Type: amodel.TypeLoginFailed, ActorID: u.ID, ActorEmail: u.Email, Status: fiber.StatusForbidden,
			Detail: "wrong password confirming passkey enrolment",
		})

		return response.Forbidden(c, "incorrect password")
	}

	n, err := h.Runtime.Store.Passkey.CountForUser(c.Context(), u.ID)
	if err != nil {
		return response.Internal(c, err)
	}

	if n >= maxPasskeysPerUser {
		return response.Conflict(c, fmt.Sprintf("this account already holds %d passkeys, remove one first", n))
	}

	svc, err := h.webauthnService(c)
	if err != nil {
		return response.BadRequest(c, err.Error())
	}

	wu, _, err := h.webauthnUser(c, u)
	if err != nil {
		return response.Internal(c, err)
	}

	opts, sess, err := svc.BeginRegistration(wu)
	if err != nil {
		return response.Internal(c, err)
	}

	if err := h.setCeremony(c, passkeyRegCookie, sess); err != nil {
		return response.Internal(c, err)
	}

	return response.Success(c, opts)
}

// PasskeyRegisterFinish verifies the attestation and stores the
// credential.
//
// The body is the raw WebAuthn attestation, exactly as the browser
// produced it, so the label rides in ?name= rather than wrapping the
// body in an envelope the parser would have to be taught about.
func (h *Handler) PasskeyRegisterFinish(c fiber.Ctx) error {
	u, resp, ok := h.passkeySelf(c)
	if !ok {
		return resp
	}

	svc, err := h.webauthnService(c)
	if err != nil {
		return response.BadRequest(c, err.Error())
	}

	sess, err := h.takeCeremony(c, passkeyRegCookie)
	if err != nil {
		return response.BadRequest(c, "the enrolment expired, start again")
	}

	wu, _, err := h.webauthnUser(c, u)
	if err != nil {
		return response.Internal(c, err)
	}

	cred, err := svc.FinishRegistration(wu, *sess, c.Body())
	if err != nil {
		slog.Warn("auth: passkey enrolment rejected", "user_id", u.ID, "err", err)

		return response.BadRequest(c, "the passkey could not be verified")
	}

	encoded, err := corepasskey.EncodeCredential(cred)
	if err != nil {
		return response.Internal(c, err)
	}

	name := c.Query("name")
	if len(name) > maxPasskeyName {
		name = name[:maxPasskeyName]
	}

	if name == "" {
		name = "Passkey"
	}

	row := &pkmodel.Passkey{
		ID:           ids.New(),
		UserID:       u.ID,
		CredentialID: base64.RawURLEncoding.EncodeToString(cred.ID),
		Name:         name,
		Credential:   encoded,
		CreatedAt:    time.Now().UTC(),
	}
	if err := h.Runtime.Store.Passkey.Put(c.Context(), row); err != nil {
		return response.Internal(c, err)
	}

	slog.Info("auth: passkey enrolled", "user_id", u.ID, "passkey_id", row.ID)
	h.Runtime.Audit.Security(c, &amodel.Event{
		Type: amodel.TypePasskeyAdded, ActorID: u.ID, ActorEmail: u.Email, Status: fiber.StatusCreated, Detail: "added passkey " + name,
	})

	return response.Created(c, PasskeyResponse{Passkey: row})
}

// PasskeyRename relabels one. No password: a label is not a way in.
func (h *Handler) PasskeyRename(c fiber.Ctx) error {
	u, resp, ok := h.passkeySelf(c)
	if !ok {
		return resp
	}

	in, bindResp, bindOK := validation.Bind[passkeyRenameInput](c)
	if !bindOK {
		return bindResp
	}

	ok, err := h.Runtime.Store.Passkey.Rename(c.Context(), u.ID, c.Params("id"), in.Name)
	if err != nil {
		return response.Internal(c, err)
	}

	if !ok {
		return response.NotFound(c, "passkey not found")
	}

	return response.Success(c, RenamedResponse{Renamed: true})
}

// PasskeyDelete removes one, behind the same password confirmation as
// enrolment.
func (h *Handler) PasskeyDelete(c fiber.Ctx) error {
	u, resp, ok := h.passkeySelf(c)
	if !ok {
		return resp
	}

	in, bindResp, bindOK := validation.Bind[passkeyReauthInput](c)
	if !bindOK {
		return bindResp
	}

	if !authenticator.VerifyPassword(u.PasswordHash, in.Password) {
		return response.Forbidden(c, "incorrect password")
	}

	removed, err := h.Runtime.Store.Passkey.Delete(c.Context(), u.ID, c.Params("id"))
	if err != nil {
		return response.Internal(c, err)
	}

	if !removed {
		return response.NotFound(c, "passkey not found")
	}

	slog.Info("auth: passkey removed", "user_id", u.ID, "passkey_id", c.Params("id"))
	h.Runtime.Audit.Security(c, &amodel.Event{
		Type: amodel.TypePasskeyRemoved, ActorID: u.ID, ActorEmail: u.Email, Status: fiber.StatusOK, Detail: "removed passkey " + c.Params("id"),
	})

	return response.Success(c, RemovedResponse{Removed: true})
}

// PasskeyLoginBegin starts a usernameless assertion. Open endpoint:
// nobody has identified themselves yet, which is the point.
func (h *Handler) PasskeyLoginBegin(c fiber.Ctx) error {
	if !h.passkeysAvailable() {
		return response.BadRequest(c, "passkey sign-in is not available")
	}

	svc, err := h.webauthnService(c)
	if err != nil {
		return response.BadRequest(c, err.Error())
	}

	opts, sess, err := svc.BeginLogin()
	if err != nil {
		return response.Internal(c, err)
	}

	if err := h.setCeremony(c, passkeyLoginCookie, sess); err != nil {
		return response.Internal(c, err)
	}

	return response.Success(c, opts)
}

// PasskeyLoginFinish verifies the assertion and signs the owner in.
//
// A passkey completes sign-in on its own and does not go on to ask
// for a TOTP code. User verification was required at both enrolment
// and assertion, so the person proved possession of the authenticator
// AND unlocked it - two factors in one gesture, and a phishing-proof
// pair, which is more than a password plus a code that can be typed
// into somebody else's page.
func (h *Handler) PasskeyLoginFinish(c fiber.Ctx) error {
	if !h.passkeysAvailable() {
		return response.BadRequest(c, "passkey sign-in is not available")
	}

	svc, err := h.webauthnService(c)
	if err != nil {
		return response.BadRequest(c, err.Error())
	}

	sess, err := h.takeCeremony(c, passkeyLoginCookie)
	if err != nil {
		return response.BadRequest(c, "the sign-in expired, start again")
	}

	ctx := c.Context()

	var (
		matched *usermodel.User
		row     *pkmodel.Passkey
	)

	// The credential id IS the identity claim here, and the signature
	// the library checks afterwards is what makes it true. The handle
	// is compared as well: a credential that resolves to one account
	// must not be asserted under another's handle.
	resolve := func(rawID, userHandle []byte) (*corepasskey.User, error) {
		pk, err := h.Runtime.Store.Passkey.GetByCredential(ctx,
			base64.RawURLEncoding.EncodeToString(rawID))
		if err != nil {
			return nil, err
		}

		if pk == nil {
			return nil, fmt.Errorf("unknown passkey")
		}

		if pk.UserID != string(userHandle) {
			return nil, fmt.Errorf("passkey handle mismatch")
		}

		u, err := h.Runtime.Store.User.GetByID(ctx, pk.UserID)
		if err != nil {
			return nil, err
		}

		if u == nil || u.Disabled {
			return nil, fmt.Errorf("account unavailable")
		}

		cred, err := corepasskey.DecodeCredential(pk.Credential)
		if err != nil {
			return nil, err
		}

		matched, row = u, pk

		return &corepasskey.User{
			Handle: []byte(u.ID), Name: u.Email, Display: u.Email,
			Creds: []corepasskey.Credential{*cred},
		}, nil
	}

	cred, _, err := svc.FinishLogin(resolve, *sess, c.Body())
	if err != nil || matched == nil || row == nil {
		slog.Warn("auth: passkey sign-in failed", "client_ip", clientip.From(c), "err", err)
		h.recordLoginFailure(c, matched, emailOf(matched), "passkey assertion rejected")

		return response.Unauthorized(c, "passkey sign-in failed")
	}

	// Per the WebAuthn spec, a sign counter that did not advance means
	// the credential may have been copied. Refuse, and do not persist
	// the regressed count. Synced passkeys (iCloud, Google) report a
	// zero counter and never trip this - it only catches a regression
	// on hardware that counts.
	if cred.Authenticator.CloneWarning {
		slog.Warn("auth: passkey clone warning, sign-in refused",
			"user_id", matched.ID, "client_ip", clientip.From(c))
		h.recordLoginFailure(c, matched, matched.Email, "passkey clone warning")

		return response.Unauthorized(c, "passkey sign-in failed")
	}

	if err := h.startSession(c, matched, ""); err != nil {
		return response.Internal(c, err)
	}

	// Best effort from here: the sign-in has already succeeded, and
	// failing it now over bookkeeping would be worse than a stale
	// counter.
	if encoded, eerr := corepasskey.EncodeCredential(cred); eerr == nil {
		if uerr := h.Runtime.Store.Passkey.RecordUse(ctx, row.ID, encoded, time.Now().UTC()); uerr != nil {
			slog.Warn("auth: recording passkey use failed", "passkey_id", row.ID, "err", uerr)
		}
	}

	if err := h.Runtime.Store.User.TouchLastLogin(ctx, matched.Email); err != nil {
		slog.Warn("auth: touch last login failed", "user_id", matched.ID, "err", err)
	}

	slog.Info("auth: passkey sign-in", "user_id", matched.ID, "passkey_id", row.ID)
	h.Runtime.Audit.Security(c, &amodel.Event{
		Type: amodel.TypePasskeyLogin, ActorID: matched.ID, ActorEmail: matched.Email, Status: fiber.StatusOK, Detail: "signed in with passkey " + row.Name,
	})

	return response.Success(c, UserResponse{User: matched})
}

// emailOf is nil-safe, because a failed assertion may never have
// resolved an account at all.
func emailOf(u *usermodel.User) string {
	if u == nil {
		return ""
	}

	return u.Email
}
