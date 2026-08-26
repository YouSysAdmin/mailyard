// Mailyard, Copyright (c) 2021-2026 YouSysAdmin

package server

import (
	"net/url"
	"strings"

	"github.com/gofiber/fiber/v3"

	"github.com/yousysadmin/mailyard/internal/core/env"
	"github.com/yousysadmin/mailyard/internal/core/response"
	authdomain "github.com/yousysadmin/mailyard/internal/domain/auth"
)

// refuseCrossSite refuses a mutating request that rides on the session
// cookie from an origin that is not ours.
//
// SameSite=Strict was the whole CSRF story, and it is a SITE boundary,
// not an ORIGIN one: a page on blog.example.com is same-site to
// mail.example.com, so the browser attaches the cookie to a form it
// posts here. Nothing checked the origin after that, and the decoder
// does not look at Content-Type, so a text/plain form was a valid JSON
// body for POST /api/v1/api-keys - minting a credential - or DELETE
// /api/v1/projects/:id. A compromised sibling host, or a hosting
// arrangement that shares the registrable domain, was enough.
//
// What it checks is the browser's own word. A browser sends Origin on
// every cross-origin POST, form or fetch, and Sec-Fetch-Site on every
// request, and neither can be set by a page. So: an Origin that is not
// ours is refused, and with no Origin at all, a Sec-Fetch-Site that says
// the request came from another site is refused. A client that sends
// neither - curl, an SDK - is not a browser and has no ambient cookie to
// forge with.
//
// Ours is: the origin of server.public_url, whatever host this request
// arrived on (so an installation reachable under a second name keeps
// working), and every cors.allowed_origins entry, since a browser app
// there was let in with credentials on purpose. The request-host form
// uses c.Scheme(), which behind a TLS-terminating proxy is "http" until
// server.trusted_proxies names the proxy - the same setting the Secure
// cookie already needs there, and server.public_url covers it either
// way.
//
// Scoped to the COOKIE: a request carrying an Authorization header is
// let through. A bearer is not ambient, so there is nothing for a
// third-party page to ride on, and a browser app holding a key on
// another origin is exactly the client cors exists for. Reads are let
// through too - a cross-site GET cannot read the answer without CORS.
func refuseCrossSite(rt *env.Runtime) fiber.Handler {
	allowed := map[string]bool{}
	if o := originOf(rt.Config.Server.PublicURL); o != "" {
		allowed[o] = true
	}

	if rt.Config.CORS.Enabled {
		for _, o := range rt.Config.CORS.AllowedOrigins {
			allowed[strings.ToLower(strings.TrimRight(o, "/"))] = true
		}
	}

	return func(c fiber.Ctx) error {
		switch c.Method() {
		case fiber.MethodGet, fiber.MethodHead, fiber.MethodOptions:
			return c.Next()
		}

		if c.Get(fiber.HeaderAuthorization) != "" || c.Cookies(authdomain.SessionCookie) == "" {
			return c.Next()
		}

		origin := strings.ToLower(c.Get(fiber.HeaderOrigin))
		if origin == "" {
			switch c.Get("Sec-Fetch-Site") {
			case "cross-site", "same-site":
				return refusedCrossSite(c)
			}

			return c.Next()
		}

		if allowed[origin] || origin == c.Scheme()+"://"+strings.ToLower(c.Host()) {
			return c.Next()
		}

		return refusedCrossSite(c)
	}
}

func refusedCrossSite(c fiber.Ctx) error {
	return response.Forbidden(c, "cross-site request refused")
}

// originOf reduces a URL to scheme://host[:port], lowercased, or ""
// when it is not an absolute URL.
func originOf(raw string) string {
	u, err := url.Parse(strings.TrimSpace(raw))
	if err != nil || u.Scheme == "" || u.Host == "" {
		return ""
	}

	return strings.ToLower(u.Scheme + "://" + u.Host)
}
