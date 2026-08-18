// Mailyard, Copyright (c) 2021-2026 YouSysAdmin

package env

import (
	"testing"

	"github.com/spf13/viper"
)

// TLSHost is what a generated self-signed pair is named after. Same
// source as the ACME list, so the two cannot disagree about what this
// installation is called.
func TestTLSHostComesFromThePublicURL(t *testing.T) {
	c := &Config{}
	c.Server.PublicURL = "https://mail.example.com:8443"
	if got := c.TLSHost(); got != "mail.example.com" {
		t.Errorf("TLSHost() = %q, want mail.example.com", got)
	}

	if got := (&Config{}).TLSHost(); got != "" {
		t.Errorf("TLSHost() with no public url = %q, want empty", got)
	}
}

// The two SMTP listeners terminate TLS by default and the HTTP one does
// not. Not an inconsistency - see TLSConfig.Enabled. Pinned here because
// flipping the HTTP default is a one-word change that breaks every
// reverse-proxy deployment and stops every relay node enrolling, and
// neither failure names TLS when it happens.
func TestTLSDefaults(t *testing.T) {
	c, err := Load("")
	if err != nil {
		t.Fatalf("Load: %v", err)
	}

	if c.Server.TLS.Enabled {
		t.Error("server.tls.enabled defaults on - a reverse proxy talking HTTP upstream would break, and a relay node verifies our certificate with the system trust store")
	}

	if !c.Submission.TLS.Enabled {
		t.Error("submission.tls.enabled defaults off - AUTH would run over cleartext")
	}

	if !c.Inbound.TLS.Enabled {
		t.Error("inbound.tls.enabled defaults off - opportunistic STARTTLS costs nothing")
	}

	// empty, not ":80". tls-alpn-01 needs no port, so binding a
	// privileged one by default would take it from every installation
	// for a challenge type most never use.
	if c.ACME.ChallengeAddr != "" {
		t.Errorf("acme.challenge_addr = %q, want empty", c.ACME.ChallengeAddr)
	}
}

// A key that parses and does nothing is worse than one that is refused:
// it looks like working configuration. An operator upgrading has a
// tls.mode in a file, and silence would let them believe it is in force.
func TestRemovedTLSKeysAreNamed(t *testing.T) {
	v := viper.New()
	if got := removedKeysIn(v); len(got) != 0 {
		t.Errorf("a bare config reported %v", got)
	}

	v.Set("server.tls.mode", "self")
	v.Set("submission.tls.cert", "/etc/ssl/mail.pem")
	v.Set("inbound.tls.acme.hosts", []string{"mx.example.com"})

	want := map[string]bool{
		"server.tls.mode":        true,
		"submission.tls.cert":    true,
		"inbound.tls.acme.hosts": true,
	}
	got := removedKeysIn(v)
	if len(got) != len(want) {
		t.Fatalf("removedKeysIn() = %v, want the three that were set", got)
	}

	for _, key := range got {
		if !want[key] {
			t.Errorf("removedKeysIn() reported %q, which was not set", key)
		}
	}
}

// TestReplicaReadDefaultsMatchTheRisk pins the one decision in
// ReplicaReadsConfig that is a judgement rather than a mechanism.
//
// Each default answers "does a console page write this list and then
// immediately re-read it". Only the suppressions page does - it adds
// and removes blocks and reloads on the spot - so it is the only
// group that starts off. Flipping any of these is a decision about
// what a person will see when a follower is behind, and it should
// take editing a test that says so.
func TestReplicaReadDefaultsMatchTheRisk(t *testing.T) {
	dir := t.TempDir()
	t.Chdir(dir)
	t.Setenv("MAILYARD_DATABASE_DSN", "postgres://x@localhost/x")
	t.Setenv("MAILYARD_AUTH_JWT_SECRET", "0123456789abcdef0123456789abcdef")
	t.Setenv("MAILYARD_CRYPTO_ENCRYPTION_KEY", "0123456789abcdef0123456789abcdef")

	cfg, err := Load("")
	if err != nil {
		t.Fatalf("load: %v", err)
	}

	r := cfg.Database.ReplicaReads

	on := map[string]bool{
		"analytics":          r.Analytics,
		"email_log":          r.EmailLog,
		"inbound_log":        r.InboundLog,
		"sandbox":            r.Sandbox,
		"bounces":            r.Bounces,
		"contacts":           r.Contacts,
		"webhook_deliveries": r.WebhookDeliveries,
		"audit_log":          r.AuditLog,
	}
	for name, v := range on {
		if !v {
			t.Errorf("replica_reads.%s defaults off - no console page writes that list "+
				"and re-reads it, so a follower cannot show anybody a stale answer there", name)
		}
	}

	if r.Suppressions {
		t.Error("replica_reads.suppressions defaults ON. The suppressions page adds and " +
			"removes blocks and reloads the list immediately, so on a lagging follower the " +
			"address just blocked is missing from the answer - which is the whole question " +
			"that page exists to answer")
	}

	if !r.Any() {
		t.Error("Any() reports no group enabled, which contradicts the defaults above")
	}
}

// The key a listener still has must not be in the removed list, or an
// operator reads a warning about the one line they got right.
func TestTheSurvivingKeysAreNotReportedAsRemoved(t *testing.T) {
	v := viper.New()
	v.Set("server.tls.enabled", true)
	v.Set("submission.tls.enabled", true)
	// The only acme key left in the file. Whether ACME is on, and which
	// hosts, are platform settings now - so `acme.enabled` IS a removed
	// key, and this test asserting otherwise is how that would have gone
	// unnoticed.
	v.Set("acme.challenge_addr", ":80")

	if got := removedKeysIn(v); len(got) != 0 {
		t.Errorf("removedKeysIn() reported %v for keys that still work", got)
	}
}
