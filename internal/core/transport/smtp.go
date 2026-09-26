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

// Send bounds the conversation by ctx. net/smtp has no timeouts of
// its own, so the deadline and the idle cut live on the connection -
// see smtpclient.IdleTimeout.
func (t *smtpTransport) Send(ctx context.Context, msg *smtpclient.Message) error {
	return smtpclient.Send(ctx, t.cfg, msg)
}

// Test probes the configuration without sending mail.
func (t *smtpTransport) Test(ctx context.Context) error {
	return smtpclient.TestConnection(ctx, t.cfg)
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
