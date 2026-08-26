// Mailyard, Copyright (c) 2021-2026 YouSysAdmin

package smtpserver

import "testing"

// The private-address guard is decided from tenancy, in Spec, and only
// a project's own server gets it: the shared pool (no project) and a
// relay node were placed by an operator and are often on this network.
func TestOnlyAProjectServerIsGuardedAgainstPrivateAddresses(t *testing.T) {
	project := &Server{ProjectID: "p1", Host: "smtp.example.com", Port: 587}
	if !project.Spec(nil).GuardPrivate {
		t.Error("a project's server is not guarded, so its members can scan this network")
	}

	shared := &Server{Host: "10.0.0.5", Port: 25}
	if shared.Spec(nil).GuardPrivate {
		t.Error("a shared server is guarded, so an operator's own relay is refused")
	}

	node := &Server{ProjectID: "p1", NodeID: "n1", Host: "10.0.0.9", Port: 2525}
	if node.Spec(nil).GuardPrivate {
		t.Error("a relay node is guarded, so a node on this network cannot be reached")
	}
}
