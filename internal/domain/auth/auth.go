// Package auth handles operator-console authentication: local
// (email + password) and OIDC SSO, plus the session cookie that
// gates the CRUD APIs.
package auth

import (
	"github.com/gofiber/fiber/v3"

	"github.com/yousysadmin/mailyard/internal/core/env"
)

// SessionCookie is the name of the cookie carrying the session JWT on
// a plain-HTTP installation. The console reads nothing from it - it is
// HttpOnly - so this name only ever appears server side.
//
// SessionCookieHost is the same cookie over HTTPS. The __Host- prefix
// needs Secure, Path=/ and no Domain, and stops a sibling host from
// setting a same-named cookie over ours. A plain-HTTP instance gets
// the plain name because a browser refuses the prefix there.
const (
	SessionCookie     = "mailyard_session"
	SessionCookieHost = "__Host-mailyard_session"
)

// sessionCookieName is the name a cookie minted now gets.
func sessionCookieName(secure bool) string {
	if secure {
		return SessionCookieHost
	}

	return SessionCookie
}

// SessionCookieValue is the session token the request carries under
// the name this server would mint for it. Over HTTPS that is the
// prefixed name ONLY: a bare one there may have been set by a sibling
// host, which is what the prefix exists to rule out.
func SessionCookieValue(c fiber.Ctx, rt *env.Runtime) string {
	return c.Cookies(sessionCookieName(cookieSecure(c, rt)))
}
