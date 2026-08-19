// Mailyard, Copyright (c) 2021-2026 YouSysAdmin

// Package eventstream serves the project live feed over
// server-sent events.
//
// SSE rather than websockets: the traffic is one-way, it is plain
// HTTP so proxies and the existing auth middleware need no special
// handling, and the browser reconnects on its own.
package eventstream

import (
	"bufio"
	"encoding/json"
	"fmt"
	"time"

	"github.com/gofiber/fiber/v3"
	"github.com/yousysadmin/mailyard/internal/core/env"
	"github.com/yousysadmin/mailyard/internal/core/eventbus"
	"github.com/yousysadmin/mailyard/internal/core/response"
	"github.com/yousysadmin/mailyard/internal/domain"
)

// Handler owns GET /api/events/stream.
type Handler struct {
	Runtime *env.Runtime
}

// heartbeat keeps idle connections open. Proxies and load balancers
// close a connection that has been silent too long, and the browser
// would then reconnect in a loop. A comment line costs nothing and is
// ignored by EventSource.
const heartbeat = 25 * time.Second

// MaxStreamLife caps a single connection. EventSource reconnects
// automatically, so recycling costs the reader nothing and stops a
// forgotten tab holding a goroutine and a database-free but non-zero
// slice of memory for weeks.
//
// Exported because the HTTP edge has to know it: fasthttp applies ONE
// write deadline to a streamed body, so internal/server raises it above
// this value for the streaming paths. Left at the app-wide WriteTimeout
// the stream would be cut at two minutes and the reader would reconnect
// forever.
const MaxStreamLife = 30 * time.Minute

// Stream opens the feed for the caller's active project.
//
// Authentication is the ordinary session cookie. The old design
// documented a ?token= query parameter, on the grounds that
// EventSource cannot set headers - but it does send cookies on
// same-origin requests, which is what the console makes. Putting a
// JWT in a URL would write it into access logs, proxy logs, and
// Referer headers for no benefit, so this endpoint does not accept
// one.
func (h *Handler) Stream(c fiber.Ctx) error {
	rc := domain.GetRequestContext(c)
	if rc == nil || rc.Project == nil {
		return response.Forbidden(c, "project not resolved")
	}

	if h.Runtime.Events == nil {
		return response.BadRequest(c, "the event stream is not available")
	}

	projectID := rc.Project.ID

	c.Set(fiber.HeaderContentType, "text/event-stream")
	c.Set(fiber.HeaderCacheControl, "no-cache")
	c.Set(fiber.HeaderConnection, "keep-alive")
	// Nginx buffers proxied responses by default, which holds events
	// until the buffer fills and makes a live stream anything but.
	c.Set("X-Accel-Buffering", "no")

	// Subscribe before the write loop starts so events raised between
	// the handler running and the stream opening are not lost.
	sub := h.Runtime.Events.Subscribe(projectID)

	// Fiber hands the connection to fasthttp's stream writer, which
	// runs after the handler returns. Everything the loop needs is
	// captured here.
	return c.SendStreamWriter(func(w *bufio.Writer) {
		defer sub.Close()

		deadline := time.NewTimer(MaxStreamLife)
		defer deadline.Stop()
		ticker := time.NewTicker(heartbeat)
		defer ticker.Stop()

		// An opening event tells the client the stream is live, and
		// flushes the headers through any proxy that is holding them.
		if !writeEvent(w, "system.connected", fiber.Map{"project_id": projectID}) {
			return
		}

		for {
			select {
			case e, ok := <-sub.C:
				if !ok {
					return
				}

				if !writeEvent(w, e.Type, fiber.Map{"at": e.At, "data": e.Data}) {
					return
				}
			case <-ticker.C:
				// A comment line. EventSource ignores it, proxies see
				// traffic.
				if _, err := w.WriteString(": ping\n\n"); err != nil {
					return
				}

				if err := w.Flush(); err != nil {
					return
				}
			case <-deadline.C:
				// Ask the client to come straight back, then close.
				_, _ = w.WriteString("retry: 1000\n")
				writeEvent(w, "system.reconnect", fiber.Map{"reason": "connection recycled"})

				return
			}
		}
	})
}

// writeEvent emits one SSE frame. Returns false when the connection
// has gone, which is the normal way a stream ends - a reader closing
// a tab is not an error worth logging.
func writeEvent(w *bufio.Writer, event string, payload any) bool {
	body, err := json.Marshal(payload)
	if err != nil {
		return false
	}

	if _, err := fmt.Fprintf(w, "event: %s\ndata: %s\n\n", event, body); err != nil {
		return false
	}

	return w.Flush() == nil
}

// Stats reports live subscriber counts, for the admin view.
func (h *Handler) Stats(c fiber.Ctx) error {
	subs, projects, dropped := h.Runtime.Events.Stats()

	return response.Success(c, StatsResponse{
		Subscribers: subs,
		Projects:    projects,
		Dropped:     dropped,
	})
}

// Publish is the helper other domains use, so call sites do not have
// to nil-check the bus.
func Publish(rt *env.Runtime, e eventbus.Event) {
	if rt == nil {
		return
	}

	rt.Events.Publish(e)
}
