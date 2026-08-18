// Mailyard, Copyright (c) 2021-2026 YouSysAdmin

package database

import (
	"database/sql"
	"testing"
)

// The handles are never dialed here. reader() picks one, and which
// pointer it picked is the whole question - a distinct *sql.DB per
// slot is enough to answer it without a database.
func handle() *sql.DB { return &sql.DB{} }

// With nothing configured every read still goes to the primary, so
// adding the Read* helpers to a store changes nothing until an
// operator actually configures a follower.
func TestWithNoReplicasReadsStayOnThePrimary(t *testing.T) {
	primary := handle()
	b := NewBase(primary)

	if b.HasReplicas() {
		t.Error("a base built with no replicas reports having some")
	}

	for range 5 {
		if b.reader() != primary {
			t.Fatal("a read went somewhere other than the primary")
		}
	}
}

// The property that makes several followers worth configuring.
func TestReadsRoundRobinAcrossReplicas(t *testing.T) {
	primary := handle()
	a, c, d := handle(), handle(), handle()
	b := NewBase(primary, a, c, d)

	if !b.HasReplicas() {
		t.Fatal("replicas were configured but not reported")
	}

	counts := map[*sql.DB]int{}
	for range 300 {
		counts[b.reader()]++
	}

	if counts[primary] != 0 {
		t.Errorf("%d reads went to the primary while followers were configured", counts[primary])
	}

	for _, r := range []*sql.DB{a, c, d} {
		if counts[r] != 100 {
			t.Errorf("a follower took %d of 300 reads, want an even 100 - the round robin is uneven", counts[r])
		}
	}
}

// Writes are not a routing decision. Whatever is configured, Exec and
// the transaction handle go to the primary, because a follower is
// read-only and because a write that silently landed elsewhere is the
// failure this whole design exists to make impossible.
func TestWritesAlwaysGoToThePrimary(t *testing.T) {
	primary := handle()
	b := NewBase(primary, handle(), handle())

	if b.DB() != primary {
		t.Error("DB() returned something other than the primary, so transactions would run on a follower")
	}
}

// One follower needs no counter, and the branch that skips it is
// worth a test because it is the common deployment.
func TestASingleReplicaTakesEveryRead(t *testing.T) {
	only := handle()
	b := NewBase(handle(), only)
	for range 5 {
		if b.reader() != only {
			t.Fatal("a read missed the only configured follower")
		}
	}
}
