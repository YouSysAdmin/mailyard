// Mailyard, Copyright (c) 2021-2026 YouSysAdmin

package server

import (
	"sync"
	"time"

	"github.com/gofiber/fiber/v3"

	"github.com/yousysadmin/mailyard/internal/core/clientip"
	"github.com/yousysadmin/mailyard/internal/core/response"
)

// uncredentialedReadTimeout is how long a request naming no credential
// may take to arrive, body included. Its body is capped at
// apiBodyLimit, so this is minutes of margin on any real link, and it
// is what stops a stranger holding a connection open by trickling a
// body for the full server ReadTimeout.
const uncredentialedReadTimeout = 30 * time.Second

// admission bounds the requests being handled at once, overall and per
// caller, and refuses past either bound with the JSON envelope rather
// than the library's plain-text 503.
//
// The probes are exempt, so a node at its ceiling still answers its
// orchestrator, and so are the streams, which hold a request for their
// whole life and are bounded by the event bus instead.
type admission struct {
	limit int
	perIP int

	mu       sync.Mutex
	inFlight int
	byIP     map[string]int
}

func newAdmission(limit, perIP int) *admission {
	return &admission{limit: limit, perIP: perIP, byIP: map[string]int{}}
}

// exemptFromAdmission are the paths admission never refuses.
func exemptFromAdmission(path string) bool {
	switch normalizePath(path) {
	case "/healthz", "/readyz":
		return true
	}

	return isStreamingPath(path)
}

func (a *admission) handler(c fiber.Ctx) error {
	if (a.limit <= 0 && a.perIP <= 0) || exemptFromAdmission(c.Path()) {
		return c.Next()
	}

	ip := clientip.From(c)
	ok, overall := a.enter(ip)
	if !ok {
		c.Set(fiber.HeaderRetryAfter, "1")
		if overall {
			return response.Unavailable(c, "the server is at its limit of requests in progress, retry shortly")
		}

		return response.TooManyRequests(c, "too many requests in progress from this address, retry shortly")
	}

	defer a.leave(ip)

	return c.Next()
}

// enter takes a slot, reporting false and which bound refused when
// there is none.
func (a *admission) enter(ip string) (ok, overall bool) {
	a.mu.Lock()
	defer a.mu.Unlock()

	if a.limit > 0 && a.inFlight >= a.limit {
		return false, true
	}

	if a.perIP > 0 && a.byIP[ip] >= a.perIP {
		return false, false
	}

	a.inFlight++
	a.byIP[ip]++

	return true, false
}

func (a *admission) leave(ip string) {
	a.mu.Lock()
	defer a.mu.Unlock()

	a.inFlight--
	if a.byIP[ip]--; a.byIP[ip] <= 0 {
		delete(a.byIP, ip)
	}
}

// connectionCeiling is what fasthttp is told, which counts CONNECTIONS
// and answers past it in plain text before any handler runs. It sits
// above the request limit so that admission, with its envelope and its
// exempt probes, is the bound a caller meets first. 0 is the library
// default.
func connectionCeiling(limit int) int {
	if limit <= 0 {
		return 0
	}

	return limit + max(limit/4, 64)
}
