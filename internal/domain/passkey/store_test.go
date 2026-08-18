// Mailyard, Copyright (c) 2021-2026 YouSysAdmin

package passkey

import (
	"testing"
	"time"

	"github.com/yousysadmin/mailyard/internal/database"
	"github.com/yousysadmin/mailyard/internal/database/dbtest"
	pkmodel "github.com/yousysadmin/mailyard/internal/models/passkey"
)

func newStore(t *testing.T) *Store {
	t.Helper()
	db := dbtest.Open(t)
	dbtest.Migrate(t, db)
	dbtest.Schema(t, db, `
        INSERT INTO users (id, email, account_type, created_at)
        VALUES ('1ce81574-b36f-4f3e-8e6e-dabcaaf8f5b1', 'a@example.com', 1, now()),
               ('c3533f0f-413f-4604-8417-1bbdc59958ec', 'b@example.com', 1, now())`)

	return &Store{Base: database.NewBase(db)}
}

func seed(t *testing.T, s *Store, id, userID, credID, name string) *pkmodel.Passkey {
	t.Helper()
	m := &pkmodel.Passkey{
		ID: id, UserID: userID, CredentialID: credID, Name: name,
		Credential: `{"id":"` + credID + `"}`,
	}
	if err := s.Put(t.Context(), m); err != nil {
		t.Fatalf("put %s: %v", id, err)
	}

	return m
}

func TestPasskeyRoundTrip(t *testing.T) {
	s := newStore(t)
	seed(t, s, "e66e7a4d-9e6c-4884-869a-cf9ffcf22181", "1ce81574-b36f-4f3e-8e6e-dabcaaf8f5b1", "cred-1", "YubiKey")

	got, err := s.GetByCredential(t.Context(), "cred-1")
	if err != nil {
		t.Fatal(err)
	}

	if got == nil {
		t.Fatal("the row came back nil")
	}

	if got.UserID != "1ce81574-b36f-4f3e-8e6e-dabcaaf8f5b1" || got.Name != "YubiKey" || got.Credential != `{"id":"cred-1"}` {
		t.Errorf("round trip lost a field: %+v", got)
	}

	if got.LastUsedAt != nil {
		t.Error("a passkey that has never signed in reports a last use")
	}

	missing, err := s.GetByCredential(t.Context(), "nope")
	if err != nil {
		t.Fatal(err)
	}

	if missing != nil {
		t.Error("an unknown credential resolved to a row")
	}
}

// The sign counter lives inside the credential JSON and has to be
// rewritten on every use. A stale one makes the clone check on the
// NEXT sign-in compare against a number that stopped advancing.
func TestRecordUseRewritesTheCredential(t *testing.T) {
	s := newStore(t)
	seed(t, s, "e66e7a4d-9e6c-4884-869a-cf9ffcf22181", "1ce81574-b36f-4f3e-8e6e-dabcaaf8f5b1", "cred-1", "Key")

	at := time.Now().UTC().Truncate(time.Millisecond)
	if err := s.RecordUse(t.Context(), "e66e7a4d-9e6c-4884-869a-cf9ffcf22181", `{"id":"cred-1","signCount":7}`, at); err != nil {
		t.Fatal(err)
	}

	got, err := s.GetByCredential(t.Context(), "cred-1")
	if err != nil {
		t.Fatal(err)
	}

	if got.Credential != `{"id":"cred-1","signCount":7}` {
		t.Errorf("credential is %q, want the updated one", got.Credential)
	}

	if got.LastUsedAt == nil || !got.LastUsedAt.Equal(at) {
		t.Errorf("last_used_at is %v, want %v", got.LastUsedAt, at)
	}
}

