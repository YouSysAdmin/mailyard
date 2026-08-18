// Mailyard, Copyright (c) 2021-2026 YouSysAdmin

package paging

import (
	"net/http/httptest"
	"strings"
	"testing"
	"unicode/utf8"

	"github.com/gofiber/fiber/v2"
)

// searchOf runs Search over a query string the way a request would.
func searchOf(t *testing.T, raw string) string {
	t.Helper()

	app := fiber.New()
	var got string
	app.Get("/", func(c *fiber.Ctx) error {
		got = Search(c, "q")

		return nil
	})
	req := httptest.NewRequest("GET", "/?q="+raw, nil)
	if _, err := app.Test(req); err != nil {
		t.Fatalf("request: %v", err)
	}

	return got
}

// A two-byte script is the case the OLD code got right by luck: 200 is
// a multiple of 2, so the byte cut landed on a boundary. Kept so the
// rune cut is exercised on more than the one width that used to break.
func TestALongTwoByteTermStaysValidUTF8(t *testing.T) {
	long := strings.Repeat("\u0436", 400)
	got := searchOf(t, long)

	if !utf8.ValidString(got) {
		t.Error("the truncated term is not valid UTF-8, which Postgres refuses")
	}

	if n := utf8.RuneCountInString(got); n != MaxSearchTerm {
		t.Errorf("kept %d runes, want %d", n, MaxSearchTerm)
	}
}

// three bytes is the width that actually broke: 200 is not a multiple
// of 3, so the old byte cut split the last character and produced
// invalid UTF-8, which Postgres refuses with 22021 - not the 22P02 that
// response.Internal turns into a 404. CJK and most scripts above U+07FF
// are three bytes, so search failed for those users and nobody else.
func TestALongThreeByteTermStaysValidUTF8(t *testing.T) {
	got := searchOf(t, strings.Repeat("\u5b57", 400))

	if !utf8.ValidString(got) {
		t.Error("the truncated term is not valid UTF-8")
	}

	if n := utf8.RuneCountInString(got); n != MaxSearchTerm {
		t.Errorf("kept %d runes, want %d", n, MaxSearchTerm)
	}
}

// A short term is returned untouched, trimmed - the cap must not be
// doing anything to ordinary input.
func TestAnOrdinaryTermIsUnchanged(t *testing.T) {
	if got := searchOf(t, "%20bob@example.com%20"); got != "bob@example.com" {
		t.Errorf("got %q, want it trimmed and otherwise untouched", got)
	}
}
