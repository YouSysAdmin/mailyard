// Mailyard, Copyright (c) 2021-2026 YouSysAdmin

package cli

import (
	"context"
	"log/slog"
	"testing"
	"time"

	"github.com/yousysadmin/mailyard/internal/domain/store"
	emailmodel "github.com/yousysadmin/mailyard/internal/models/email"
)

type outcomeContacts struct {
	store.ContactStore
	sent map[string]bool
}

func (f *outcomeContacts) RecordOutcome(_ context.Context, _, email, _ string, sent bool, _ time.Time) error {
	f.sent[email] = sent

	return nil
}

// A recipient the server refused on a sent message counts as a failure
// for that address, and the others as delivered.
func TestContactsCountARefusedRecipientAlone(t *testing.T) {
	contacts := &outcomeContacts{sent: map[string]bool{}}
	job := &emailmodel.Email{ID: "e1", ProjectID: "p1", Recipients: []string{"Ann <ann@x.test>", "gone@x.test"}}

	trackContacts(t.Context(), &store.Store{Contact: contacts}, slog.New(slog.DiscardHandler), job, true, []string{"GONE@x.test"})

	if !contacts.sent["ann@x.test"] || contacts.sent["gone@x.test"] {
		t.Errorf("outcomes = %v, want ann delivered and gone failed", contacts.sent)
	}
}
