// Mailyard, Copyright (c) 2021-2026 YouSysAdmin

package auth

import (
	"context"
	"log/slog"
	"time"

	"github.com/gofiber/fiber/v3"
	"github.com/pquerna/otp"
	"github.com/pquerna/otp/totp"

	"github.com/yousysadmin/mailyard/internal/core/response"
	"github.com/yousysadmin/mailyard/internal/core/validation"
	"github.com/yousysadmin/mailyard/internal/domain"
	amodel "github.com/yousysadmin/mailyard/internal/models/audit"
)

// TOTPSetup mints a fresh secret and stores it (encrypted, not yet
// enabled). The response carries the otpauth URL for the
// authenticator app. Re-running setup replaces a pending secret but
// refuses when 2FA is already enabled - disable first.
//
// A live session is the whole gate, and no password re-auth, which is
// deliberate rather than an omission - it reads like one beside
// PasskeyRegisterBegin, which does demand the password.
//
// The reason that rule gives is the reason it does not extend here:
// enrolling or removing a passkey changes HOW THE ACCOUNT CAN BE
// ENTERED, so a hijacked session must not be enough. Enrolling a second
// factor adds a REQUIREMENT to entering it. The worst a hijacked session
// achieves is locking the owner out, which is recoverable - ResetTOTP
// exists for exactly that and refuses to run on the caller's own
// account, so a stolen admin session cannot use it to strip its own
// second factor.
//
// Disabling is gated by a CODE, which proves possession, and that is the
// half where proof matters.
func (h *Handler) TOTPSetup(c fiber.Ctx) error {
	rc := domain.GetRequestContext(c)
	if rc == nil || rc.User == nil {
		return response.Unauthorized(c, "not authenticated")
	}

	u, err := h.Runtime.Store.User.GetByID(c.Context(), rc.User.ID)
	if err != nil {
		return response.Internal(c, err)
	}

	// A live session whose account is gone is not an internal error, and
	// response.Internal(c, nil) logs nothing - so the deleted-mid-session
	// case answered 500 with no record of why. passkeySelf already
	// splits these two apart.
	if u == nil {
		return response.Unauthorized(c, "not authenticated")
	}

	// Same rule as passkeys and the password: an identity provider
	// owns this account's sign-in, and a second factor held here would
	// be one the IdP knows nothing about.
	if !u.ManagesOwnCredentials() {
		return response.Forbidden(c,
			"this account signs in through an identity provider, manage two-factor auth there")
	}

	if u.TOTPEnabled {
		return response.Conflict(c, "two-factor auth is already enabled, disable it first")
	}

	key, err := totp.Generate(totp.GenerateOpts{
		Issuer:      "Mailyard",
		AccountName: u.Email,
	})
	if err != nil {
		return response.Internal(c, err)
	}

	u.TOTPSecret, err = h.Runtime.Crypto.Encrypt(key.Secret())
	if err != nil {
		return response.Internal(c, err)
	}

	// SetTOTP, not Put: Put writes the whole row from a read taken before
	// this request, so enrolling a second factor also rewrote `admin`,
	// `disabled` and `email_verified` as they stood then.
	if err := h.Runtime.Store.User.SetTOTP(c.Context(), u.ID, u.TOTPSecret, u.TOTPEnabled); err != nil {
		return response.Internal(c, err)
	}

	return response.Success(c, TOTPSetupResponse{
		Secret:     key.Secret(),
		OTPAuthURL: key.URL(),
	})
}

// TOTPEnable turns 2FA on after the user proves the authenticator
// works with a valid code for the pending secret.
func (h *Handler) TOTPEnable(c fiber.Ctx) error {
	rc := domain.GetRequestContext(c)
	if rc == nil || rc.User == nil {
		return response.Unauthorized(c, "not authenticated")
	}

	in, resp, ok := validation.Bind[totpCodeInput](c)
	if !ok {
		return resp
	}

	u, err := h.Runtime.Store.User.GetByID(c.Context(), rc.User.ID)
	if err != nil {
		return response.Internal(c, err)
	}

	// A live session whose account is gone is not an internal error, and
	// response.Internal(c, nil) logs nothing - so the deleted-mid-session
	// case answered 500 with no record of why. passkeySelf already
	// splits these two apart.
	if u == nil {
		return response.Unauthorized(c, "not authenticated")
	}

	if u.TOTPSecret == "" {
		return response.BadRequest(c, "run 2fa setup first")
	}

	if !h.consumeTOTP(c.Context(), u.ID, u.TOTPSecret, in.Code) {
		return response.BadRequest(c, "invalid code")
	}

	u.TOTPEnabled = true
	if err := h.Runtime.Store.User.SetTOTP(c.Context(), u.ID, u.TOTPSecret, true); err != nil {
		return response.Internal(c, err)
	}

	slog.Info("auth: totp enabled", "user_id", u.ID)
	// Recorded here, explicitly, like every other security event: the
	// middleware only sees the request, and what matters is that this
	// account's second factor changed.
	//
	// It was missing. Both constants existed in internal/models/audit and
	// nothing wrote either one, so turning 2FA off left no trace in the
	// security log at all - found by a guard that asks whether every
	// mailed event is an event something produces.
	h.Runtime.Audit.Security(c, &amodel.Event{
		Type: amodel.TypeTOTPEnabled, ActorID: u.ID, ActorEmail: u.Email, Status: fiber.StatusOK,
	})

	return response.Success(c, TOTPStateResponse{TOTPEnabled: true})
}

