// Mailyard, Copyright (c) 2021-2026 YouSysAdmin

package transport

import (
	"context"

	"github.com/yousysadmin/mailyard/internal/core/smtpclient"
)

// The SMTP provider: a dial, which is what every server was before
// providers existed.
//
// Deliberately a thin wrapper and nothing more. The dial, the encryption
// modes, the failure classification and the relay-node transport all stay
// in smtpclient, where they are tested - this adds no behaviour, so the
// existing suite keeps describing reality.

type smtpTransport struct {
	cfg smtpclient.ServerConfig
}

func openSMTP(spec Spec) (Transport, error) {
	return &smtpTransport{cfg: smtpclient.ServerConfig{
		Host:         spec.Host,
		Port:         spec.Port,
		Username:     spec.Username,
		Password:     spec.Password,
		Encryption:   spec.Encryption,
		TLS:          spec.TLS,
		GuardPrivate: spec.GuardPrivate,
	}}, nil
}

// Send takes no context, because smtpclient.Send has no hook for one -
// net/smtp is synchronous with its own timeouts. Accepting one here and
// ignoring it is honest about the interface and dishonest about this
// implementation, so the parameter is named and dropped rather than
// wrapped in a goroutine that would leak a connection on cancellation.
func (t *smtpTransport) Send(_ context.Context, msg *smtpclient.Message) error {
	return smtpclient.Send(t.cfg, msg)
}

// Test probes the configuration without sending mail.
func (t *smtpTransport) Test(_ context.Context) error {
	return smtpclient.TestConnection(t.cfg)
}

func smtpDescriptor() Descriptor {
	return Descriptor{
		ID:    ProviderSMTP,
		Label: "SMTP",
		Dial:  true,
		CredentialHint: "The SMTP login. Leave both empty for a server that " +
			"authenticates by IP address.",
	}
}
