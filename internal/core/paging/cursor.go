// Mailyard, Copyright (c) 2021-2026 YouSysAdmin

package paging

import (
	"github.com/gofiber/fiber/v2"

	"github.com/yousysadmin/mailyard/internal/core/keyset"
)

// The cursor type lives in internal/core/keyset, which imports
// nothing. This package reads one off an HTTP request, and it imports
// fiber to do that - which is exactly why the type cannot live here.
// internal/domain/store puts a Cursor in its filter structs, and a
// store interface that drags a web framework in with it is a store
// interface no CLI command or test can touch cheaply.

// CursorFrom reads the cursor query parameter. A malformed value
// yields the zero cursor, which is the first page.
func CursorFrom(c *fiber.Ctx) keyset.Cursor {
	return keyset.DecodeCursor(c.Query("cursor"))
}

// Window is one keyset page request: how many rows, and where to
// resume from.
type Window struct {
	Limit  int
	Cursor keyset.Cursor
}

// WindowFrom reads a keyset page request off the query string, with
// the limit clamped exactly as From clamps it.
func WindowFrom(c *fiber.Ctx) Window {
	return Window{Limit: From(c).Limit, Cursor: CursorFrom(c)}
}

// Fetch is how many rows a store should read for this window: one
// more than asked, so the handler can tell "last page" from "there is
// more" without a second query or a count.
func (w Window) Fetch() int { return w.Limit + 1 }
