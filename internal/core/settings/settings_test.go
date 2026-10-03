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

// A day count past the ceiling is refused at the write, and one already
// stored is read back clamped, so no retention cutoff can land in the
// future.
func TestIntSettingsAreBounded(t *testing.T) {
	for _, tc := range []struct {
		key, value string
		ok         bool
	}{
		{smodel.KeyAuditLogRetentionDays, "9223372036854775807", false},
		{smodel.KeyAuditLogRetentionDays, "100000000", false},
		{smodel.KeyAuditLogRetentionDays, "3650", true},
		{smodel.KeyAuditLogRetentionDays, "3651", false},
		{smodel.KeyBounceAlertPercent, "100", true},
		{smodel.KeyBounceAlertPercent, "101", false},
		{smodel.KeySandboxMaxMessages, "2000000", false},
	} {
		_, err := Validate(tc.key, tc.value)
		if (err == nil) != tc.ok {
			t.Errorf("Validate(%s, %s) err = %v, want ok=%v", tc.key, tc.value, err, tc.ok)
		}
	}

	s := New(&fakeLoader{rows: []*smodel.Setting{
		{Key: smodel.KeyAuditLogRetentionDays, Value: "9223372036854775807"},
		{Key: smodel.KeyRetentionDays, Value: "-5"},
		{Key: smodel.KeyBounceAlertPercent, Value: "500"},
	}})
	if err := s.Reload(t.Context()); err != nil {
		t.Fatal(err)
	}

	if got := s.Int(smodel.KeyAuditLogRetentionDays); got != smodel.MaxDays {
		t.Errorf("stored huge window read as %d, want the ceiling %d", got, smodel.MaxDays)
	}

	if got := s.Int(smodel.KeyRetentionDays); got != 0 {
		t.Errorf("stored negative window read as %d, want 0 (keep)", got)
	}

	if got := s.Int(smodel.KeyBounceAlertPercent); got != 100 {
		t.Errorf("stored percentage read as %d, want 100", got)
	}
}

func TestPerKeyRulesRefuseWhatCannotWork(t *testing.T) {
	for _, tc := range []struct {
		key, value string
		ok         bool
	}{
		{smodel.KeyACMEHosts, `["mail.example.com"]`, true},
		{smodel.KeyACMEHosts, `["*.example.com"]`, false},
		{smodel.KeyACMEHosts, `["bad host"]`, false},
		{smodel.KeyACMEHosts, `["10.0.0.1"]`, false},
		{smodel.KeyACMEHosts, `["localhost"]`, false},
		{smodel.KeyACMEHosts, `not json`, false},
		{smodel.KeyACMEEmail, "ops@example.com", true},
		{smodel.KeyACMEEmail, "bad", false},
		{smodel.KeyACMEEmail, "", true},
		{smodel.KeyACMEDirectoryURL, "https://acme-staging-v02.api.letsencrypt.org/directory", true},
		{smodel.KeyACMEDirectoryURL, "ftp://x", false},
		{smodel.KeyACMEDirectoryURL, "http://x", false},
		{smodel.KeyPlatformMailFromName, "Acme Ops", true},
		{smodel.KeyPlatformMailFromName, "Acme\r\nBcc: x@y", false},
	} {
		_, err := Validate(tc.key, tc.value)
		if (err == nil) != tc.ok {
			t.Errorf("Validate(%s, %q) err = %v, want ok=%v", tc.key, tc.value, err, tc.ok)
		}
	}

	got, err := Validate(smodel.KeyACMEHosts, `["Mail.Example.COM"]`)
	if err != nil || got != `["mail.example.com"]` {
		t.Errorf("hosts normalized to %q, %v", got, err)
	}
}
