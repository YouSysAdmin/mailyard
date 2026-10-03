// Mailyard, Copyright (c) 2021-2026 YouSysAdmin

package submission

import (
	"errors"
	"log/slog"
	"strings"
	"testing"

	"github.com/emersion/go-smtp"
)

// The X-Mailyard-Sandbox opt-in writes to the sandbox, so a principal
// without sandbox:write is refused rather than captured or delivered.
func TestTheSandboxHeaderNeedsSandboxWrite(t *testing.T) {
	sender := &fakeSender{}
	s := &session{
		backend: &Backend{Sender: sender, Log: slog.New(slog.DiscardHandler)},
		auth:    &principal{projectID: "proj-1", label: "api key k"},
		from:    "ann@example.com",
		to:      []string{"bob@example.com"},
	}

	msg := "From: ann@example.com\r\nTo: bob@example.com\r\nSubject: t\r\n" +
		"X-Mailyard-Sandbox: 1\r\n\r\nbody\r\n"
	err := s.Data(strings.NewReader(msg))

	se, ok := errors.AsType[*smtp.SMTPError](err)
	if !ok || se.Code != 550 || !strings.Contains(se.Message, "sandbox:write") {
		t.Fatalf("err = %v, want a 550 naming sandbox:write", err)
	}

	if sender.lastReq != nil {
		t.Error("a refused opt-in was delivered for real")
	}
}
