// Mailyard, Copyright (c) 2021-2026 YouSysAdmin

package email

import (
	"github.com/gofiber/fiber/v2"

	"github.com/yousysadmin/mailyard/internal/core/emailverify"
	"github.com/yousysadmin/mailyard/internal/core/response"
	"github.com/yousysadmin/mailyard/internal/core/validation"
	"github.com/yousysadmin/mailyard/internal/domain"
)

// Verify judges whether an address is worth sending to.
//
// The intrinsic checks come from the shared verifier (and its cache).
// The two per-project facts - suppressed, previously bounced - are
// re-read on every call and layered on top, so a cached verdict can
// never claim an address is fine seconds after you suppressed it.
func (h *Handler) Verify(c *fiber.Ctx) error {
	if h.Runtime.Verifier == nil {
		return response.BadRequest(c,
			"email verification is disabled on this install (set email_verify.enabled)")
	}

	rc := domain.GetRequestContext(c)
	in, resp, ok := validation.Bind[verifyInput](c)
	if !ok {
		return resp
	}

	res := h.Runtime.Verifier.Check(c.UserContext(), in.Email, c.QueryBool("fresh", false))

	suppressed, err := h.Runtime.Store.Suppression.IsSuppressed(c.UserContext(), rc.Project.ID, res.Email)
	if err != nil {
		return response.Internal(c, err)
	}

	bounced, err := h.Runtime.Store.Bounce.HasHardBounce(c.UserContext(), rc.Project.ID, res.Email)
	if err != nil {
		return response.Internal(c, err)
	}

	res.Suppressed = suppressed
	res.PreviouslyBounced = bounced

	// Your own history outranks any intrinsic verdict. An address
	// that bounced for you is undeliverable for you, whatever its
	// domain's DNS says.
	if suppressed || bounced {
		res.Status = emailverify.StatusInvalid
		res.Score = 0
		switch {
		case suppressed:
			res.Reason = "the address is on this project's suppression list"
		default:
			res.Reason = "the address has previously hard-bounced for this project"
		}
	}

	return response.Success(c, VerifyResponse{Verification: res})
}
