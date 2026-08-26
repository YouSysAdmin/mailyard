// Mailyard, Copyright (c) 2021-2026 YouSysAdmin

package auth

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"log/slog"
	"strconv"
	"strings"

	"github.com/gofiber/fiber/v3"

	"github.com/yousysadmin/mailyard/internal/core/authenticator"
	"github.com/yousysadmin/mailyard/internal/core/response"
	"github.com/yousysadmin/mailyard/internal/core/validation"
	"github.com/yousysadmin/mailyard/internal/domain"
	amodel "github.com/yousysadmin/mailyard/internal/models/audit"
	usermodel "github.com/yousysadmin/mailyard/internal/models/user"
)

// recoveryCodeCount is how many codes a set holds. Ten is what every
// authenticator-backed service hands out, and enough that a person who
// spends one a year never runs dry before they replace the phone.
const recoveryCodeCount = 10

// recoveryAlphabet is what a code is spelled in: lowercase without the
// letters that read as digits and the digits that read as letters, so a
// code copied by hand from a printout is typed as it was written. 31
// symbols to the power of 16 is about 2^79.
const recoveryAlphabet = "abcdefghjkmnpqrstuvwxyz23456789"

// recoveryCodeLen is the length of a code without its dashes.
//
// Sixteen, from ten. A recovery code is a whole second factor on its
// own and is stored under a single fast hash (see hashRecoveryCode), so
// its strength IS its length: ten symbols was 49 bits nominal - "eight
// hundred quadrillion" in the old comment was off by a thousand, it is
// eight hundred TRILLION - and 48 effective after the modulo bias
// below, which a stolen table meets with hours of GPU time. Sixteen is
// 79 bits, out of reach of any table attack, and still a line on a
// printout.
const recoveryCodeLen = 16

// legacyRecoveryCodeLen is the length codes were minted at before, and
// looksLikeRecoveryCode still admits it: a set printed under the old
// length keeps working until it is spent or regenerated, since the
// hash does not know how long its input was.
const legacyRecoveryCodeLen = 10

// recoveryGroup is how many symbols sit between dashes.
const recoveryGroup = 4

// newRecoveryCodes mints a set and returns the codes as the person sees
// them (xxxx-xxxx-xxxx-xxxx) beside the hashes that are stored.
func newRecoveryCodes() (codes, hashes []string, err error) {
	codes = make([]string, 0, recoveryCodeCount)
	hashes = make([]string, 0, recoveryCodeCount)
	for range recoveryCodeCount {
		var b strings.Builder
		n := 0
		for n < recoveryCodeLen {
			// One byte at a time through the sampler. Rejection rather
			// than modulo: 256 is not a multiple of 31, so `r % 31`
			// gave the first eight letters nine chances in 256 each and
			// the other twenty-three eight - a bias, small, that a
			// guesser gets for free.
			var r [1]byte
			if _, err := rand.Read(r[:]); err != nil {
				return nil, nil, err
			}

			sym, ok := recoverySymbol(r[0])
			if !ok {
				continue
			}

			if n > 0 && n%recoveryGroup == 0 {
				b.WriteByte('-')
			}

			b.WriteByte(sym)
			n++
		}

		codes = append(codes, b.String())
		hashes = append(hashes, hashRecoveryCode(b.String()))
	}

	return codes, hashes, nil
}

// recoverySymbol maps one random byte onto the alphabet without bias,
// rejecting the tail that would wrap unevenly. 248 is the largest
// multiple of 31 that fits a byte, so every accepted value has exactly
// eight preimages.
func recoverySymbol(r byte) (byte, bool) {
	const fair = 256 - 256%len(recoveryAlphabet)
	if int(r) >= fair {
		return 0, false
	}

	return recoveryAlphabet[int(r)%len(recoveryAlphabet)], true
}

// normalizeRecoveryCode folds what a person typed - case, the dash, a
// stray space - to the ten symbols that identify the code.
func normalizeRecoveryCode(in string) string {
	in = strings.ToLower(strings.TrimSpace(in))

	return strings.NewReplacer("-", "", " ", "").Replace(in)
}

// looksLikeRecoveryCode tells a recovery code from an authenticator
// code at sign-in, where both arrive in one field: sixteen symbols from
// the alphabet, or ten for a set minted before the length grew, where a
// TOTP code is six digits.
func looksLikeRecoveryCode(in string) bool {
	n := normalizeRecoveryCode(in)
	if len(n) != recoveryCodeLen && len(n) != legacyRecoveryCodeLen {
		return false
	}

	for i := range len(n) {
		if !strings.ContainsRune(recoveryAlphabet, rune(n[i])) {
			return false
		}
	}

	return true
}

// hashRecoveryCode is the stored form. SHA-256 and not bcrypt, for the
// reason API keys and reset tokens are: the input is random and long
// enough - 79 bits at recoveryCodeLen - that a slow hash buys nothing
// against a stolen table. At the old ten symbols that was not true,
// which is why the length grew rather than the hash slowing down: ten
// bcrypt verifications per sign-in is a cost, sixteen symbols is not.
func hashRecoveryCode(code string) string {
	sum := sha256.Sum256([]byte(normalizeRecoveryCode(code)))

	return hex.EncodeToString(sum[:])
}

