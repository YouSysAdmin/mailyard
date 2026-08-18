// Mailyard, Copyright (c) 2021-2026 YouSysAdmin

// Package paging reads the limit and offset query parameters that
// every list endpoint accepts.
//
// It exists because the same twelve lines were written in each
// handler that pages, with the ceiling drifting between them, and
// because some handlers passed the raw parameter through to their
// store and relied on the store's own clamp instead. Two layers each
// half-responsible for the same bound is how one of them ends up
// missing. Handlers clamp here, stores keep their clamp as a
// backstop for callers that are not HTTP handlers.
package paging

import (
	"strings"

	"github.com/gofiber/fiber/v2"

	"github.com/yousysadmin/mailyard/internal/core/safetext"
)

// DefaultLimit and MaxLimit apply when a caller does not ask for a
// specific window. MaxLimit is the ceiling a caller cannot raise:
// without it, ?limit=1000000 turns one request into a full table
// scan and a response nobody can render.
const (
	DefaultLimit = 50
	MaxLimit     = 200
)

// Page is a resolved, already-clamped window. Both fields are safe to
// hand straight to a store.
type Page struct {
	Limit  int
	Offset int
}

// From reads limit and offset from the query string using the
// package defaults.
func From(c *fiber.Ctx) Page {
	return FromWith(c, DefaultLimit, MaxLimit)
}

// FromWith is From with an endpoint-specific default and ceiling, for
// the few lists whose rows are much larger or much smaller than
// average. Pass 0 for either to take the package value.
//
// A caller asking for more than max gets max rather than an error:
// paging parameters are a hint about how much to send, and failing a
// list request over one is unhelpful.
func FromWith(c *fiber.Ctx, def, max int) Page {
	if def <= 0 {
		def = DefaultLimit
	}

	if max <= 0 {
		max = MaxLimit
	}

	if def > max {
		def = max
	}

	limit := c.QueryInt("limit", def)
	if limit < 1 {
		limit = def
	}

	if limit > max {
		limit = max
	}

	offset := c.QueryInt("offset", 0)
	// The original API paged by zero-based page number. Honor it when
	// offset was not given, so a client written against those docs
	// keeps working.
	if offset == 0 {
		if p := c.QueryInt("page", 0); p > 0 {
			offset = p * limit
		}
	}

	if offset < 0 {
		offset = 0
	}

	return Page{Limit: limit, Offset: offset}
}

// MaxSearchTerm bounds a free-text search term.
//
// 200 characters is longer than any address (320 is the RFC ceiling
// for a whole address, and nobody pastes one that long into a filter)
// and longer than any subject somebody types into a box. What it stops
// is the other direction: a megabyte of text becomes a megabyte LIKE
// pattern matched against every row of a table that grows per message,
// and the caller pays nothing to send it.
const MaxSearchTerm = 200

// Search reads a free-text term from the query string, trimmed and
// bounded.
//
// Truncated rather than refused, for the same reason an over-large limit
// is clamped: a search is a hint about what to show, and a term this
// long matches nothing either way.
//
// Cut on a RUNE boundary, through safetext. term[:200] splits whatever
// multi-byte character straddles byte 200, and the invalid UTF-8 that
// results is refused by Postgres with 22021 - not the 22P02 that
// response.Internal softens into a 404, so the search box answers 500.
// The same hazard was then found on every other header reaching a TEXT
// column, which is why the mechanics live in safetext rather than here.
func Search(c *fiber.Ctx, param string) string {
	return safetext.Clamp(strings.TrimSpace(c.Query(param)), MaxSearchTerm)
}
