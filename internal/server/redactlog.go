// Mailyard, Copyright (c) 2021-2026 YouSysAdmin

package server

import (
	"log/slog"
	"net/http"
	"strings"
	"sync"
	"time"

	"github.com/gofiber/fiber/v3"

	"github.com/yousysadmin/mailyard/internal/core/clientip"
	"github.com/yousysadmin/mailyard/internal/core/env"
)

// sensitivePathPrefixes name the URL subtrees where the URL ITSELF is a
// credential, so the access logger must not record it.
//
// The tokens these paths carry were each hashed or sealed so a reader
// of the DATABASE cannot use them - and the access logger was quietly
// undoing that care: slog-fiber records the raw path, the query string
// AND every route parameter, so a password-reset token, an OAuth
// authorization code, an invitation, an unsubscribe capability and the
// 90-day web-view token all landed in a log that gets shipped to
// wherever logs go, which is always somewhere with more readers than
// the database has.
//
// Matched on normalized SEGMENT boundaries - /tracking covers
// /TRACKING/view/x and /tracking/, never /trackingfoo.
func sensitivePathPrefixes() []string {
	return []string{
		// Open and click carry ?sig=, unsubscribe and the hosted
		// web-view carry the capability in the PATH. The web-view
		// token is the strongest of these: full message content,
		// valid for 90 days.
		"/tracking",
		// The callback query is ?code=...&state=... - RFC 9700 is
		// explicit that authorization codes must stay out of logs.
		env.ConsolePath + "/api/auth/oauth",
		// Invitation tokens: the accept route carries one in the
		// path, and the console page takes one in the query. Accept
		// additionally binds to the invited address, but the token
		// still does not belong in a log.
		"/api/v1/invitations",
		env.ConsolePath + "/invitations",
		// SPA document requests whose query is a single-use account
		// credential. The password-reset token is account takeover
		// for its whole TTL.
		env.ConsolePath + "/reset-password",
		env.ConsolePath + "/verify-email",
	}
}

// redactURLs wraps the access logger: a request whose URL carries a
// capability is logged HERE, by route pattern, and never reaches the
// wrapped logger. Everything else passes through untouched.
//
// Replaced rather than filtered, because a slog-fiber Filter can only
// keep or drop an entry, and dropping is wrong twice over: the public
// tracking surface is exactly where an operator investigating abuse
// needs the log, and an attacker should not be able to choose an
// unlogged path. What the operator reads - method, route, status,
// latency, caller - is all here. What is not here is the one thing
// they cannot use and someone else could.
func redactURLs(log *slog.Logger, inner fiber.Handler) fiber.Handler {
	prefixes := sensitivePathPrefixes()
	for i, p := range prefixes {
		prefixes[i] = normalizePath(p)
	}

	var (
		once       sync.Once
		errHandler fiber.ErrorHandler
	)

	return func(c fiber.Ctx) error {
		path := normalizePath(c.Path())
		redact := false
		for _, p := range prefixes {
			if path == p || strings.HasPrefix(path, p+"/") {
				redact = true

				break
			}
		}

		if !redact {
			return inner(c)
		}

		// The error is answered here, the way the wrapped logger
		// answers it, so the status this entry records is the status
		// that went out rather than a 200 the error handler is about
		// to replace.
		once.Do(func() { errHandler = c.App().ErrorHandler })
		start := time.Now()
		err := c.Next()
		if err != nil {
			if err = errHandler(c, err); err != nil {
				_ = c.SendStatus(fiber.StatusInternalServerError)
			}
		}

		status := c.Response().StatusCode()
		attrs := []any{
			slog.String("method", c.Method()),
			// The PATTERN, not the path: /tracking/view/:token says
			// which surface was hit and carries nothing the caller
			// presented.
			slog.String("route", c.Route().Path),
			slog.Int("status", status),
			slog.Duration("latency", time.Since(start)),
			slog.String("ip", clientip.From(c)),
		}

		switch {
		case status >= http.StatusInternalServerError:
			log.Error("Incoming request", attrs...)
		case status >= http.StatusBadRequest:
			log.Warn("Incoming request", attrs...)
		default:
			log.Info("Incoming request", attrs...)
		}

		return err
	}
}
