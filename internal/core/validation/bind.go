// Mailyard, Copyright (c) 2021-2026 YouSysAdmin

package validation

import (
	"github.com/gofiber/fiber/v3"

	"github.com/yousysadmin/mailyard/internal/core/response"
)

// Bind parses and validates the request body, writing the 400 itself
// when the body does not hold up.
//
// It replaced five lines at the top of sixty-five handlers, and the
// duplication that mattered was not the line count: every one of them
// knew how a validation error becomes a response - Humanize, then
// Summary, then which helper - so changing the error envelope meant
// sixty-five edits and one of them missed.
//
// Three return values, like the auth middleware: the response helpers
// write the status and return nil, so an error alone cannot tell
// "rejected" from "fine". Return resp the moment ok is false:
//
//	in, resp, ok := validation.Bind[createInput](c)
//	if !ok {
//	    return resp
//	}
func Bind[T any](c fiber.Ctx) (out T, resp error, ok bool) {
	in, err := BindAndValidate[T](c)
	if err != nil {
		fes := Humanize(err)

		return out, response.BadRequestFields(c, Summary(fes), fes), false
	}

	return in, nil, true
}
