// Mailyard, Copyright (c) 2021-2026 YouSysAdmin

package relayagent

import (
	"crypto/tls"
	"fmt"
	"time"

	"github.com/yousysadmin/mailyard/internal/core/certgen"
)

// Meta keys for the MX listener's own pair. In the spool, not on the
// disk beside it, for the same reason the node's identity is: a node
// is one directory.
const (
	metaMXCert = "mx_cert_pem"
	metaMXKey  = "mx_key_pem"
)

// mxRenewBefore regenerates a self-signed pair before it expires.
//
// An expired certificate on an opportunistic-TLS listener does not
// refuse anything: most senders do not verify. It just means the one
// thing this pair is good for - not sending mail in the clear across
// the internet - quietly stops on a machine nobody is watching.
const mxRenewBefore = 30 * 24 * time.Hour

// mxValidity is a year, comfortably longer than mxRenewBefore so a
// node regenerates on its own schedule rather than at every restart.
const mxValidity = 365 * 24 * time.Hour

// ReceiveTLS builds the STARTTLS config for the MX listener.
//
// An operator's own pair when they named one, otherwise a self-signed
// pair this node generates once and keeps. Never an error that stops
// the node: a listener with no TLS still receives mail, and refusing
// to start would take the MX down over the half of the connection
// almost no sender verifies.
func (a *Agent) ReceiveTLS(certFile, keyFile string) (*tls.Config, error) {
	if certFile != "" && keyFile != "" {
		pair, err := tls.LoadX509KeyPair(certFile, keyFile)
		if err != nil {
			return nil, fmt.Errorf("relay node inbound tls: %w", err)
		}

		return &tls.Config{Certificates: []tls.Certificate{pair}, MinVersion: tls.VersionTLS12}, nil
	}

	pair, err := a.selfSignedMX()
	if err != nil {
		return nil, err
	}

	// TLS 1.2 as the floor, unlike the worker-facing listener's 1.3.
	// That one talks only to our own processes. This one talks to
	// whatever mail server the internet points at it, and a receiver
	// that demands 1.3 does not get a downgrade - it gets the message
	// delivered in the clear instead, which is the opposite of the
	// intent.
	return &tls.Config{Certificates: []tls.Certificate{pair}, MinVersion: tls.VersionTLS12}, nil
}

func (a *Agent) selfSignedMX() (tls.Certificate, error) {
	certPEM, _ := a.spool.Meta(metaMXCert)
	keyPEM, _ := a.spool.Meta(metaMXKey)

	if certPEM != "" && keyPEM != "" {
		if crt, err := certgen.ParseCertificate(certPEM); err == nil &&
			time.Until(crt.NotAfter) > mxRenewBefore {
			if pair, perr := tls.X509KeyPair([]byte(certPEM), []byte(keyPEM)); perr == nil {
				return pair, nil
			}
		}
		// Unparseable, expiring, or not a pair. Generating a new one is
		// the only outcome that leaves the listener able to offer TLS,
		// and there is nothing here worth preserving - a self-signed
		// certificate nobody verifies has no identity to keep.
		a.log.Info("relay node: regenerating the inbound TLS certificate")
	}

	// The Subject NAMES THIS NODE: it is what every mail server
	// connecting to the MX is shown.
	newCert, newKey, err := certgen.MintLeaf(certgen.LeafRequest{
		Subject:   certgen.Subject{CommonName: a.cfg.Hostname},
		Hosts:     []string{a.cfg.Hostname},
		Algorithm: certgen.AlgECDSA,
		Validity:  mxValidity,
	}, nil)
	if err != nil {
		return tls.Certificate{}, fmt.Errorf("relay node inbound tls: %w", err)
	}
	for k, v := range map[string]string{metaMXCert: newCert, metaMXKey: newKey} {
		if serr := a.spool.SetMeta(k, v); serr != nil {
			// Not fatal. The pair in hand is perfectly good for this
			// process - it only means the next start generates another.
			a.log.Warn("relay node: could not store the inbound TLS certificate", "err", serr)
			break
		}
	}

	return tls.X509KeyPair([]byte(newCert), []byte(newKey))
}
