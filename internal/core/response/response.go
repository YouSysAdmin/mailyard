// Mailyard, Copyright (c) 2021-2026 YouSysAdmin

// Package response wraps Fiber JSON responses behind named helpers so
// handlers don't inline c.JSON(fiber.Map{...}) - keeps status codes
// and the error envelope consistent across the API.
package response

import (
	"fmt"
	"log/slog"

	"github.com/gofiber/fiber/v3"

	"github.com/yousysadmin/mailyard/internal/core/blob"
	"github.com/yousysadmin/mailyard/internal/core/clientip"
	"github.com/yousysadmin/mailyard/internal/database"
)

// Success writes 200 with data as the body.
//
// c.JSON marshals through this package's Marshal - the server wires it
// as the app's JSONEncoder - so an empty list is [] and never null. See
// marshalOptions in wire.go for the whole wire policy.
func Success(c fiber.Ctx, data any) error {
	return c.Status(fiber.StatusOK).JSON(data)
}

// Created writes 201 with data as the body.
func Created(c fiber.Ctx, data any) error {
	return c.Status(fiber.StatusCreated).JSON(data)
}

// NoContent writes 204 and no body.
func NoContent(c fiber.Ctx) error {
	return c.SendStatus(fiber.StatusNoContent)
}

// BadRequest writes 400 with msg. For a field-level refusal use the
// validation package, which lists which fields were refused.
func BadRequest(c fiber.Ctx, msg string) error {
	return c.Status(fiber.StatusBadRequest).JSON(fiber.Map{"error": msg})
}

// BadRequestFields is the structured-error variant of BadRequest.
// Both the legacy "error" string (so existing api.js callers
// continue to surface a single-line message) and the new "fields"
// array land in the body, so the SPA can incrementally adopt
// field-level rendering without a synchronized backend cut-over.
//
// The summary string and the fields list are caller-supplied --
// validation.Humanize + validation.Summary are the canonical
// producers, but any handler that wants to surface multiple field
// errors (e.g. cross-field business rules) can build them directly.
func BadRequestFields(c fiber.Ctx, summary string, fields any) error {
	return c.Status(fiber.StatusBadRequest).JSON(fiber.Map{
		"error":  summary,
		"fields": fields,
	})
}

// TooManyRequests is the quota rejection: the caller can retry after
// the window rolls or the operator raises the plan.
func TooManyRequests(c fiber.Ctx, msg string) error {
	return c.Status(fiber.StatusTooManyRequests).JSON(fiber.Map{"error": msg})
}

// Unauthorized writes 401 with msg, meaning the caller is not
// identified.
func Unauthorized(c fiber.Ctx, msg string) error {
	return c.Status(fiber.StatusUnauthorized).JSON(fiber.Map{"error": msg})
}

// Forbidden writes 403 with msg, meaning the caller is identified and
// still may not.
func Forbidden(c fiber.Ctx, msg string) error {
	return c.Status(fiber.StatusForbidden).JSON(fiber.Map{"error": msg})
}

// NotFound writes 404 with msg. Cross-project access answers this too,
// so a caller cannot tell a foreign row from a missing one.
func NotFound(c fiber.Ctx, msg string) error {
	return c.Status(fiber.StatusNotFound).JSON(fiber.Map{"error": msg})
}

// Conflict is for state-conflict failures the operator can fix
// themselves (FK constraint violations on delete, unique-key
// collisions, etc.) - distinct from BadRequest (which connotes
// "your input is wrong") and Internal (which connotes "server bug").
func Conflict(c fiber.Ctx, msg string) error {
	return c.Status(fiber.StatusConflict).JSON(fiber.Map{"error": msg})
}

// Gone is for single-use resources that have already been consumed
// (or never existed) - the caller has no viable retry, unlike
// Conflict where the operator can resolve the state and try again.
func Gone(c fiber.Ctx, msg string) error {
	return c.Status(fiber.StatusGone).JSON(fiber.Map{"error": msg})
}

// Unavailable is a refusal that is TEMPORARY by nature - the caller
// should come back, and nothing about the request needs changing.
// Distinct from Conflict, where somebody has to fix state, and from
// Forbidden, which a machine client is right to treat as permanent.
func Unavailable(c fiber.Ctx, msg string) error {
	return c.Status(fiber.StatusServiceUnavailable).JSON(fiber.Map{"error": msg})
}

// Coded answers with an explicit status and message, for the ONE caller
// that cannot name the status at compile time: the app-wide error handler,
// which repeats the status Fiber chose for an error it raised itself.
//
// Every other refusal has a helper of its own above and should use it - a
// status passed in as a number is a status nobody can grep for.
func Coded(c fiber.Ctx, code int, msg string) error {
	return c.Status(code).JSON(fiber.Map{"error": msg})
}

// Internal logs the underlying error server-side and returns a generic
// 500 to the caller. The raw err is intentionally not echoed back - it
// commonly contains stack-revealing detail (file paths, SQL state,
// AWS request IDs) that's useful to attackers but not to operators
// using the UI. Operators see the full error in the access log - the
// caller sees a uniform message they can correlate by the request_id
// on the matching access-log line, or by timestamp.
//
// Pass nil err when the caller has already done its own logging or
// when invoked from the panic-recovery path.
func Internal(c fiber.Ctx, err error) error {
	// An id that is not a uuid names nothing, and that is a 404.
	//
	// This is the one place every store error becomes a response, which
	// is why the translation lives here rather than in 117 handlers or
	// as a constraint on 134 routes. It can only ever soften a 500 that
	// was going to be sent anyway, so it cannot refuse a request that
	// works today.
	//
	// Still logged, at warn: passing the wrong field as an id is a bug
	// in a caller or in us, and answering the caller honestly must not
	// mean nobody can see it happening.
	if err != nil && database.MalformedID(err) {
		slog.Warn("request carried an id that is not a uuid",
			"err", err,
			"path", c.Path(),
			"method", c.Method(),
			"client_ip", clientip.From(c),
		)

		return NotFound(c, "not found")
	}

	if err != nil {
		slog.Error("handler internal error",
			"err", err,
			"path", c.Path(),
			"method", c.Method(),
			"client_ip", clientip.From(c),
		)
	}

	return c.Status(fiber.StatusInternalServerError).JSON(fiber.Map{"error": "internal server error"})
}

// Attachment streams a downloaded file.
//
// Always Content-Disposition: attachment, never inline. Attachment
// bytes and their content type are supplied by whoever sent the mail,
// so an HTML attachment rendered inline would execute on this origin
// - and the console's CSP allows 'unsafe-inline' scripts, so it
// would execute with the session cookie in reach. The disposition
// header, together with the global X-Content-Type-Options: nosniff,
// is what keeps a download a download.
//
// Callers pass the raw filename: sanitizing it is this function's
// job, not something three endpoints should each remember.
func Attachment(c fiber.Ctx, filename, contentType string, raw []byte) error {
	if contentType == "" {
		contentType = fiber.MIMEOctetStream
	}

	c.Set(fiber.HeaderContentType, contentType)
	c.Set(fiber.HeaderContentDisposition,
		fmt.Sprintf("attachment; filename=%q", blob.SanitizeFilename(filename)))

	return c.Send(raw)
}
