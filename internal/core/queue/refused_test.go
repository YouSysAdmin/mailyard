// Mailyard, Copyright (c) 2021-2026 YouSysAdmin

package queue

import (
	"errors"
	"log/slog"
	"testing"

	emailmodel "github.com/yousysadmin/mailyard/internal/models/email"
)

// A message sent with some recipients refused is finalized as sent, the
// refusals kept as its error text, and the hook told who was refused.
func TestASentMessageCarriesItsRefusals(t *testing.T) {
	job := queuedEmail("partial")
	src := newMemSource(job)
	w := NewWorker(src, funcProcessor(func(*emailmodel.Email) Outcome { return Done() }),
		testConfig(), slog.New(slog.DiscardHandler))

	var refused []string
	w.OnFinal = func(_ *emailmodel.Email, _, _ string, r []string) { refused = r }
	w.Complete(t.Context(), job, DoneRefusing("srv-1",
		errors.New("delivered, but 1 recipient(s) were refused"), []string{"gone@x.test"}))

	if got := src.statusOf("partial"); got != emailmodel.StatusSent {
		t.Fatalf("status = %s, want sent", got)
	}

	if src.rows["partial"].ErrorMessage == "" || len(refused) != 1 || refused[0] != "gone@x.test" {
		t.Errorf("error = %q, refused = %v, want the refusal recorded", src.rows["partial"].ErrorMessage, refused)
	}
}
