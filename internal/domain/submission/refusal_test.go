// Mailyard, Copyright (c) 2021-2026 YouSysAdmin

package submission

import (
	"errors"
	"log/slog"
	"net"
	"net/textproto"
	"strings"
	"sync/atomic"
	"testing"

	akmodel "github.com/yousysadmin/mailyard/internal/models/apikey"
)

// startTuned is startServer with a small size limit and a maintenance
// switch the test controls.
func startTuned(t *testing.T, parked *atomic.Bool) (string, string) {
	t.Helper()

	token, prefix, hash, err := akmodel.Generate()
	if err != nil {
		t.Fatal(err)
	}

	b := &Backend{
		Keys: &fakeKeys{key: &akmodel.Key{
			ID: "eea04bb8-feb6-49e8-8e3d-69758376292a", ProjectID: "proj-1", CreatedBy: "user-1",
			KeyPrefix: prefix, KeyHash: hash, Permissions: canSend,
		}},
		Sender:         &fakeSender{},
		Projects:       &fakeProjects{},
		Log:            slog.New(slog.DiscardHandler),
		MaxMessageSize: 4096,
		Maintenance:    parked.Load,
	}

	srv := NewServer(b, "127.0.0.1:0", "test", nil)
	ln, err := net.Listen("tcp", srv.Addr)
	if err != nil {
		t.Fatal(err)
	}

	go func() { _ = srv.Serve(ln) }()
	t.Cleanup(func() { _ = srv.Close() })

	return ln.Addr().String(), token
}

func smtpCode(err error) int {
	if te, ok := errors.AsType[*textproto.Error](err); ok {
		return te.Code
	}

	return 0
}

// Maintenance defers new mail with a 4xx, and lifting it lets the same
// message through.
func TestMaintenanceDefersSubmission(t *testing.T) {
	var parked atomic.Bool
	parked.Store(true)
	addr, token := startTuned(t, &parked)

	err := submit(addr, token, "ann@example.com", []string{"bob@example.com"}, testMsg)
	if code := smtpCode(err); code != 451 {
		t.Fatalf("parked: %v (code %d), want 451", err, code)
	}

	parked.Store(false)
	if err := submit(addr, token, "ann@example.com", []string{"bob@example.com"}, testMsg); err != nil {
		t.Fatalf("after maintenance: %v", err)
	}
}

// A message that can never be accepted is refused permanently, so the
// client stops retrying it: one over the size limit sent without a
// SIZE declaration, and one carrying a line over the line limit.
func TestAnUnacceptableMessageIsAPermanentRefusal(t *testing.T) {
	var parked atomic.Bool
	addr, token := startTuned(t, &parked)

	oversize := testMsg + strings.Repeat("0123456789abcdef\r\n", 1000)
	if code := smtpCode(submit(addr, token, "ann@example.com", []string{"bob@example.com"}, oversize)); code != 552 {
		t.Errorf("oversize: code %d, want 552", code)
	}

	longLine := testMsg + strings.Repeat("x", 3000) + "\r\n"
	if code := smtpCode(submit(addr, token, "ann@example.com", []string{"bob@example.com"}, longLine)); code < 500 {
		t.Errorf("long line: code %d, want a 5xx", code)
	}
}
