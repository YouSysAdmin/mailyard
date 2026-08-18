// Mailyard, Copyright (c) 2021-2026 YouSysAdmin

package settings

import (
	"context"
	"testing"

	smodel "github.com/yousysadmin/mailyard/internal/models/setting"
)

type fakeLoader struct {
	rows []*smodel.Setting
	err  error
}

func (f *fakeLoader) All(context.Context) ([]*smodel.Setting, error) {
	return f.rows, f.err
}

func TestDefaultsBeforeAnyLoad(t *testing.T) {
	s := New(&fakeLoader{})
	// Registry defaults must be live immediately: something may read
	// a setting between construction and the first Reload.
	if got := s.Int(smodel.KeyWebhookDeliveryRetentionDays); got != 30 {
		t.Errorf("webhook delivery retention = %d, want the default 30", got)
	}

	if s.Bool(smodel.KeyMaintenanceMode) {
		t.Error("maintenance mode must default to off")
	}
}

func TestReloadAppliesOverrides(t *testing.T) {
	loader := &fakeLoader{rows: []*smodel.Setting{
		{Key: smodel.KeyRetentionDays, Value: "60", Type: smodel.TypeInt},
		{Key: smodel.KeyMaintenanceMode, Value: "true", Type: smodel.TypeBool},
	}}
	s := New(loader)
	if err := s.Reload(t.Context()); err != nil {
		t.Fatal(err)
	}

	if got := s.Int(smodel.KeyRetentionDays); got != 60 {
		t.Errorf("retention = %d, want 60", got)
	}

	if !s.Bool(smodel.KeyMaintenanceMode) {
		t.Error("maintenance mode must be on")
	}

	// Dropping the row restores the default on the next reload.
	loader.rows = nil
	if err := s.Reload(t.Context()); err != nil {
		t.Fatal(err)
	}

	if s.Bool(smodel.KeyMaintenanceMode) {
		t.Error("removing the override must restore the default")
	}

	if got := s.Int(smodel.KeyRetentionDays); got != 30 {
		t.Errorf("retention = %d, want the registry default 30", got)
	}
}

func TestReloadIgnoresUnknownKeys(t *testing.T) {
	// An older binary may have written a key this one no longer
	// knows. It must be skipped, not surfaced.
	s := New(&fakeLoader{rows: []*smodel.Setting{
		{Key: "removed_in_a_later_version", Value: "x", Type: smodel.TypeString},
	}})
	if err := s.Reload(t.Context()); err != nil {
		t.Fatal(err)
	}

	if got := s.String("removed_in_a_later_version"); got != "" {
		t.Errorf("unknown key resolved to %q", got)
	}
}

func TestGarbageValueFallsBackToDefault(t *testing.T) {
	s := New(&fakeLoader{rows: []*smodel.Setting{
		{Key: smodel.KeyWebhookDeliveryRetentionDays, Value: "not-a-number", Type: smodel.TypeInt},
	}})
	if err := s.Reload(t.Context()); err != nil {
		t.Fatal(err)
	}

	if got := s.Int(smodel.KeyWebhookDeliveryRetentionDays); got != 30 {
		t.Errorf("got %d, want the default 30 rather than 0 (which would mean keep forever)", got)
	}
}

func TestValidate(t *testing.T) {
	if _, err := Validate("nope", "1"); err == nil {
		t.Error("unknown key must be rejected")
	}

	if _, err := Validate(smodel.KeyRetentionDays, "abc"); err == nil {
		t.Error("non-numeric int must be rejected")
	}

	if _, err := Validate(smodel.KeyRetentionDays, "-5"); err == nil {
		t.Error("negative retention must be rejected")
	}

	if _, err := Validate(smodel.KeyMaintenanceMode, "maybe"); err == nil {
		t.Error("non-boolean must be rejected")
	}

	got, err := Validate(smodel.KeyMaintenanceMode, "1")
	if err != nil || got != "true" {
		t.Errorf("Validate(bool, \"1\") = %q, %v, want normalized \"true\"", got, err)
	}
}

func TestNilServiceIsSafe(t *testing.T) {
	var s *Service
	if s.Bool(smodel.KeyMaintenanceMode) {
		t.Error("nil service must report false")
	}

	// Zero even though the registry default is 30. A nil service
	// inventing a window that deletes mail is the worst thing it could
	// guess - see the comment on Int.
	if s.Int(smodel.KeyRetentionDays) != 0 {
		t.Error("nil service must report zero")
	}

	if len(s.Snapshot()) != 0 {
		t.Error("nil service must snapshot empty")
	}
}
