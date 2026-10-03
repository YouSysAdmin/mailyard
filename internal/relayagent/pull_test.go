// Mailyard, Copyright (c) 2021-2026 YouSysAdmin

package relayagent

import (
	"encoding/base64"
	"slices"
	"testing"

	"github.com/yousysadmin/mailyard/internal/core/ids"
)

// A delivered message leaves the queue before its outcome is reported,
// and the platform keeps handing it out until that report lands. The
// node must go on naming it as held, and must not spool it again if it
// comes back anyway.
func TestADeliveredButUnreportedMessageIsNotClaimedAgain(t *testing.T) {
	s := testSpool(t)
	a := &Agent{spool: s}
	emailID := ids.New()
	claimed := ClaimedMessage{
		ID:           emailID,
		EnvelopeFrom: "bounces@mail.example.com",
		Recipients:   []string{"user@example.org"},
		RawB64:       base64.StdEncoding.EncodeToString([]byte(rawBody)),
	}

	n, err := a.spoolClaimed(claimed)
	if err != nil || n != 1 {
		t.Fatalf("first claim spooled %d, err %v", n, err)
	}

	// What the deliverer does on success: the outcome is written down,
	// then the message leaves the queue.
	if err := s.PutOutcome(OutcomeKey(emailID, "user@example.org"), []byte(`{}`)); err != nil {
		t.Fatalf("PutOutcome: %v", err)
	}

	if err := s.Remove(claimedID(emailID, "example.org")); err != nil {
		t.Fatalf("Remove: %v", err)
	}

	held, err := s.EmailIDs()
	if err != nil {
		t.Fatalf("EmailIDs: %v", err)
	}

	if !slices.Contains(held, emailID) {
		t.Errorf("held %v does not name the unreported message", held)
	}

	n, err = a.spoolClaimed(claimed)
	if err != nil {
		t.Fatalf("second claim: %v", err)
	}

	if n != 0 {
		t.Errorf("the delivered message was spooled again (%d entries)", n)
	}

	// Once reported, nothing names it any more.
	if err := s.RemoveOutcomes([]string{OutcomeKey(emailID, "user@example.org")}); err != nil {
		t.Fatalf("RemoveOutcomes: %v", err)
	}

	held, err = s.EmailIDs()
	if err != nil {
		t.Fatalf("EmailIDs: %v", err)
	}

	if len(held) != 0 {
		t.Errorf("held after report: %v", held)
	}
}
