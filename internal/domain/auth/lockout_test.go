// Mailyard, Copyright (c) 2021-2026 YouSysAdmin

package auth

import "testing"

// An address is under the lock only once it has itself failed a
// password for that account.
func TestTheLockIsHeldAgainstTheAddressesThatEarnedIt(t *testing.T) {
	h := &Handler{}
	if h.failedFrom("u1", "203.0.113.5") {
		t.Fatal("an address that never failed is under the lock")
	}

	h.chargeAddress("u1", "203.0.113.5")
	if !h.failedFrom("u1", "203.0.113.5") {
		t.Error("the address that failed is not under the lock")
	}

	if h.failedFrom("u1", "198.51.100.7") {
		t.Error("another address is under a lock it did not earn")
	}

	if h.failedFrom("u2", "203.0.113.5") {
		t.Error("the same address is under a lock for another account")
	}
}
