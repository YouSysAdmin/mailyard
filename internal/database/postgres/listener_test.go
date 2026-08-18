// Mailyard, Copyright (c) 2021-2026 YouSysAdmin

package postgres

import (
	"database/sql"
	"io"
	"log/slog"
	"os"
	"testing"
	"time"

	"github.com/yousysadmin/mailyard/internal/database/dbtest"
)

// testChannel is not one of the product channels. NOTIFY is scoped to
// a database, not a schema, so a test firing ChannelEmailQueue would
// be heard by anything else connected to the same test database.
const testChannel = "mailyard_listener_test"

func testListener(t *testing.T) (*Listener, string) {
	t.Helper()
	dsn := os.Getenv(dbtest.DSNEnv)
	if dsn == "" {
		t.Skipf("%s is not set, skipping the tests that need a real database", dbtest.DSNEnv)
	}

	db, err := sql.Open("pgx", dsn)
	if err != nil {
		t.Fatalf("open: %v", err)
	}

	t.Cleanup(func() { _ = db.Close() })
	quiet := slog.New(slog.NewTextHandler(io.Discard, nil))

	return NewListener(db, dsn, quiet), dsn
}

func TestNotifyReachesASubscriber(t *testing.T) {
	l, _ := testListener(t)
	fired := make(chan struct{}, 8)

	l.Subscribe(testChannel, func() { fired <- struct{}{} })
	l.Start(t.Context())

	// Start fires every subscriber once on connect, by design - a node
	// that has just subscribed missed whatever happened before it did.
	// Drain that one before testing the real round trip.
	select {
	case <-fired:
	case <-time.After(10 * time.Second):
		t.Fatal("the subscriber never fired on connect")
	}

	l.Notify(testChannel)
	select {
	case <-fired:
	case <-time.After(10 * time.Second):
		t.Fatal("Notify did not reach the subscriber")
	}
}

// A wake is level-triggered, so a burst must collapse. Without this
// every accepted email would cost a NOTIFY round trip on the request
// path.
func TestNotifyCoalescesABurst(t *testing.T) {
	l, _ := testListener(t)
	fired := make(chan struct{}, 256)

	l.Subscribe(testChannel, func() { fired <- struct{}{} })
	l.Start(t.Context())

	select {
	case <-fired:
	case <-time.After(10 * time.Second):
		t.Fatal("the subscriber never fired on connect")
	}

	for range 100 {
		l.Notify(testChannel)
	}

	// One notifyInterval plus slack: enough for the first send and the
	// one that follows the pause, nowhere near enough for a hundred.
	time.Sleep(notifyInterval + 2*time.Second)

	if n := len(fired); n > 3 {
		t.Errorf("100 notifies produced %d deliveries, want them coalesced into a handful", n)
	} else if n == 0 {
		t.Error("100 notifies produced none at all")
	}
}

func TestListenerRefusesAnUnsafeChannelName(t *testing.T) {
	for _, bad := range []string{"", "Mixed_Case", "has space", `x"; DROP TABLE emails; --`, "1leading"} {
		if safeChannel.MatchString(bad) {
			t.Errorf("safeChannel accepted %q", bad)
		}
	}

	for _, good := range []string{ChannelEmailQueue, ChannelCampaign, testChannel} {
		if !safeChannel.MatchString(good) {
			t.Errorf("safeChannel rejected %q", good)
		}
	}
}
