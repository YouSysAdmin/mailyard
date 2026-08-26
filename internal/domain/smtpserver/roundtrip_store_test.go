// Mailyard, Copyright (c) 2021-2026 YouSysAdmin

package smtpserver

import (
	"testing"
	"time"

	"github.com/yousysadmin/mailyard/internal/core/crypto"
	"github.com/yousysadmin/mailyard/internal/core/ids"
	"github.com/yousysadmin/mailyard/internal/core/smtpclient"
	"github.com/yousysadmin/mailyard/internal/core/transport"
	"github.com/yousysadmin/mailyard/internal/database/dbtest"
	ssmodel "github.com/yousysadmin/mailyard/internal/models/smtpserver"
)

// The project table has the same shape the shared one is guarded for:
// one positional INSERT, one positional SELECT, one positional scan.
// Three lists to edit and no compiler between them - a column added to
// two of the three is silent, and the shared table had a test for this
// while the project table, which is the one every tenant writes, did
// not.
//
// Every field is set to something distinctive so a mismatch shows up as
// the wrong VALUE rather than as a type error, which is how these go
// unnoticed.
func TestAProjectServerSurvivesARoundTrip(t *testing.T) {
	db := dbtest.Open(t)
	dbtest.Migrate(t, db)
	ctx := t.Context()

	proj := ids.New()
	if _, err := db.ExecContext(ctx, `
		INSERT INTO projects (id, name, slug, owner_id, created_at)
		VALUES ($1, 'acme', $2, NULL, now())`, proj, proj); err != nil {
		t.Fatalf("seed project: %v", err)
	}

	s := NewStore(db, crypto.New("0123456789abcdef0123456789abcdef"))
	validated := time.Now().Add(-time.Hour).UTC().Truncate(time.Second)
	want := &ssmodel.Server{
		ID:              ids.New(),
		ProjectID:       proj,
		Name:            "the-name",
		Host:            "smtp.example.net",
		Port:            2525,
		Username:        "the-username",
		Password:        "the-password",
		Encryption:      smtpclient.EncryptionSTARTTLS,
		AllowedEmails:   []string{"news@a.test", "*@b.test"},
		AllowedDomains:  []string{"a.test", "c.test"},
		Priority:        7,
		Status:          ssmodel.StatusEnabled,
		ValidationError: "last test failed",
		ValidatedAt:     &validated,
		CreatedAt:       time.Now().UTC().Truncate(time.Second),
	}
	if err := s.Put(ctx, want); err != nil {
		t.Fatalf("Put: %v", err)
	}

	got, err := s.Get(ctx, proj, want.ID)
	if err != nil || got == nil {
		t.Fatalf("Get: %v (row %v)", err, got)
	}

	if got.Name != want.Name || got.Host != want.Host || got.Port != want.Port ||
		got.Username != want.Username || got.Encryption != want.Encryption {
		t.Errorf("identity fields came back wrong: %+v", got)
	}

	// Sealed on the way in, plain on the way out. A column read one
	// position off would come back as some other field's text and
	// decrypt to nothing.
	if got.Password != want.Password {
		t.Errorf("password = %q, want %q", got.Password, want.Password)
	}

	if len(got.AllowedEmails) != 2 || got.AllowedEmails[0] != "news@a.test" {
		t.Errorf("allowed_emails = %v", got.AllowedEmails)
	}

	if len(got.AllowedDomains) != 2 || got.AllowedDomains[0] != "a.test" ||
		got.AllowedDomains[1] != "c.test" {
		t.Errorf("allowed_domains = %v", got.AllowedDomains)
	}

	if got.Priority != want.Priority || got.Status != want.Status ||
		got.ValidationError != want.ValidationError {
		t.Errorf("status fields came back wrong: %+v", got)
	}

	if got.ValidatedAt == nil || !got.ValidatedAt.Equal(validated) {
		t.Errorf("validated_at = %v, want %v", got.ValidatedAt, validated)
	}

	// Set by Normalize on the way in rather than by the caller, so this
	// also proves the store still calls it.
	if got.Provider != transport.ProviderSMTP {
		t.Errorf("provider = %q, want the default filled in by Normalize", got.Provider)
	}
}

// The domain list is cleaned by the store, not only by the handler, so
// an enrolling relay node gets the same treatment a console edit does.
func TestTheStoreCleansTheDomainListItWrites(t *testing.T) {
	db := dbtest.Open(t)
	dbtest.Migrate(t, db)
	ctx := t.Context()

	proj := ids.New()
	if _, err := db.ExecContext(ctx, `
		INSERT INTO projects (id, name, slug, owner_id, created_at)
		VALUES ($1, 'acme', $2, NULL, now())`, proj, proj); err != nil {
		t.Fatalf("seed project: %v", err)
	}

	s := NewStore(db, crypto.New("0123456789abcdef0123456789abcdef"))
	srv := &ssmodel.Server{
		ID: ids.New(), ProjectID: proj, Name: "n", Host: "h", Port: 25,
		Status:         ssmodel.StatusEnabled,
		AllowedDomains: []string{" A.test ", "@b.test"},
		CreatedAt:      time.Now().UTC(),
	}
	if err := s.Put(ctx, srv); err != nil {
		t.Fatalf("Put: %v", err)
	}

	got, err := s.Get(ctx, proj, srv.ID)
	if err != nil || got == nil {
		t.Fatalf("Get: %v", err)
	}

	if len(got.AllowedDomains) != 2 || got.AllowedDomains[0] != "a.test" ||
		got.AllowedDomains[1] != "b.test" {
		t.Errorf("allowed_domains = %q, want cleaned", got.AllowedDomains)
	}
}