// TOTPDisable turns 2FA off, gated on a currently valid code so a
// hijacked session cannot silently weaken the account.
func (h *Handler) TOTPDisable(c fiber.Ctx) error {
	rc := domain.GetRequestContext(c)
	if rc == nil || rc.User == nil {
		return response.Unauthorized(c, "not authenticated")
	}

	in, resp, ok := validation.Bind[totpCodeInput](c)
	if !ok {
		return resp
	}

	u, err := h.Runtime.Store.User.GetByID(c.Context(), rc.User.ID)
	if err != nil {
		return response.Internal(c, err)
	}

	// A live session whose account is gone is not an internal error, and
	// response.Internal(c, nil) logs nothing - so the deleted-mid-session
	// case answered 500 with no record of why. passkeySelf already
	// splits these two apart.
	if u == nil {
		return response.Unauthorized(c, "not authenticated")
	}

	if !u.TOTPEnabled {
		return response.BadRequest(c, "two-factor auth is not enabled")
	}

	if !h.consumeTOTP(c.Context(), u.ID, u.TOTPSecret, in.Code) {
		return response.BadRequest(c, "invalid code")
	}

	u.TOTPEnabled = false
	u.TOTPSecret = ""
	if err := h.Runtime.Store.User.SetTOTP(c.Context(), u.ID, "", false); err != nil {
		return response.Internal(c, err)
	}

	slog.Info("auth: totp disabled", "user_id", u.ID)

	// The one of the pair that matters most: an account just got weaker,
	// and the owner is the person who needs to know it was them.
	h.Runtime.Audit.Security(c, &amodel.Event{
		Type: amodel.TypeTOTPDisabled, ActorID: u.ID, ActorEmail: u.Email, Status: fiber.StatusOK,
	})

	return response.Success(c, TOTPStateResponse{TOTPEnabled: false})
}

// totpPeriod is the TOTP step length in seconds and totpSkew the
// number of steps accepted either side of now, to tolerate clock
// drift between the server and the authenticator app. Both match
// pquerna/otp's defaults, which is what totp.Generate mints against.
const (
	totpPeriod = 30
	totpSkew   = 1
)

// verifyTOTP decrypts the stored secret and validates the code,
// returning the time-step the code belongs to.
//
// The step matters because a code must be usable exactly once. With
// skew 1 there are three live codes at any instant, so a code stays
// valid for up to 90 seconds - long enough for anyone who observes
// it (a relaying phishing page, a shoulder surfer, a form post in a
// log) to present it again. Callers pair this with
// UserStore.ClaimTOTPStep to burn the step.
//
// pquerna/otp only answers yes or no, so the step is found by asking
// about each candidate individually with the skew turned off.
func (h *Handler) verifyTOTP(encSecret, code string) (step uint64, ok bool) {
	secret, err := h.Runtime.Crypto.Decrypt(encSecret)
	if err != nil {
		return 0, false
	}

	now := time.Now()
	opts := totp.ValidateOpts{
		Period:    totpPeriod,
		Skew:      0,
		Digits:    otp.DigitsSix,
		Algorithm: otp.AlgorithmSHA1,
	}
	for delta := -totpSkew; delta <= totpSkew; delta++ {
		at := now.Add(time.Duration(delta*totpPeriod) * time.Second)
		valid, verr := totp.ValidateCustom(code, secret, at, opts)
		if verr == nil && valid {
			sec := at.Unix()
			if sec < 0 {
				// A clock set before 1970 would wrap the conversion into a
				// colossal step number, claim it, and then refuse every
				// real code forever. Refuse the code instead.
				return 0, false
			}

			return uint64(sec) / totpPeriod, true
		}
	}

	return 0, false
}

// consumeTOTP validates a code and burns its step so the same code
// cannot be presented twice. Every 2FA check goes through here rather
// than verifyTOTP directly - a validated-but-unclaimed code is
// exactly the replay this exists to stop.
func (h *Handler) consumeTOTP(ctx context.Context, userID, encSecret, code string) bool {
	// Locked is refused BEFORE the code is looked at, so a right guess
	// during the lockout learns nothing. Same answer as a wrong code:
	// the caller already holds the password, and telling them the
	// factor is locked tells them their guesses are being counted.
	until, err := h.Runtime.Store.User.TOTPLockedUntil(ctx, userID)
	if err != nil {
		slog.Error("auth: totp lock read failed", "user_id", userID, "err", err)

		return false
	}

	if until != nil && time.Now().Before(*until) {
		slog.Warn("auth: totp refused, factor is locked", "user_id", userID, "until", until)

		return false
	}

	step, ok := h.verifyTOTP(encSecret, code)
	if !ok {
		locked, rerr := h.Runtime.Store.User.RecordTOTPFailure(ctx, userID, totpMaxFailures, totpLockout)
		if rerr != nil {
			slog.Error("auth: totp failure count failed", "user_id", userID, "err", rerr)
		}

		if locked {
			slog.Warn("auth: totp locked after repeated wrong codes",
				"user_id", userID, "failures", totpMaxFailures, "for", totpLockout)
		}

		return false
	}

	claimed, err := h.Runtime.Store.User.ClaimTOTPStep(ctx, userID, step)
	if err != nil {
		slog.Error("auth: totp step claim failed", "user_id", userID, "err", err)

		return false
	}

	if !claimed {
		slog.Warn("auth: totp code replayed", "user_id", userID, "step", step)

		return false
	}

	if err := h.Runtime.Store.User.ClearTOTPFailures(ctx, userID); err != nil {
		slog.Error("auth: totp failure reset failed", "user_id", userID, "err", err)
	}

	return true
}

// totpMaxFailures wrong codes in a row lock the factor for totpLockout.
// Five is more than a person mistypes and a thousandth of what a guess
// at six digits needs. Fifteen minutes turns the remaining guesses into
// years.
const (
	totpMaxFailures = 5
	totpLockout     = 15 * time.Minute
)
