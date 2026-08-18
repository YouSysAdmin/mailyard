// Mailyard, Copyright (c) 2021-2026 YouSysAdmin

// Package health serves the two probe endpoints.
//
// The split is the usual one and it matters for orchestrators:
// liveness answers "is this process wedged, restart it?", readiness
// answers "can this instance serve traffic right now?". Conflating
// them means a database blip restarts every pod instead of just
// taking them out of rotation until the database returns.
package health

import (
	"context"
	"log/slog"
	"time"

	"github.com/gofiber/fiber/v2"

	"github.com/yousysadmin/mailyard/internal/core/env"
	"github.com/yousysadmin/mailyard/internal/core/response"
)

// Handler answers the two probes an orchestrator uses, /readyz and
// (behind a session) /app/api/health.
//
// Liveness and readiness are separate on purpose: Status checks
// nothing while Ready checks the database, so a database outage drains
// the instance instead of restarting it.
type Handler struct {
	Runtime *env.Runtime
}

// Status is the liveness probe. Deliberately checks nothing: if the
// HTTP server can answer at all, the process is alive. Adding a
// dependency check here would turn a database outage into a restart
// loop, which is the one thing that cannot help.
func (h *Handler) Status(c *fiber.Ctx) error {
	return response.Success(c, StatusResponse{Status: "ok"})
}

// readyTimeout bounds the dependency check so a hung database cannot
// hold the probe open until the orchestrator's own timeout fires.
const readyTimeout = 2 * time.Second

// Ready is the readiness probe. Open, like liveness: a probe target
// that needs a credential is not a probe target.
//
// It reports 503 with the failing check named when a dependency is
// unreachable, so the instance leaves the load balancer's rotation
// without being killed.
func (h *Handler) Ready(c *fiber.Ctx) error {
	ctx, cancel := contextWithTimeout(c, readyTimeout)
	defer cancel()

	checks := map[string]string{}
	ready := true

	// The database is the only hard dependency. Everything else in
	// this build (the queue, the scheduler, mail) is in-process and
	// cannot be unreachable while the process answers at all.
	if h.Runtime.DB == nil {
		checks["database"] = "not configured"
		ready = false
	} else if err := h.Runtime.DB.DB().PingContext(ctx); err != nil {
		// The probe is open, so the REASON goes to the log and not to
		// the response. A pgx connection error carries the DSN's host,
		// port, user and database name, and this endpoint answers
		// anybody - so an outage was handing internal topology and the
		// database username to whoever asked.
		slog.Error("health: database unreachable", "error", err)
		checks["database"] = "unreachable"
		ready = false
	} else {
		checks["database"] = "ok"
	}

	status := "ok"
	code := fiber.StatusOK
	if !ready {
		status = "unavailable"
		code = fiber.StatusServiceUnavailable
	}

	return c.Status(code).JSON(ReadyResponse{
		Status: status,
		Checks: checks,
	})
}

// contextWithTimeout derives from the request context so a client
// disconnect cancels the ping too.
func contextWithTimeout(c *fiber.Ctx, d time.Duration) (context.Context, context.CancelFunc) {
	return context.WithTimeout(c.UserContext(), d)
}
