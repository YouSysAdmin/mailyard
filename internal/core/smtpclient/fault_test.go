// Mailyard, Copyright (c) 2021-2026 YouSysAdmin

package smtpclient

import (
	"bufio"
	"crypto/tls"
	"crypto/x509"
	"errors"
	"fmt"
	"io"
	"log"
	"net"
	"net/http"
	"net/http/httptest"
	"net/textproto"
	"strings"
	"testing"
)

// A server is taken out of rotation only on a refusal that cannot
// change by itself. A 454 or a dead network is retried as before.
func TestConfigFaultsAreTheServersOwn(t *testing.T) {
	for name, tc := range map[string]struct {
		err   error
		fault bool
	}{
		"bad credentials":     {classifyAuth(fmt.Errorf("smtp auth failed: %w", &textproto.Error{Code: 535})), true},
		"mechanism too weak":  {classifyAuth(&textproto.Error{Code: 534}), true},
		"encryption required": {classifyAuth(&textproto.Error{Code: 538}), true},
		"auth temporarily":    {classifyAuth(&textproto.Error{Code: 454}), false},
		"auth not an answer":  {classifyAuth(errors.New("EOF")), false},
		"unknown authority":   {classifyTLS(fmt.Errorf("starttls failed: %w", x509.UnknownAuthorityError{})), true},
		"wrong hostname":      {classifyTLS(x509.HostnameError{Host: "mail.example.com"}), true},
		"expired":             {classifyTLS(x509.CertificateInvalidError{Reason: x509.Expired}), true},
		"verification":        {classifyTLS(&tls.CertificateVerificationError{Err: errors.New("x")}), true},
		"connection refused":  {classifyTLS(&net.OpError{Op: "dial", Err: errors.New("refused")}), false},
	} {
		if _, got := errors.AsType[*ConfigError](tc.err); got != tc.fault {
			t.Errorf("%s: ConfigError = %v, want %v", name, got, tc.fault)
		}
	}
}

// authSMTP is a server that offers AUTH and answers it with code.
func authSMTP(t *testing.T, code string) int {
	t.Helper()

	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}

	t.Cleanup(func() { _ = ln.Close() })
	go func() {
		conn, err := ln.Accept()
		if err != nil {
			return
		}

		defer func() { _ = conn.Close() }()
		r := bufio.NewReader(conn)
		_, _ = conn.Write([]byte("220 fake ESMTP\r\n"))
		for {
			line, err := r.ReadString('\n')
			if err != nil {
				return
			}

			switch {
			case strings.HasPrefix(line, "EHLO"):
				_, _ = conn.Write([]byte("250-fake\r\n250 AUTH PLAIN\r\n"))
			case strings.HasPrefix(line, "AUTH"):
				_, _ = conn.Write([]byte(code + " refused\r\n"))
			default:
				_, _ = conn.Write([]byte("221 bye\r\n"))

				return
			}
		}
	}()

	return ln.Addr().(*net.TCPAddr).Port
}

// Through the real net/smtp client, not a hand-built error: a refused
// login comes back as a ConfigError, a temporary one does not.
func TestARealAuthRefusalIsAConfigError(t *testing.T) {
	msg := &Message{From: "s@example.com", To: []string{"r@example.com"}, Subject: "s", Text: "b"}
	for code, fault := range map[string]bool{"535": true, "454": false} {
		cfg := ServerConfig{
			Host: "127.0.0.1", Port: authSMTP(t, code), Encryption: EncryptionNone,
			Username: "u", Password: "p",
		}

		err := Send(t.Context(), cfg, msg)
		if _, got := errors.AsType[*ConfigError](err); got != fault {
			t.Errorf("%s: ConfigError = %v, want %v (err %v)", code, got, fault, err)
		}
	}
}

// A certificate nobody vouches for is the server's configuration.
func TestAnUntrustedCertificateIsAConfigError(t *testing.T) {
	srv := httptest.NewUnstartedServer(http.NotFoundHandler())
	srv.Config.ErrorLog = log.New(io.Discard, "", 0)
	srv.StartTLS()
	t.Cleanup(srv.Close)

	port := srv.Listener.Addr().(*net.TCPAddr).Port
	cfg := ServerConfig{Host: "127.0.0.1", Port: port, Encryption: EncryptionSSL}

	err := Send(t.Context(), cfg, &Message{From: "s@example.com", To: []string{"r@example.com"}, Text: "b"})
	if _, ok := errors.AsType[*ConfigError](err); !ok {
		t.Fatalf("err %v (%T), want a ConfigError", err, err)
	}
}
