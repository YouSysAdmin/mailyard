// Mailyard, Copyright (c) 2021-2026 YouSysAdmin

package notify

import (
	"context"
	"testing"

	"github.com/yousysadmin/mailyard/internal/domain/store"
	nmodel "github.com/yousysadmin/mailyard/internal/models/notification"
)

type fakeNotes struct {
	store.NotificationStore
	filed int
}

func (f *fakeNotes) Create(context.Context, *nmodel.Notification) (bool, error) {
	f.filed++

	return true, nil
}

type fakeAlerts struct{ mailed int }

func (f *fakeAlerts) OnNotification(*nmodel.Notification) { f.mailed++ }

// RaiseInConsole is for a condition its producer already mails, so a
// second mail through the alerter would be a duplicate.
func TestRaiseInConsoleDoesNotMail(t *testing.T) {
	notes, alerts := &fakeNotes{}, &fakeAlerts{}
	r := &Raiser{Store: &store.Store{Notification: notes}, Alerts: alerts}
	n := &nmodel.Notification{ProjectID: "p1", Severity: nmodel.SeverityWarning}

	r.RaiseInConsole(t.Context(), n)
	if notes.filed != 1 || alerts.mailed != 0 {
		t.Fatalf("RaiseInConsole: filed %d mailed %d, want 1 and 0", notes.filed, alerts.mailed)
	}

	r.Raise(t.Context(), n)
	if notes.filed != 2 || alerts.mailed != 1 {
		t.Fatalf("Raise: filed %d mailed %d, want 2 and 1", notes.filed, alerts.mailed)
	}
}