// issueRecoveryCodes mints and stores a fresh set for u, returning the
// codes to show once.
func (h *Handler) issueRecoveryCodes(ctx context.Context, userID string) ([]string, error) {
	codes, hashes, err := newRecoveryCodes()
	if err != nil {
		return nil, err
	}

	if err := h.Runtime.Store.User.ReplaceRecoveryCodes(ctx, userID, hashes); err != nil {
		return nil, err
	}

	return codes, nil
}

// consumeRecoveryCode spends a recovery code at sign-in. The lockout
// and its counter are shared with the authenticator code - see
// consumeSecondFactor - so this only answers whether a code matched.
func (h *Handler) consumeRecoveryCode(ctx context.Context, userID, code string) bool {
	claimed, err := h.Runtime.Store.User.ClaimRecoveryCode(ctx, userID, hashRecoveryCode(code))
	if err != nil {
		slog.Error("auth: recovery code claim failed", "user_id", userID, "err", err)

		return false
	}

	return claimed
}

// consumeSecondFactor is the sign-in check: an authenticator code, or
// failing that a recovery code, under ONE lockout. A recovery code
// spent is recorded as a security event and mailed, since the owner
// either lost their phone or somebody holds a copy of the codes.
func (h *Handler) consumeSecondFactor(c fiber.Ctx, u *usermodel.User, code string) bool {
	ctx := c.Context()
	if h.factorLocked(ctx, u.ID) {
		return false
	}

	if step, ok := h.verifyTOTP(u.TOTPSecret, code); ok {
		return h.claimStep(ctx, u.ID, step)
	}

	if looksLikeRecoveryCode(code) && h.consumeRecoveryCode(ctx, u.ID, code) {
		h.clearFactorFailures(ctx, u.ID)
		remaining, err := h.Runtime.Store.User.RemainingRecoveryCodes(ctx, u.ID)
		if err != nil {
			slog.Error("auth: recovery code count failed", "user_id", u.ID, "err", err)
		}

		slog.Warn("auth: recovery code used at sign-in", "user_id", u.ID, "remaining", remaining)
		h.Runtime.Audit.Security(c, &amodel.Event{
			Type: amodel.TypeTOTPRecoveryUsed, ActorID: u.ID, ActorEmail: u.Email, Status: fiber.StatusOK,
			Detail: "a recovery code signed in, " + strconv.Itoa(remaining) + " left",
		})

		return true
	}

	h.recordFactorFailure(ctx, u.ID)

	return false
}

// RecoveryCodesStatus serves GET /app/api/auth/2fa/recovery-codes: how
// many unspent codes the account holds.
func (h *Handler) RecoveryCodesStatus(c fiber.Ctx) error {
	u, resp, ok := h.totpSelf(c)
	if !ok {
		return resp
	}

	remaining, err := h.Runtime.Store.User.RemainingRecoveryCodes(c.Context(), u.ID)
	if err != nil {
		return response.Internal(c, err)
	}

	return response.Success(c, RecoveryCodesStatusResponse{Remaining: remaining})
}

// RecoveryCodesRegenerate serves POST /app/api/auth/2fa/recovery-codes:
// a fresh set, voiding the old one. Gated on the PASSWORD, not a code:
// the person asking is usually the one whose authenticator is gone, and
// a hijacked session must not be able to mint itself a way back in
// later. Shown once.
func (h *Handler) RecoveryCodesRegenerate(c fiber.Ctx) error {
	u, resp, ok := h.totpSelf(c)
	if !ok {
		return resp
	}

	in, resp, ok := validation.Bind[passkeyReauthInput](c)
	if !ok {
		return resp
	}

	if !authenticator.VerifyPassword(u.PasswordHash, in.Password) {
		return response.Unauthorized(c, "password is incorrect")
	}

	if !u.TOTPEnabled {
		return response.BadRequest(c, "two-factor auth is not enabled")
	}

	codes, err := h.issueRecoveryCodes(c.Context(), u.ID)
	if err != nil {
		return response.Internal(c, err)
	}

	h.Runtime.Audit.Security(c, &amodel.Event{
		Type: amodel.TypeTOTPRecoveryRegenerated, ActorID: u.ID, ActorEmail: u.Email, Status: fiber.StatusOK,
	})

	return response.Success(c, RecoveryCodesResponse{Codes: codes})
}

// totpSelf resolves the signed-in local account, the way passkeySelf
// does for that surface. (resp, ok), never a lone error.
func (h *Handler) totpSelf(c fiber.Ctx) (*usermodel.User, error, bool) {
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

	if !u.ManagesOwnCredentials() {
		return nil, response.Forbidden(c,
			"this account signs in through an identity provider, manage two-factor auth there"), false
	}

	return u, nil, true
}
