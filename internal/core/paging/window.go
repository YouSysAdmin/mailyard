// Mailyard, Copyright (c) 2021-2026 YouSysAdmin

package paging

import (
	"errors"
	"strings"
	"time"

	"github.com/gofiber/fiber/v3"
)

// Bound parses one end of a time window: an RFC 3339 instant, or a
// bare date, reported as such because a date names a whole day and
// which end of the day it means depends on which end of the window it
// sits at.
func Bound(raw string) (t time.Time, dateOnly bool, err error) {
	if t, err := time.Parse(time.RFC3339, raw); err == nil {
		return t.UTC(), false, nil
	}

	t, err = time.Parse("2006-01-02", raw)
	if err != nil {
		return time.Time{}, false, err
	}

	return t.UTC(), true, nil
}

// ErrBound is what a window parameter that is neither a date nor an
// instant fails with. The message is the one a caller shows.
var ErrBound = errors.New("must be a date (2026-08-01) or an RFC 3339 timestamp")

// Instant reads one query parameter as a point in time, nil when it is
// absent. A bare date is its midnight.
func Instant(c fiber.Ctx, name string) (*time.Time, error) {
	raw := strings.TrimSpace(c.Query(name))
	if raw == "" {
		return nil, nil
	}

	t, _, err := Bound(raw)
	if err != nil {
		return nil, ErrBound
	}

	return &t, nil
}

// TimeWindow reads ?from= and ?to= as a half-open window, either end
// optional. A bare date on the to side includes that whole day, which
// is what somebody typing it means.
func TimeWindow(c fiber.Ctx) (from, to *time.Time, err error) {
	if raw := strings.TrimSpace(c.Query("from")); raw != "" {
		t, _, err := Bound(raw)
		if err != nil {
			return nil, nil, ErrBound
		}

		from = &t
	}

	if raw := strings.TrimSpace(c.Query("to")); raw != "" {
		t, dateOnly, err := Bound(raw)
		if err != nil {
			return nil, nil, ErrBound
		}

		if dateOnly {
			t = t.AddDate(0, 0, 1)
		}

		to = &t
	}

	if from != nil && to != nil && !to.After(*from) {
		return nil, nil, errors.New("to must be after from")
	}

	return from, to, nil
}
