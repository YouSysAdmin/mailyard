// Mailyard, Copyright (c) 2021-2026 YouSysAdmin

// Package smtpclient is the outbound SMTP transport: hand-built MIME
// messages delivered over plain, STARTTLS, or implicit-TLS
// connections. It is deliberately crypto-free and model-free - the
// caller passes a plain ServerConfig with the password already
// decrypted.
package smtpclient

import (
	"crypto/tls"
	"errors"
	"fmt"
	"net/mail"
	"net/smtp"
	"net/textproto"
	"strings"
)

// Encryption modes for ServerConfig.Encryption.
const (
	EncryptionNone     = "none"
	EncryptionSTARTTLS = "starttls"
	EncryptionSSL      = "ssl"
)

// ServerConfig is everything needed to reach one SMTP server.
type ServerConfig struct {
	Host       string
	Port       int
	Username   string
	Password   string
	Encryption string

	// TLS overrides the connection's TLS settings.
	//
	// Set for a relay node, where the transport IS the
	// authentication: the config carries our client certificate and
	// the private CA to verify the node against, and Username stays
	// empty so no AUTH is attempted. Nil everywhere else, which keeps
	// the ordinary dial exactly as it was.
	TLS *tls.Config
}

func (c ServerConfig) addr() string { return fmt.Sprintf("%s:%d", c.Host, c.Port) }

func (c ServerConfig) auth() smtp.Auth {
	if c.Username == "" {
		return nil
	}

	return smtp.PlainAuth("", c.Username, c.Password, c.Host)
}

// SendError carries structured information about an SMTP failure so
// callers can distinguish a permanent recipient rejection (5xx at
// RCPT TO) from a transient error worth retrying. Error() preserves
// the original human-readable string.
type SendError struct {
	Stage     string // "RCPT TO", "MAIL FROM", "DATA", ...
	Recipient string // bare recipient address, set for RCPT TO failures
	Code      int    // SMTP reply code (e.g. 550), 0 when not an SMTP reply
	Msg       string // server reply text, incl. enhanced status code
	Err       error
}

// Permanent reports whether the failure is a permanent (5xx) SMTP
// rejection.
func (e *SendError) Permanent() bool { return e.Code >= 500 && e.Code < 600 }

// RejectedRecipient is the address the server refused, empty when it
// refused the message without naming one.
//
// ONLY at the RCPT TO stage, and that rule lives here rather than at the
// caller because it is a fact about SMTP. Recipient is filled on every
// stage for the error message, and on MAIL FROM it holds the SENDER - so
// acting on it there would record a bounce against, and suppress, the
// project's own sender or bounce address the first time a server answers
// 5xx to the envelope. The suppression list doubles as an inbound
// blocklist, so that mistake is not confined to sending.
//
// The rule lives here so no caller has to remember an SMTP stage name to
// use the result safely.
func (e *SendError) RejectedRecipient() string {
	if e.Stage != "RCPT TO" {
		return ""
	}

	return e.Recipient
}

// Error renders the failure for a log or a caller.
func (e *SendError) Error() string { return e.Err.Error() }

// Unwrap returns the underlying error, for errors.Is and errors.As.
func (e *SendError) Unwrap() error { return e.Err }

// smtpReply extracts the SMTP reply code and message from an error
// returned by the net/smtp client. Returns (0, "") when err does not
// wrap a *textproto.Error (e.g. a connection-level failure).
func smtpReply(err error) (int, string) {
	if t, ok := errors.AsType[*textproto.Error](err); ok {
		return t.Code, t.Msg
	}

	return 0, ""
}

// wrapSendError turns a stage failure into a *SendError carrying the
// SMTP reply code and recipient, preserving the original error string.
func wrapSendError(stage, recipient string, err error) error {
	code, msg := smtpReply(err)

	return &SendError{Stage: stage, Recipient: recipient, Code: code, Msg: msg, Err: err}
}

// TestConnection verifies that the server is reachable and, when
// credentials are configured, that authentication succeeds.
func TestConnection(cfg ServerConfig) error {
	client, err := dial(cfg)
	if err != nil {
		return err
	}

	defer func() { _ = client.Close() }()

	if cfg.Username != "" {
		if err := client.Auth(cfg.auth()); err != nil {
			return fmt.Errorf("smtp auth failed: %w", err)
		}
	}

	return client.Quit()
}

// Send delivers msg through the server. Returns a *SendError for
// stage failures so the caller can branch on Permanent().
func Send(cfg ServerConfig, msg *Message) error {
	client, err := dial(cfg)
	if err != nil {
		return err
	}

	defer func() { _ = client.Close() }()

	return sendViaClient(client, cfg.auth(), msg)
}

