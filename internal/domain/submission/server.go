// Mailyard, Copyright (c) 2021-2026 YouSysAdmin

package submission

import (
	"crypto/tls"
	"time"

	"github.com/emersion/go-smtp"
)

// NewServer builds the go-smtp server around a Backend. With a TLS
// config the listener offers STARTTLS and refuses AUTH on plaintext
// connections. Without one AllowInsecureAuth is required - AUTH PLAIN
// over cleartext - which is fine behind a TLS-terminating proxy or on
// a trusted network, and is the operator's call.
func NewServer(b *Backend, addr, hostname string, tlsCfg *tls.Config) *smtp.Server {
	srv := smtp.NewServer(b)
	srv.Addr = addr
	srv.Domain = hostname
	srv.ReadTimeout = 30 * time.Second
	srv.WriteTimeout = 30 * time.Second
	srv.MaxMessageBytes = b.MaxMessageSize
	srv.MaxRecipients = 100
	srv.EnableSMTPUTF8 = true

	if tlsCfg != nil {
		srv.TLSConfig = tlsCfg
	} else {
		srv.AllowInsecureAuth = true
	}

	return srv
}
