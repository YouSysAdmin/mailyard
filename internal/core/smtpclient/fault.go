// Mailyard, Copyright (c) 2021-2026 YouSysAdmin

package smtpclient

import (
	"crypto/tls"
	"crypto/x509"
	"errors"
	"net/textproto"
)

// ConfigError is a failure of the configured server itself - its login
// was refused or its certificate does not verify - rather than of the
// message. Retrying the same server cannot help until somebody changes
// its settings, while a different server can still carry the message.
type ConfigError struct {
	Err error
}

// Error renders the failure for a log or a caller.
func (e *ConfigError) Error() string { return e.Err.Error() }

// Unwrap returns the underlying error, for errors.Is and errors.As.
func (e *ConfigError) Unwrap() error { return e.Err }

// ServerFault reports that the server's configuration is at fault.
func (e *ConfigError) ServerFault() bool { return true }

// authFault reports whether an AUTH failure is a definitive refusal:
// 535 bad credentials, 534 mechanism too weak, 538 encryption
// required. A 454 is a temporary failure and stays retryable.
func authFault(err error) bool {
	t, ok := errors.AsType[*textproto.Error](err)
	if !ok {
		return false
	}

	return t.Code == 535 || t.Code == 534 || t.Code == 538
}

// tlsFault reports whether a handshake failed on certificate
// verification, as opposed to the network.
func tlsFault(err error) bool {
	if _, ok := errors.AsType[*tls.CertificateVerificationError](err); ok {
		return true
	}

	if _, ok := errors.AsType[x509.UnknownAuthorityError](err); ok {
		return true
	}

	if _, ok := errors.AsType[x509.HostnameError](err); ok {
		return true
	}

	_, ok := errors.AsType[x509.CertificateInvalidError](err)

	return ok
}

// classifyTLS wraps a handshake failure in ConfigError when the
// certificate is what failed.
func classifyTLS(err error) error {
	if tlsFault(err) {
		return &ConfigError{Err: err}
	}

	return err
}

// classifyAuth wraps an AUTH failure in ConfigError when the server
// refused the credentials for good.
func classifyAuth(err error) error {
	if authFault(err) {
		return &ConfigError{Err: err}
	}

	return err
}
