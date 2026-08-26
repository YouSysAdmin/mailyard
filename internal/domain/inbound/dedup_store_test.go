// Mailyard, Copyright (c) 2021-2026 YouSysAdmin

package inbound

import (
	"context"
	"errors"
	"io"
	"log/slog"
	"testing"
	"time"

	"github.com/yousysadmin/mailyard/internal/core/ids"
	"github.com/yousysadmin/mailyard/internal/database"
	"github.com/yousysadmin/mailyard/internal/database/dbtest"
	imodel "github.com/yousysadmin/mailyard/internal/models/inbound"
)

func dedupStore(t *testing.T) (*Store, string, context.Context) {
	t.Helper()

	db := dbtest.Open(t)
	dbtest.Migrate(t, db)
	s := &Store{Base: database.NewBase(db)}
	ctx := t.Context()

	proj := ids.New()
	if _, err := db.ExecContext(ctx, `
		INSERT INTO projects (id, name, slug, owner_id, created_at)
		VALUES ($1, 'acme', $2, NULL, now())`, proj, proj); err != nil {
		t.Fatalf("seed project: %v", err)
	}

	return s, proj, ctx
}

func arrived(proj, messageID, hash string) *imodel.Email {
	return &imodel.Email{
		ID:         ids.New(),
		ProjectID:  proj,
		MessageID:  messageID,
		DedupHash:  hash,
		Sender:     "sender@x.test",
		Recipients: []string{"in@acme.test"},
		Subject:    "hello",
		Status:     imodel.StatusReceived,
		ReceivedAt: time.Now().UTC(),
	}
}

// Deduplication is enforced by the DATABASE, not by the read before the
// write.
//
// Ingest reads for the Message-ID and inserts when it finds nothing,
// which settles an ordinary MTA retry and settles nothing about two
// deliveries arriving together - both read nothing, and the indexes were
// plain, so both rows landed. Each duplicate is a second
// inbound.received webhook, and for a bounce report a second bounce row
// and a second suppression.
//
// This is the second write refused, which is the whole mechanism. It
// needs no concurrency to assert: what the race depends on is that
// nothing rejects the second INSERT, and that is a property of the
// index, not of timing.
func TestASecondCopyOfAMessageIsRefused(t *testing.T) {
	s, proj, ctx := dedupStore(t)

	hash := dedupHash("<abc@sender.test>", "sender@x.test", []string{"in@acme.test"}, "hello", 42)
	if err := s.Put(ctx, arrived(proj, "<abc@sender.test>", hash)); err != nil {
		t.Fatalf("first: %v", err)
	}

	err := s.Put(ctx, arrived(proj, "<abc@sender.test>", hash))
	if err == nil {
		t.Fatal("a second copy of the same message was stored - " +
			"the dedup index is not unique, so two deliveries arriving together both land")
	}

	if !database.UniqueViolation(err, "idx_inbound_dedup") {
		t.Fatalf("refused for another reason: %v", err)
	}

	// The SAME Message-ID over DIFFERENT content is a different
	// message, not a duplicate. The id used to be a key of its own,
	// and a stranger who could guess it pre-empted the real message.
	other := dedupHash("<abc@sender.test>", "sender@x.test", []string{"in@acme.test"}, "something else", 99)
	if err := s.Put(ctx, arrived(proj, "<abc@sender.test>", other)); err != nil {
		t.Fatalf("a different message under a reused Message-ID was refused: %v", err)
	}
}