// dial opens the connection in the configured encryption mode and
// returns a ready SMTP client.
func dial(cfg ServerConfig) (*smtp.Client, error) {
	tlsConfig := cfg.TLS
	if tlsConfig == nil {
		tlsConfig = &tls.Config{ServerName: cfg.Host}
	}

	switch cfg.Encryption {
	case EncryptionSSL:
		conn, err := tls.Dial("tcp", cfg.addr(), tlsConfig)
		if err != nil {
			return nil, fmt.Errorf("ssl dial failed: %w", err)
		}

		client, err := smtp.NewClient(conn, cfg.Host)
		if err != nil {
			_ = conn.Close()

			return nil, fmt.Errorf("smtp client creation failed: %w", err)
		}

		return client, nil
	case EncryptionSTARTTLS:
		client, err := smtp.Dial(cfg.addr())
		if err != nil {
			return nil, fmt.Errorf("smtp dial failed: %w", err)
		}

		if err := client.StartTLS(tlsConfig); err != nil {
			_ = client.Close()

			return nil, fmt.Errorf("starttls failed: %w", err)
		}

		return client, nil
	default:
		client, err := smtp.Dial(cfg.addr())
		if err != nil {
			return nil, fmt.Errorf("smtp dial failed: %w", err)
		}

		return client, nil
	}
}

func sendViaClient(client *smtp.Client, auth smtp.Auth, msg *Message) error {
	if auth != nil {
		if err := client.Auth(auth); err != nil {
			return fmt.Errorf("smtp auth failed: %w", err)
		}
	}

	envFrom := msg.From
	if msg.EnvelopeFrom != "" {
		envFrom = msg.EnvelopeFrom
	}

	if err := client.Mail(EnvelopeAddress(envFrom)); err != nil {
		return wrapSendError("MAIL FROM", envFrom, fmt.Errorf("smtp mail from failed: %w", err))
	}

	for _, addr := range msg.To {
		rcpt := EnvelopeAddress(addr)
		if err := client.Rcpt(rcpt); err != nil {
			return wrapSendError("RCPT TO", rcpt, fmt.Errorf("smtp rcpt to failed: %w", err))
		}
	}

	raw := msg.Build()
	if msg.Sign != nil {
		signed, serr := msg.Sign(raw)
		if serr != nil {
			// Refuse rather than fall back to sending unsigned. A
			// message that silently loses its signature reaches the
			// recipient's spam folder and nothing in the delivery log
			// explains why, which is a far worse failure than a visible
			// retry.
			return fmt.Errorf("signing failed: %w", serr)
		}

		raw = signed
	}

	w, err := client.Data()
	if err != nil {
		return wrapSendError("DATA", "", fmt.Errorf("smtp data failed: %w", err))
	}

	if _, err := w.Write(raw); err != nil {
		return fmt.Errorf("smtp write failed: %w", err)
	}

	if err := w.Close(); err != nil {
		return wrapSendError("DATA", "", fmt.Errorf("smtp close failed: %w", err))
	}

	return client.Quit()
}

// FormatAddress renders a display name and an address as one RFC 5322
// mailbox, the way a From header wants it.
//
// Through mail.Address.String(), never Sprintf("%s <%s>"), because the
// name is data:
//
//   - `Faria, Inc.` produces `Faria, Inc. <a@b>`, which is two addresses
//     to a parser - the comma is the list separator - so the header is
//     either rejected or read as mail from somebody called Faria.
//   - a name with a quote or a backslash needs escaping inside the quotes.
//   - a non-ASCII name needs RFC 2047 encoding, and a raw UTF-8 byte in a
//     header is not something a receiver has to accept. Subject has been
//     encoded here since the beginning - the From name was not, in the
//     two places that composed one by hand.
//
// An empty name returns the bare address, which is what a From header
// with nothing to say looks like.
func FormatAddress(name, addr string) string {
	name = strings.TrimSpace(name)
	addr = strings.TrimSpace(addr)
	if name == "" {
		return addr
	}

	return (&mail.Address{Name: name, Address: addr}).String()
}

// EnvelopeAddress extracts the bare email from an RFC 5322 address
// like "Display Name <user@example.com>". If parsing fails the input
// is returned unchanged.
func EnvelopeAddress(from string) string {
	addr, err := mail.ParseAddress(from)
	if err != nil {
		return from
	}

	return addr.Address
}
