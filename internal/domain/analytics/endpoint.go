// Mailyard, Copyright (c) 2021-2026 YouSysAdmin

package analytics

import (
	"time"

	"github.com/gofiber/fiber/v3"

	"github.com/yousysadmin/mailyard/internal/core/env"
	"github.com/yousysadmin/mailyard/internal/core/paging"
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

// parseRange reads from/to through paging.TimeWindow like every other
// window, defaulting to the trailing 30 days. The returned window is
// half-open [from, to), and a bare date on the to side includes that
// whole day.
func parseRange(c fiber.Ctx) (from, to time.Time, err error) {
	f, t, err := paging.TimeWindow(c)
	if err != nil {
		return from, to, err
	}

	now := time.Now().UTC()
	to = time.Date(now.Year(), now.Month(), now.Day(), 0, 0, 0, 0, time.UTC).AddDate(0, 0, 1)
	if t != nil {
		to = t.UTC()
	}

	from = to.AddDate(0, 0, -30)
	if f != nil {
		from = f.UTC()
	}

	if !from.Before(to) {
		return from, to, errRange("to must be after from")
	}

	if to.Sub(from) > maxRange {
		return from, to, errRange("the range must not exceed 366 days")
	}

	return from, to, nil
}

type rangeError struct{ msg string }

// Error renders the failure for a log or a caller.
func (e *rangeError) Error() string { return e.msg }

func errRange(msg string) error { return &rangeError{msg: msg} }