// Every mutating method is user scoped, so one account cannot touch
// another's credentials even holding the id.
func TestMutationsAreUserScoped(t *testing.T) {
	s := newStore(t)
	seed(t, s, "e66e7a4d-9e6c-4884-869a-cf9ffcf22181", "1ce81574-b36f-4f3e-8e6e-dabcaaf8f5b1", "cred-1", "Key")

	renamed, err := s.Rename(t.Context(), "c3533f0f-413f-4604-8417-1bbdc59958ec", "e66e7a4d-9e6c-4884-869a-cf9ffcf22181", "stolen")
	if err != nil {
		t.Fatal(err)
	}

	if renamed {
		t.Error("another user renamed a passkey that is not theirs")
	}

	removed, err := s.Delete(t.Context(), "c3533f0f-413f-4604-8417-1bbdc59958ec", "e66e7a4d-9e6c-4884-869a-cf9ffcf22181")
	if err != nil {
		t.Fatal(err)
	}

	if removed {
		t.Error("another user deleted a passkey that is not theirs")
	}

	// The owner can do both.
	if renamed, err = s.Rename(t.Context(), "1ce81574-b36f-4f3e-8e6e-dabcaaf8f5b1", "e66e7a4d-9e6c-4884-869a-cf9ffcf22181", "renamed"); err != nil || !renamed {
		t.Fatalf("owner rename: %v %v", renamed, err)
	}

	if removed, err = s.Delete(t.Context(), "1ce81574-b36f-4f3e-8e6e-dabcaaf8f5b1", "e66e7a4d-9e6c-4884-869a-cf9ffcf22181"); err != nil || !removed {
		t.Fatalf("owner delete: %v %v", removed, err)
	}
}

func TestListAndCountAreScopedToTheOwner(t *testing.T) {
	s := newStore(t)
	seed(t, s, "e66e7a4d-9e6c-4884-869a-cf9ffcf22181", "1ce81574-b36f-4f3e-8e6e-dabcaaf8f5b1", "cred-1", "One")
	seed(t, s, "6a5f0b90-6a56-47f4-8926-7cc56968798b", "1ce81574-b36f-4f3e-8e6e-dabcaaf8f5b1", "cred-2", "Two")
	seed(t, s, "601da9d8-a9cd-49ad-8ca1-7998351403ef", "c3533f0f-413f-4604-8417-1bbdc59958ec", "cred-3", "Theirs")

	rows, err := s.ListForUser(t.Context(), "1ce81574-b36f-4f3e-8e6e-dabcaaf8f5b1")
	if err != nil {
		t.Fatal(err)
	}

	if len(rows) != 2 {
		t.Fatalf("got %d passkeys, want 2", len(rows))
	}

	n, err := s.CountForUser(t.Context(), "1ce81574-b36f-4f3e-8e6e-dabcaaf8f5b1")
	if err != nil {
		t.Fatal(err)
	}

	if n != 2 {
		t.Errorf("count is %d, want 2", n)
	}
}

// The credential id is what an assertion names, so two accounts
// holding the same one would make the owner ambiguous. The unique
// index is what stops that, not write-path discipline.
func TestACredentialCannotBeEnrolledTwice(t *testing.T) {
	s := newStore(t)
	seed(t, s, "e66e7a4d-9e6c-4884-869a-cf9ffcf22181", "1ce81574-b36f-4f3e-8e6e-dabcaaf8f5b1", "cred-1", "Mine")

	err := s.Put(t.Context(), &pkmodel.Passkey{
		ID: "6a5f0b90-6a56-47f4-8926-7cc56968798b", UserID: "c3533f0f-413f-4604-8417-1bbdc59958ec", CredentialID: "cred-1", Name: "Also mine",
		Credential: `{}`,
	})
	if err == nil {
		t.Fatal("the same credential was enrolled on a second account")
	}
}

// Deleting the account takes its credentials with it. Without the
// cascade a re-created user id would inherit somebody else's way in.
func TestDeletingTheUserRemovesItsPasskeys(t *testing.T) {
	s := newStore(t)
	seed(t, s, "e66e7a4d-9e6c-4884-869a-cf9ffcf22181", "1ce81574-b36f-4f3e-8e6e-dabcaaf8f5b1", "cred-1", "Key")

	if _, err := s.DB().ExecContext(t.Context(), s.Q(`DELETE FROM users WHERE id = ?`), "1ce81574-b36f-4f3e-8e6e-dabcaaf8f5b1"); err != nil {
		t.Fatal(err)
	}

	got, err := s.GetByCredential(t.Context(), "cred-1")
	if err != nil {
		t.Fatal(err)
	}

	if got != nil {
		t.Error("the passkey outlived the account it belonged to")
	}
}
