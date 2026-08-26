// Mailyard, Copyright (c) 2021-2026 YouSysAdmin

package smtpclient

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/yousysadmin/mailyard/internal/core/safedial"
)

// A guarded ServerConfig refuses a private host at the dial, in every
// encryption mode, before any byte is exchanged - so the connection
// test cannot be used to learn which ports on this network answer.
func TestAGuardedServerRefusesAPrivateHost(t *testing.T) {
	for _, enc := range []string{EncryptionNone, EncryptionSTARTTLS, EncryptionSSL} {
		cfg := ServerConfig{Host: "127.0.0.1", Port: 25, Encryption: enc, GuardPrivate: true}
		var blocked *safedial.ErrBlocked
		if err := TestConnection(cfg); !errors.As(err, &blocked) {
			t.Errorf("%s: got %v, want ErrBlocked", enc, err)
		}
	}
}

// The direct MX path takes the same guard: an MX record is the recipient
// domain owner's word, and a guarded config refuses one that resolves
// into this network before any byte is exchanged.
func TestAGuardedDirectDialRefusesAPrivateHost(t *testing.T) {
	cfg := DirectConfig{GuardPrivate: true, Timeout: time.Second}
	var blocked *safedial.ErrBlocked
	if _, err := cfg.dial(context.Background(), "127.0.0.1:25"); !errors.As(err, &blocked) {
		t.Fatalf("got %v, want ErrBlocked", err)
	}
}