// The indexes are PARTIAL, and this is why.
//
// A message refused at the suppression check or by DMARC is persisted
// for the audit trail before it is ever parsed, so it carries neither a
// Message-ID nor a hash - and so does one whose MIME failed to parse.
// Both columns default to the empty string. A full unique index would allow exactly
// one such row per project and refuse every rejection after the first,
// which is the audit trail going silent at the point it matters most.
func TestRejectionsWithoutAMessageIDAreNotEachOthersDuplicates(t *testing.T) {
	s, proj, ctx := dedupStore(t)

	for i := range 3 {
		rec := arrived(proj, "", "")
		rec.Status = imodel.StatusRejected
		rec.ErrorMessage = "sender is on the suppression list"
		if err := s.Put(ctx, rec); err != nil {
			t.Fatalf("rejection %d: %v - a partial index would allow all three", i, err)
		}
	}

	var n int
	if err := s.DB().QueryRowContext(ctx,
		`SELECT count(*) FROM inbound_emails WHERE project_id = $1 AND message_id = ''`,
		proj).Scan(&n); err != nil {
		t.Fatalf("count: %v", err)
	}

	if n != 3 {
		t.Errorf("%d rows with no Message-ID, want 3", n)
	}
}

// Two projects receiving the same message are not duplicates of each
// other - the key is (project, id), and a forwarder fanning one message
// at two tenants is ordinary.
func TestTwoProjectsMayHoldTheSameMessage(t *testing.T) {
	s, proj, ctx := dedupStore(t)

	other := ids.New()
	if _, err := s.DB().ExecContext(ctx, `
		INSERT INTO projects (id, name, slug, owner_id, created_at)
		VALUES ($1, 'other', $2, NULL, now())`, other, other); err != nil {
		t.Fatalf("seed project: %v", err)
	}

	hash := dedupHash("<shared@sender.test>", "sender@x.test", []string{"in@acme.test"}, "hello", 42)
	if err := s.Put(ctx, arrived(proj, "<shared@sender.test>", hash)); err != nil {
		t.Fatalf("first project: %v", err)
	}

	if err := s.Put(ctx, arrived(other, "<shared@sender.test>", hash)); err != nil {
		t.Errorf("second project refused a message of its own: %v", err)
	}
}

// Losing the race must answer exactly as the fast path does, or the
// sender is told a message failed that is sitting in the table.
//
// duplicateOf turns the refusal into the row that won it. The named
// index is load-bearing: reading any 23505 as "already ingested" would
// answer 250 for a message that was never stored, which is the worst
// way to lose mail - the sender believes it arrived and never retries.
func TestTheRowThatWonTheRaceIsWhatTheLoserReports(t *testing.T) {
	s, proj, ctx := dedupStore(t)
	svc := &Service{Inbound: s, Log: slog.New(slog.NewTextHandler(io.Discard, nil))}

	hash := dedupHash("<abc@sender.test>", "sender@x.test", []string{"in@acme.test"}, "hello", 42)
	winner := arrived(proj, "<abc@sender.test>", hash)
	if err := s.Put(ctx, winner); err != nil {
		t.Fatalf("winner: %v", err)
	}

	loser := arrived(proj, "<abc@sender.test>", hash)
	err := s.Put(ctx, loser)
	if err == nil {
		t.Fatal("the second insert was allowed")
	}

	existing := svc.duplicateOf(ctx, loser, err)
	if existing == nil {
		t.Fatal("a dedup violation did not resolve to the row that caused it - " +
			"the relay node echoes this id back to the sender")
	}

	if existing.ID != winner.ID {
		t.Errorf("reported id %s, want the row already stored (%s)", existing.ID, winner.ID)
	}

	// An unrelated unique violation is not a duplicate. The primary key
	// is the one to hand: re-inserting an id that exists is a fault, and
	// must not be reported as mail that already arrived.
	same := arrived(proj, "<other@sender.test>", "")
	same.ID = winner.ID

	// ON CONFLICT(id) makes this an update rather than an error, which
	// is why the check below asks the helper directly instead.
	if err := s.Put(ctx, same); err != nil {
		t.Fatalf("update by id: %v", err)
	}

	if got := svc.duplicateOf(ctx, loser, errors.New("connection reset")); got != nil {
		t.Error("a plain error was read as a duplicate")
	}
}
