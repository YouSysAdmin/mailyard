// Mailyard, Copyright (c) 2021-2026 YouSysAdmin

package analytics

import (
	"time"

	"github.com/gofiber/fiber/v3"

	"github.com/yousysadmin/mailyard/internal/core/env"
	"github.com/yousysadmin/mailyard/internal/core/response"
	"github.com/yousysadmin/mailyard/internal/domain"
	amodel "github.com/yousysadmin/mailyard/internal/models/analytics"
	emailmodel "github.com/yousysadmin/mailyard/internal/models/email"
)

// Handler serves /api/dashboard/stats and /api/analytics.
type Handler struct {
	Runtime *env.Runtime
}

// maxRange bounds a query window. A year of daily buckets is 365
// rows, which is a chart. Ten years is a denial of service dressed
// as a date picker.
const maxRange = 366 * 24 * time.Hour

// DashboardStats returns the project summary.
func (h *Handler) DashboardStats(c fiber.Ctx) error {
	rc := domain.GetRequestContext(c)
	sum, err := h.Runtime.Store.Analytics.Summary(c.Context(), rc.Project.ID)
	if err != nil {
		return response.Internal(c, err)
	}

	return response.Success(c, StatsResponse{Stats: sum})
}

// Analytics returns the delivery trend over a date range.
func (h *Handler) Analytics(c fiber.Ctx) error {
	rc := domain.GetRequestContext(c)

	from, to, err := parseRange(c)
	if err != nil {
		return response.BadRequest(c, err.Error())
	}

	status := c.Query("status")
	if status != "" && !emailmodel.ValidStatus(status) {
		return response.BadRequest(c, "unknown status "+status)
	}

	daily, err := h.Runtime.Store.Analytics.DailyCounts(c.Context(), rc.Project.ID, from, to, status)
	if err != nil {
		return response.Internal(c, err)
	}

	breakdown, err := h.Runtime.Store.Analytics.StatusBreakdown(c.Context(), rc.Project.ID, from, to)
	if err != nil {
		return response.Internal(c, err)
	}

	if daily == nil {
		daily = []amodel.DayCount{}
	}

	return response.Success(c, TrendResponse{
		DailyCounts:     daily,
		StatusBreakdown: breakdown,
		From:            from.Format("2006-01-02"),
		To:              to.Add(-time.Second).Format("2006-01-02"),
	})
}

// parseRange reads from/to as YYYY-MM-DD, defaulting to the trailing
// 30 days. The returned window is half-open [from, to) with `to`
// advanced to the end of its day, so a range of one day includes that
// whole day rather than only its first instant.
func parseRange(c fiber.Ctx) (from, to time.Time, err error) {
	const layout = "2006-01-02"
	now := time.Now().UTC()

	to = time.Date(now.Year(), now.Month(), now.Day(), 0, 0, 0, 0, time.UTC).AddDate(0, 0, 1)
	if raw := c.Query("to"); raw != "" {
		parsed, perr := time.Parse(layout, raw)
		if perr != nil {
			return from, to, errBadDate("to")
		}

		to = parsed.UTC().AddDate(0, 0, 1)
	}

	from = to.AddDate(0, 0, -30)
	if raw := c.Query("from"); raw != "" {
		parsed, perr := time.Parse(layout, raw)
		if perr != nil {
			return from, to, errBadDate("from")
		}

		from = parsed.UTC()
	}

	if !from.Before(to) {
		return from, to, errRange("from must be before to")
	}

	if to.Sub(from) > maxRange {
		return from, to, errRange("the range must not exceed 366 days")
	}

	return from, to, nil
}

type rangeError struct{ msg string }

// Error renders the failure for a log or a caller.
func (e *rangeError) Error() string { return e.msg }

func errBadDate(field string) error {
	return &rangeError{msg: field + " must be a date in YYYY-MM-DD form"}
}
func errRange(msg string) error { return &rangeError{msg: msg} }
