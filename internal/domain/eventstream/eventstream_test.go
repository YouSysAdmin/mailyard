// Mailyard, Copyright (c) 2021-2026 YouSysAdmin

package eventstream

import (
	"bufio"
	"log/slog"
	"net"
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/gofiber/fiber/v3"
	"github.com/yousysadmin/mailyard/internal/core/env"
	"github.com/yousysadmin/mailyard/internal/core/eventbus"
	"github.com/yousysadmin/mailyard/internal/domain"
	"github.com/yousysadmin/mailyard/internal/models/permission"
	projmodel "github.com/yousysadmin/mailyard/internal/models/project"
)

// streamTypes opens the stream as a caller holding perms, publishes
// events in order and returns the event types the caller received up to
// and including notification.created, which is published last.
func streamTypes(t *testing.T, perms permission.Set, publish ...string) []string {
	t.Helper()

	const projectID = "p-stream"

	bus := eventbus.New()
	t.Cleanup(bus.Close)

	h := &Handler{Runtime: &env.Runtime{Events: bus, Log: slog.New(slog.DiscardHandler)}}

	app := fiber.New()
	app.Get("/events/stream", func(c fiber.Ctx) error {
		c.Locals(domain.ContextKey, &domain.RequestContext{
			Project:     &projmodel.Project{ID: projectID},
			Permissions: perms,
		})

		return h.Stream(c)
	})

	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}

	go func() { _ = app.Listener(ln, fiber.ListenConfig{DisableStartupMessage: true}) }()

	t.Cleanup(func() { _ = app.ShutdownWithTimeout(100 * time.Millisecond) })

	client := &http.Client{Timeout: 5 * time.Second}

	resp, err := client.Get("http://" + ln.Addr().String() + "/events/stream")
	if err != nil {
		t.Fatal(err)
	}

	defer func() { _ = resp.Body.Close() }()

	r := bufio.NewReader(resp.Body)
	if got := readEvent(t, r); got != "system.connected" {
		t.Fatalf("first event = %q, want system.connected", got)
	}

	for _, typ := range append(publish, eventbus.TypeNotification) {
		bus.Publish(eventbus.Event{Type: typ, ProjectID: projectID, Data: map[string]any{"subject": "secret"}})
	}

	var got []string
	for {
		typ := readEvent(t, r)
		got = append(got, typ)

		if typ == eventbus.TypeNotification {
			return got
		}
	}
}

// readEvent returns the type of the next SSE frame.
func readEvent(t *testing.T, r *bufio.Reader) string {
	t.Helper()

	for {
		line, err := r.ReadString('\n')
		if err != nil {
			t.Fatalf("stream ended: %v", err)
		}

		if typ, ok := strings.CutPrefix(line, "event: "); ok {
			return strings.TrimSpace(typ)
		}
	}
}

func TestTheStreamForwardsOnlyWhatTheCallerMayRead(t *testing.T) {
	published := []string{
		eventbus.TypeEmailSent,
		eventbus.TypeEmailFailed,
		eventbus.TypeInboundReceived,
		eventbus.TypeCampaignProgress,
		"unknown.type",
	}

	notifRead := permission.Of(permission.ResourceNotifications, permission.ActionRead)
	emailsRead := permission.Of(permission.ResourceEmails, permission.ActionRead)

	got := streamTypes(t, permission.NewSet(notifRead), published...)
	if strings.Join(got, ",") != eventbus.TypeNotification {
		t.Errorf("notifications:read received %v, want only %s", got, eventbus.TypeNotification)
	}

	got = streamTypes(t, permission.NewSet(notifRead, emailsRead), published...)

	want := eventbus.TypeEmailSent + "," + eventbus.TypeEmailFailed + "," + eventbus.TypeNotification
	if strings.Join(got, ",") != want {
		t.Errorf("notifications:read + emails:read received %v, want %s", got, want)
	}

	got = streamTypes(t, permission.NewSet(permission.All), published...)

	want = strings.Join([]string{
		eventbus.TypeEmailSent,
		eventbus.TypeEmailFailed,
		eventbus.TypeInboundReceived,
		eventbus.TypeCampaignProgress,
		eventbus.TypeNotification,
	}, ",")
	if strings.Join(got, ",") != want {
		t.Errorf("owner received %v, want %s", got, want)
	}
}
