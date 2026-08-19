// Mailyard, Copyright (c) 2021-2026 YouSysAdmin

package audit

import (
	"strings"
	"time"

	"github.com/gofiber/fiber/v3"

	"github.com/yousysadmin/mailyard/internal/core/env"
	"github.com/yousysadmin/mailyard/internal/core/paging"
	"github.com/yousysadmin/mailyard/internal/core/response"
	"github.com/yousysadmin/mailyard/internal/domain"
	amodel "github.com/yousysadmin/mailyard/internal/models/audit"
)

// Handler serves the two trails.
type Handler struct {
	Runtime *env.Runtime
}

const (
	defaultPageSize = 50
	maxPageSize     = 200
)

// ProjectLog returns configuration activity for the active
// project. Mounted behind requireProjectRole(admin) - the trail
// names who changed what, which is not something every viewer should read.
func (h *Handler) ProjectLog(c fiber.Ctx) error {
	rc := domain.GetRequestContext(c)
	pg := paging.FromWith(c, defaultPageSize, maxPageSize)
	limit, offset := pg.Limit, pg.Offset
	events, err := h.Runtime.Store.Audit.ListProject(c.Context(), rc.Project.ID, limit, offset)
	if err != nil {
		return response.Internal(c, err)
	}

	return response.Success(c, ListResponse{Events: orEmpty(events), Limit: limit, Offset: offset})
}

// ProjectEvent returns one project event by id, same gate as the
// list. Cross-project (or security-trail) ids look like a missing row.
func (h *Handler) ProjectEvent(c fiber.Ctx) error {
	rc := domain.GetRequestContext(c)
	e, err := h.Runtime.Store.Audit.GetProject(c.Context(), rc.Project.ID, c.Params("id"))
	if err != nil {
		return response.Internal(c, err)
	}

	if e == nil {
		return response.NotFound(c, "audit event not found")
	}

	return response.Success(c, EventResponse{Event: e})
}

// SecurityLog returns account activity. A platform admin sees every
// account, everyone else sees only their own - a non-admin reading
// other people's sign-in times and IPs would be a new disclosure.
func (h *Handler) SecurityLog(c fiber.Ctx) error {
	rc := domain.GetRequestContext(c)
	pg := paging.FromWith(c, defaultPageSize, maxPageSize)
	limit, offset := pg.Limit, pg.Offset

	var (
		events []*amodel.Event
		err    error
	)
	if rc != nil && rc.User != nil && rc.User.IsAdmin() && c.Query("all") == "true" {
		events, err = h.Runtime.Store.Audit.ListSecurity(c.Context(), limit, offset)
	} else {
		actorID := ""
		if rc != nil && rc.User != nil {
			actorID = rc.User.ID
		}

		events, err = h.Runtime.Store.Audit.ListForActor(c.Context(), actorID, limit, offset)
	}

	if err != nil {
		return response.Internal(c, err)
	}

	return response.Success(c, ListResponse{Events: orEmpty(events), Limit: limit, Offset: offset})
}

// exportCap bounds one export.
//
// The trail grows per REQUEST, not per thing a person made, so there is
// no window an operator can be trusted to have narrowed - "everything"
// is the ordinary ask. The response is built in memory and sent as one
// document, so it needs a ceiling, and hitting it is REPORTED rather
// than silent: the reason to export is having the record elsewhere, and
// a file that stopped early without saying so is worse than an error.
//
// The same number as the data export's suppression cap, for the same
// reason and with the same shape.
const exportCap = 50000

// ProjectLogExport is the project trail over a window, unpaged.
//
// A separate route rather than a big limit on the list: the list is a
// screen and answers with a page, this is a file and answers with a
// window. One route trying to be both would need the caller to know
// that a limit above some number changes what the endpoint IS.
func (h *Handler) ProjectLogExport(c fiber.Ctx) error {
	rc := domain.GetRequestContext(c)
	from, to, resp, ok := exportWindow(c)
	if !ok {
		return resp
	}

	events, err := h.Runtime.Store.Audit.ExportProject(c.Context(), rc.Project.ID, from, to, exportCap+1)
	if err != nil {
		return response.Internal(c, err)
	}

	return exported(c, events, from, to)
}

