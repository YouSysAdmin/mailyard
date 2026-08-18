// Mailyard, Copyright (c) 2021-2026 YouSysAdmin

package email

import (
	"testing"
	"time"

	"github.com/yousysadmin/mailyard/internal/database"
	"github.com/yousysadmin/mailyard/internal/database/dbtest"
	emailmodel "github.com/yousysadmin/mailyard/internal/models/email"
)

// newRetentionStore builds just the columns the retention statements
// touch.
func newRetentionStore(t *testing.T) *Store {
	t.Helper()
	db := dbtest.Open(t)
	dbtest.Schema(t, db, `
        CREATE TABLE emails (
            id               TEXT PRIMARY KEY,
            project_id     TEXT NOT NULL,
            status           TEXT NOT NULL,
            sender           TEXT NOT NULL DEFAULT '',
            recipients       TEXT NOT NULL DEFAULT '[]',
            html_body        TEXT NOT NULL DEFAULT '',
            text_body        TEXT NOT NULL DEFAULT '',
            attachments_json TEXT NOT NULL DEFAULT '[]',
            created_at       TIMESTAMPTZ NOT NULL
        )`)

	return &Store{Base: database.NewBase(db)}
}

func insertRow(t *testing.T, s *Store, id, status string, createdAt time.Time, key string) {
	t.Helper()
	atts := `[{"filename":"a.pdf","storage_key":"` + key + `"}]`
	if _, err := s.DB().ExecContext(t.Context(), s.Q(`
        INSERT INTO emails (id, project_id, status, attachments_json, created_at)
        VALUES (?, '9ae7f5c4-9322-4ace-8b74-4bb1d3f60040', ?, ?, ?)`), id, status, atts, createdAt); err != nil {
		t.Fatal(err)
	}
}

// The retention sweep deletes blobs first and then clears the rows
// that referenced them, so the two statements must agree on which
// rows are in scope. They did not: the key collector had no in-flight
// exemption while the clear did, so a message scheduled beyond the
// attachment window had its object deleted out from under a row that
// kept the storage_key - and the send later failed on a file that no
// longer existed.
func TestStorageKeysOlderThanSkipsInFlightRows(t *testing.T) {
	s := newRetentionStore(t)
	old := time.Now().UTC().AddDate(0, 0, -60)
	cutoff := time.Now().UTC().AddDate(0, 0, -30)

	// Terminal rows: their blobs are genuinely expired.
	insertRow(t, s, "e-sent", emailmodel.StatusSent, old, "blob/sent")
	insertRow(t, s, "e-failed", emailmodel.StatusFailed, old, "blob/failed")
	// In-flight rows older than the cutoff. A campaign scheduled months
	// out is the ordinary way to end up here.
	insertRow(t, s, "e-scheduled", emailmodel.StatusScheduled, old, "blob/scheduled")
	insertRow(t, s, "e-queued", emailmodel.StatusQueued, old, "blob/queued")
	insertRow(t, s, "e-processing", emailmodel.StatusProcessing, old, "blob/processing")

	keys, err := s.StorageKeysOlderThan(t.Context(), cutoff)
	if err != nil {
		t.Fatal(err)
	}

	got := map[string]bool{}
	for _, k := range keys {
		got[k] = true
	}

	for _, live := range []string{"blob/scheduled", "blob/queued", "blob/processing"} {
		if got[live] {
			t.Errorf("%s belongs to a live row and must not be handed to the blob deleter", live)
		}
	}

	for _, dead := range []string{"blob/sent", "blob/failed"} {
		if !got[dead] {
			t.Errorf("%s is expired and should have been collected", dead)
		}
	}
}

// The collector and the clear must cover the same rows: anything the
// collector returns should be a row the clear then blanks, or the blob
// is deleted while a reference to it survives.
func TestStorageKeyCollectionMatchesAttachmentClear(t *testing.T) {
	s := newRetentionStore(t)
	old := time.Now().UTC().AddDate(0, 0, -60)
	cutoff := time.Now().UTC().AddDate(0, 0, -30)

	insertRow(t, s, "e-sent", emailmodel.StatusSent, old, "blob/sent")
	insertRow(t, s, "e-scheduled", emailmodel.StatusScheduled, old, "blob/scheduled")

	keys, err := s.StorageKeysOlderThan(t.Context(), cutoff)
	if err != nil {
		t.Fatal(err)
	}

	cleared, err := s.ClearAttachmentsOlderThan(t.Context(), cutoff)
	if err != nil {
		t.Fatal(err)
	}

	if int64(len(keys)) != cleared {
		t.Fatalf("collected %d blob keys but cleared %d rows - every deleted blob must lose its reference",
			len(keys), cleared)
	}

	// The live row keeps its attachment metadata intact.
	var atts string
	if err := s.DB().QueryRowContext(t.Context(),
		`SELECT attachments_json FROM emails WHERE id = 'e-scheduled'`).Scan(&atts); err != nil {
		t.Fatal(err)
	}

	if atts == "[]" {
		t.Error("the scheduled row lost its attachments - it has not been sent yet")
	}
}
