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

// An IPv6 subscriber is one address however it rotates through its
// /64, and a locked account admits only so many fresh addresses per
// window.
func TestTheLockCannotBeWalkedPastByRotatingAddresses(t *testing.T) {
	h := &Handler{}
	h.chargeAddress("u1", "2001:db8:1:2::1")
	if !h.failedFrom("u1", "2001:db8:1:2:ffff::9") {
		t.Error("another address in the same /64 is not under the lock")
	}

	if h.failedFrom("u1", "2001:db8:1:3::1") {
		t.Error("the next /64 is under a lock it did not earn")
	}

	if !h.failedFrom("u1", "2001:db8:1:2::1") || addressKey("::ffff:203.0.113.5") != "203.0.113.5" {
		t.Error("addresses are not keyed the way they were charged")
	}

	for i := range loginMaxFailures {
		if !h.admitWhileLocked("u1") {
			t.Fatalf("fresh address %d refused inside the allowance", i+1)
		}
	}

	if h.admitWhileLocked("u1") {
		t.Error("a locked account admitted more fresh addresses than its allowance")
	}

	if !h.admitWhileLocked("u2") {
		t.Error("one account's allowance was spent by another's")
	}
}

// The mail budgets judge an address the way the lock does: one IPv6
// subscriber spends one budget however it rotates through its /64, and
// the account's own ceiling holds over every address.
func TestTheMailBudgetJudgesIPv6ByItsSlash64(t *testing.T) {
	spent := []string{"2001:db8:1:2::1", "2001:db8:1:2::2", "2001:db8:1:2:ffff::3"}
	if !budgetSpent(spent, "2001:db8:1:2::99", 3, 15) {
		t.Error("a fresh address in the same /64 was given a budget of its own")
	}

	if budgetSpent(spent, "2001:db8:1:3::1", 3, 15) {
		t.Error("the next /64 was charged for another's requests")
	}

	if budgetSpent([]string{"203.0.113.5", "203.0.113.5"}, "203.0.113.5", 3, 15) {
		t.Error("an address under its budget was refused")
	}

	if !budgetSpent(spent, "198.51.100.7", 3, 3) {
		t.Error("the account ceiling did not hold for a fresh address")
	}
}