// SecurityLogExport is the same for account activity, with the same
// audience rule as SecurityLog: your own unless you administer the
// installation and ask for every account.
func (h *Handler) SecurityLogExport(c fiber.Ctx) error {
	rc := domain.GetRequestContext(c)
	from, to, resp, ok := exportWindow(c)
	if !ok {
		return resp
	}

	var (
		events []*amodel.Event
		err    error
	)
	if rc != nil && rc.User != nil && rc.User.IsAdmin() && c.Query("all") == "true" {
		events, err = h.Runtime.Store.Audit.ExportSecurity(c.Context(), from, to, exportCap+1)
	} else {
		actorID := ""
		if rc != nil && rc.User != nil {
			actorID = rc.User.ID
		}

		events, err = h.Runtime.Store.Audit.ExportForActor(c.Context(), actorID, from, to, exportCap+1)
	}

	if err != nil {
		return response.Internal(c, err)
	}

	return exported(c, events, from, to)
}

// exported trims to the cap and says whether it had to.
//
// The store is asked for cap+1 rows, which is how "there was more" is
// known without a COUNT over a table that grows per request - the same
// trick keyset paging uses to answer "is there another page".
func exported(c fiber.Ctx, events []*amodel.Event, from, to time.Time) error {
	truncated := len(events) > exportCap
	if truncated {
		events = events[:exportCap]
	}

	return response.Success(c, ExportResponse{
		Events:    orEmpty(events),
		From:      from,
		To:        to,
		Count:     len(events),
		Truncated: truncated,
		Cap:       exportCap,
	})
}

// exportWindow reads the optional from/to bounds.
//
// Both ends accept a plain DATE or a full RFC 3339 timestamp, because
// the two callers are different: a person types 2026-08-01, a script
// passes the instant it last exported. A bare date for `to` means the
// whole of that day - an operator asking for "the 1st to the 3rd" and
// getting nothing from the 3rd would have to know the bound is
// exclusive, and nobody should have to.
//
// Returns (from, to, resp, ok) rather than an error, for the reason
// verifySession does: response.* writes the status and returns nil, so
// a lone error would be nil on a refusal and the caller would carry on.
func exportWindow(c fiber.Ctx) (time.Time, time.Time, error, bool) {
	// Not time.Time{}: a Go zero time is year 1 and Postgres accepts it,
	// but it also serializes into the response as 0001-01-01, which reads
	// like a bug in the export rather than "from the beginning".
	from := time.Unix(0, 0).UTC()
	to := time.Now().UTC()

	if raw := strings.TrimSpace(c.Query("from")); raw != "" {
		// A bare date needs no adjustment at this end: midnight IS the
		// start of the day somebody meant.
		t, _, err := parseBound(raw)
		if err != nil {
			return from, to, response.BadRequest(c,
				"from must be a date (2026-08-01) or an RFC 3339 timestamp"), false
		}

		from = t
	}

	if raw := strings.TrimSpace(c.Query("to")); raw != "" {
		t, dateOnly, err := parseBound(raw)
		if err != nil {
			return from, to, response.BadRequest(c,
				"to must be a date (2026-08-01) or an RFC 3339 timestamp"), false
		}

		if dateOnly {
			t = t.AddDate(0, 0, 1)
		}

		to = t
	}

	if !to.After(from) {
		return from, to, response.BadRequest(c, "to must be after from"), false
	}

	return from, to, nil, true
}

// parseBound reads one end of the window and says whether it was a bare
// date, which is what decides where the day ends.
func parseBound(raw string) (time.Time, bool, error) {
	if t, err := time.Parse(time.RFC3339, raw); err == nil {
		return t.UTC(), false, nil
	}

	t, err := time.Parse("2006-01-02", raw)
	if err != nil {
		return time.Time{}, false, err
	}

	return t.UTC(), true, nil
}

func orEmpty(in []*amodel.Event) []*amodel.Event {
	if in == nil {
		return []*amodel.Event{}
	}

	return in
}
