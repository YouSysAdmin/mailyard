// Mailyard, Copyright (c) 2021-2026 YouSysAdmin

package sandbox

import (
	"slices"
	"testing"
	"time"

	"github.com/yousysadmin/mailyard/internal/core/ids"
	"github.com/yousysadmin/mailyard/internal/domain/store"
	sbmodel "github.com/yousysadmin/mailyard/internal/models/sandbox"
)

func testInboxStore(t *testing.T) (*Store, *InboxStore) {
	t.Helper()
	s := testStore(t)

	return s, NewInboxStore(s.DB())
}

func putFrom(t *testing.T, s *Store, projID, sender string) *sbmodel.Email {
	t.Helper()
	e := put(t, s, projID, time.Now().UTC(), "from "+sender, nil)
	if _, err := s.Exec(t.Context(), `UPDATE sandbox_emails SET sender = ? WHERE id = ?`, sender, e.ID); err != nil {
		t.Fatalf("set sender: %v", err)
	}

	e.Sender = sender

	return e
}

// An inbox is a filter over the envelope sender, compared without
// regard to case: the address list is stored lowercased and the
// capture keeps whatever the client wrote. The page and the count
// beside it have to agree.
func TestListFiltersBySender(t *testing.T) {
	s, _ := testInboxStore(t)
	proj := newProject(t, s)
	a1 := putFrom(t, s, proj, "a@example.test")
	a2 := putFrom(t, s, proj, "A@Example.TEST")
	putFrom(t, s, proj, "b@example.test")

	f := store.SandboxFilter{Addresses: []string{"a@example.test", "nobody@example.test"}, Limit: 50}
	list, err := s.List(t.Context(), proj, f)
	if err != nil {
		t.Fatalf("list: %v", err)
	}

	got := []string{}
	for _, e := range list {
		got = append(got, e.ID)
	}

	slices.Sort(got)
	want := []string{a1.ID, a2.ID}
	slices.Sort(want)
	if !slices.Equal(got, want) {
		t.Errorf("filtered list = %v, want %v", got, want)
	}

	n, err := s.Count(t.Context(), proj, f)
	if err != nil {
		t.Fatalf("count: %v", err)
	}

	if n != 2 {
		t.Errorf("count = %d, want 2", n)
	}

	all, err := s.Count(t.Context(), proj, store.SandboxFilter{})
	if err != nil {
		t.Fatalf("count all: %v", err)
	}

	if all != 3 {
		t.Errorf("unfiltered count = %d, want 3", all)
	}
}

func TestAnInboxSurvivesARoundTrip(t *testing.T) {
	s, is := testInboxStore(t)
	proj := newProject(t, s)
	in := &sbmodel.Inbox{
		ID:          ids.New(),
		ProjectID:   proj,
		Name:        "checkout",
		Description: "the checkout service",
		Addresses:   []string{"checkout@example.test", "orders@example.test"},
	}
	if err := is.Put(t.Context(), in); err != nil {
		t.Fatalf("put: %v", err)
	}

	got, err := is.Get(t.Context(), proj, in.ID)
	if err != nil {
		t.Fatalf("get: %v", err)
	}

	if got == nil {
		t.Fatal("inbox not found after put")
	}

	if got.Name != in.Name || got.Description != in.Description || !slices.Equal(got.Addresses, in.Addresses) {
		t.Errorf("round trip changed the inbox: %+v", got)
	}

	if got.UpdatedAt != nil {
		t.Error("a fresh inbox reports an update time")
	}

	in.Addresses = []string{"orders@example.test"}
	in.UpdatedAt = new(time.Now().UTC())
	if err := is.Put(t.Context(), in); err != nil {
		t.Fatalf("update: %v", err)
	}

	got, _ = is.Get(t.Context(), proj, in.ID)
	if got == nil || !slices.Equal(got.Addresses, in.Addresses) || got.UpdatedAt == nil {
		t.Errorf("update did not land: %+v", got)
	}

	byName, err := is.GetByName(t.Context(), proj, "checkout")
	if err != nil || byName == nil || byName.ID != in.ID {
		t.Errorf("GetByName = %+v, %v", byName, err)
	}

	list, err := is.List(t.Context(), proj)
	if err != nil || len(list) != 1 {
		t.Fatalf("list = %d inboxes, %v", len(list), err)
	}

	if err := is.Delete(t.Context(), proj, in.ID); err != nil {
		t.Fatalf("delete: %v", err)
	}

	if got, _ := is.Get(t.Context(), proj, in.ID); got != nil {
		t.Error("inbox survived delete")
	}
}

// An inbox with no addresses scans as an empty list, not nil, so the
// handler's "no addresses means an empty page" check reads a length.
func TestAnEmptyAddressListScansAsEmpty(t *testing.T) {
	s, is := testInboxStore(t)
	proj := newProject(t, s)
	in := &sbmodel.Inbox{ID: ids.New(), ProjectID: proj, Name: "empty"}
	if err := is.Put(t.Context(), in); err != nil {
		t.Fatalf("put: %v", err)
	}

	got, err := is.Get(t.Context(), proj, in.ID)
	if err != nil || got == nil {
		t.Fatalf("get: %+v, %v", got, err)
	}

	if got.Addresses == nil || len(got.Addresses) != 0 {
		t.Errorf("addresses = %#v, want an empty list", got.Addresses)
	}
}

func TestAnotherProjectSeesNoInbox(t *testing.T) {
	s, is := testInboxStore(t)
	mine, theirs := newProject(t, s), newProject(t, s)
	in := &sbmodel.Inbox{ID: ids.New(), ProjectID: mine, Name: "private", Addresses: []string{"x@example.test"}}
	if err := is.Put(t.Context(), in); err != nil {
		t.Fatalf("put: %v", err)
	}

	if got, _ := is.Get(t.Context(), theirs, in.ID); got != nil {
		t.Error("an inbox was readable from another project")
	}

	if got, _ := is.GetByName(t.Context(), theirs, "private"); got != nil {
		t.Error("an inbox was readable by name from another project")
	}

	list, err := is.List(t.Context(), theirs)
	if err != nil || len(list) != 0 {
		t.Errorf("another project listed %d inboxes, %v", len(list), err)
	}

	if err := is.Delete(t.Context(), theirs, in.ID); err != nil {
		t.Fatalf("cross-project delete errored: %v", err)
	}

	if got, _ := is.Get(t.Context(), mine, in.ID); got == nil {
		t.Error("another project deleted the inbox")
	}
}

// Two inboxes with one name in a project is refused by the schema, and
// the same name in another project is fine.
func TestInboxNamesAreUniquePerProject(t *testing.T) {
	s, is := testInboxStore(t)
	mine, theirs := newProject(t, s), newProject(t, s)
	if err := is.Put(t.Context(), &sbmodel.Inbox{ID: ids.New(), ProjectID: mine, Name: "dup"}); err != nil {
		t.Fatalf("first put: %v", err)
	}

	if err := is.Put(t.Context(), &sbmodel.Inbox{ID: ids.New(), ProjectID: mine, Name: "dup"}); err == nil {
		t.Error("a second inbox with the same name was accepted")
	}

	if err := is.Put(t.Context(), &sbmodel.Inbox{ID: ids.New(), ProjectID: theirs, Name: "dup"}); err != nil {
		t.Errorf("the same name in another project was refused: %v", err)
	}
}
