// Mailyard, Copyright (c) 2021-2026 YouSysAdmin

package relaynode

import (
	"testing"
	"time"

	"github.com/yousysadmin/mailyard/internal/database/dbtest"
	nodemodel "github.com/yousysadmin/mailyard/internal/models/relaynode"
)

// An assignment is claimed by listing, narrowed by a report, kept alive
// by the node saying it holds it, and taken back by the sweep once it
// does not.
func TestAnAssignmentLivesAsLongAsTheNodeClaimsIt(t *testing.T) {
	db := dbtest.Open(t)
	dbtest.Migrate(t, db)
	s := NewStore(db)
	ctx := t.Context()

	const nodeID = "0195d1a2-7c3e-7f00-8000-0000000000aa"
	const emailID = "0195d1a2-7c3e-7f00-8000-0000000000ee"
	if err := s.Put(ctx, &nodemodel.Node{ID: nodeID, ServerID: "0195d1a2-7c3e-7f00-8000-0000000000bb",
		TokenHash: "x", Name: "n", Mode: nodemodel.ModePull}); err != nil {
		t.Fatal(err)
	}

	n, err := s.Get(ctx, nodeID)
	if err != nil || !n.Pulls() {
		t.Fatalf("node mode after Put: %+v %v, want pull", n, err)
	}

	now := time.Now().UTC().Truncate(time.Microsecond)
	if err := s.Assign(ctx, &nodemodel.Assignment{
		EmailID: emailID, NodeID: nodeID, ServerID: n.ServerID, EmailCreatedAt: now,
		EnvelopeFrom: "b@x.test", Recipients: []string{"a@one.test", "b@two.test"},
		Raw: []byte("From: x\r\n\r\nbody"), CreatedAt: now, ExpiresAt: now.Add(time.Minute),
	}); err != nil {
		t.Fatal(err)
	}

	got, err := s.ListAssigned(ctx, nodeID, 10, nil)
	if err != nil || len(got) != 1 || string(got[0].Raw) != "From: x\r\n\r\nbody" || len(got[0].Recipients) != 2 {
		t.Fatalf("ListAssigned: %+v %v", got, err)
	}

	// A message the node says it already holds is not handed back.
	if held, err := s.ListAssigned(ctx, nodeID, 10, []string{emailID}); err != nil || len(held) != 0 {
		t.Fatalf("ListAssigned excluding held: %+v %v, want none", held, err)
	}

	// Another node sees nothing of it.
	if other, _ := s.GetAssignment(ctx, "0195d1a2-7c3e-7f00-8000-0000000000cc", emailID); other != nil {
		t.Fatal("another node could read the assignment")
	}

	// Half reported: the assignment narrows and the delivered count
	// carries over.
	if err := s.UpdateAssignment(ctx, nodeID, emailID, []string{"b@two.test"}, 1, now.Add(2*time.Minute)); err != nil {
		t.Fatal(err)
	}

	a, _ := s.GetAssignment(ctx, nodeID, emailID)
	if a == nil || len(a.Recipients) != 1 || a.Delivered != 1 {
		t.Fatalf("after narrowing: %+v", a)
	}

	// Not yet expired, so the sweep leaves it alone.
	if exp, _ := s.ExpiredAssignments(ctx, now.Add(90*time.Second), 10); len(exp) != 0 {
		t.Fatalf("expired before its time: %+v", exp)
	}

	// The node says it still holds it: the expiry moves out.
	if err := s.ExtendAssignments(ctx, nodeID, []string{emailID}, now.Add(time.Hour)); err != nil {
		t.Fatal(err)
	}

	if exp, _ := s.ExpiredAssignments(ctx, now.Add(10*time.Minute), 10); len(exp) != 0 {
		t.Fatalf("expired although extended: %+v", exp)
	}

	// Past the extension the sweep sees it, without the bytes.
	exp, err := s.ExpiredAssignments(ctx, now.Add(2*time.Hour), 10)
	if err != nil || len(exp) != 1 || exp[0].EmailID != emailID || len(exp[0].Raw) != 0 {
		t.Fatalf("ExpiredAssignments: %+v %v", exp, err)
	}

	// Another node cannot end it, the owner can.
	if gone, _ := s.DeleteAssignment(ctx, "0195d1a2-7c3e-7f00-8000-0000000000cc", emailID); gone {
		t.Fatal("another node deleted the assignment")
	}

	if gone, err := s.DeleteAssignment(ctx, nodeID, emailID); err != nil || !gone {
		t.Fatalf("DeleteAssignment by the owner: gone=%v err=%v", gone, err)
	}

	if left, _ := s.ListAssigned(ctx, nodeID, 10, nil); len(left) != 0 {
		t.Fatalf("still assigned after delete: %+v", left)
	}
}
