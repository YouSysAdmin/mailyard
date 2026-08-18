// Mailyard, Copyright (c) 2021-2026 YouSysAdmin

package certificate

import (
	"crypto"
	"crypto/ecdsa"
	"crypto/ed25519"
	"crypto/rsa"
	"crypto/sha256"
	"crypto/x509"
	"encoding/hex"
	"encoding/pem"
	"errors"
	"fmt"
	"strings"
	"time"
)

// ScopeManaged holds a certificate an administrator put there: a pair
// they uploaded, or one they asked us to generate. Name is theirs.
//
// Kept apart from ScopeSelfSigned, which is the pair the server mints
// for itself when tls.mode is self and nobody chose anything. One is
// configuration somebody wrote and expects to find again, the other is
// a default that may be regenerated - deleting the second is
// housekeeping, deleting the first loses what they uploaded.
const ScopeManaged = "managed"

// Details is what a certificate says about itself, for display.
// Derived from the public half, so producing it never involves the
// encryption key.
type Details struct {
	Subject   string    `json:"subject"`
	Issuer    string    `json:"issuer"`
	DNSNames  []string  `json:"dns_names,omitempty"`
	NotBefore time.Time `json:"not_before"`
	NotAfter  time.Time `json:"not_after"`
	Serial    string    `json:"serial"`

	// Fingerprint is SHA-256 over the DER, the form every other tool
	// prints, so an operator can compare it with openssl without
	// converting anything.
	Fingerprint string `json:"fingerprint"`
	KeyAlgo     string `json:"key_algorithm"`
	SelfSigned  bool   `json:"self_signed"`

	// IsCA marks a certificate authority.
	//
	// It is what tells an authority apart from a server certificate in
	// a table that holds both, and it is load-bearing rather than
	// decorative: a CA carries no subject alt name and no ServerAuth,
	// so a listener assigned one refuses every client. Three places
	// read this - the assignment check, the resolver's fallback, and
	// the console, which must not offer an authority for assignment.
	IsCA bool `json:"is_ca"`

	// SubjectKeyID and AuthorityKeyID answer "which stored authority
	// signed this", which Issuer cannot: that is a distinguished name,
	// and two authorities may carry the same one. Go fills both in
	// automatically, so this needs no column of its own.
	SubjectKeyID   string `json:"subject_key_id,omitempty"`
	AuthorityKeyID string `json:"authority_key_id,omitempty"`

	// Chain counts the certificates in the file. One means the leaf
	// alone, which is the usual cause of a chain error that only some
	// clients see - a browser fills the gap from its own cache and an
	// SMTP peer does not.
	Chain int `json:"chain_length"`
}

// ExpiresIn is how long the certificate has left, negative once it
// has run out.
func (d Details) ExpiresIn() time.Duration { return time.Until(d.NotAfter) }

// ParseDetails reads the leaf out of a PEM bundle.
func ParseDetails(certPEM string) (*Details, error) {
	leaf, count, err := leafOf(certPEM)
	if err != nil {
		return nil, err
	}

	sum := sha256.Sum256(leaf.Raw)

	return &Details{
		Subject:        leaf.Subject.String(),
		Issuer:         leaf.Issuer.String(),
		DNSNames:       leaf.DNSNames,
		NotBefore:      leaf.NotBefore,
		NotAfter:       leaf.NotAfter,
		Serial:         leaf.SerialNumber.String(),
		Fingerprint:    colonHex(sum[:]),
		KeyAlgo:        keyAlgorithm(leaf),
		SelfSigned:     leaf.Subject.String() == leaf.Issuer.String(),
		IsCA:           leaf.IsCA,
		SubjectKeyID:   colonHex(leaf.SubjectKeyId),
		AuthorityKeyID: colonHex(leaf.AuthorityKeyId),
		Chain:          count,
	}, nil
}

// VerifyPair checks that the key belongs to the certificate before
// anything stores them together.
//
// The pair is what a listener will present, and a mismatch is not
// discovered until a handshake - by which time the listener is up, the
// upload looked like it worked, and the only symptom is every client
// failing. Refusing at the door costs one comparison.
func VerifyPair(certPEM, keyPEM string) error {
	leaf, _, err := leafOf(certPEM)
	if err != nil {
		return err
	}

	block, _ := pem.Decode([]byte(keyPEM))
	if block == nil {
		return errors.New("the private key is not PEM")
	}

	key, err := parsePrivateKey(block.Bytes)
	if err != nil {
		return err
	}

	// crypto.PublicKey, not any. It is a DEFINED type rather than an
	// alias, so a method declared Equal(crypto.PublicKey) does not
	// satisfy an interface asking for Equal(any) - every standard
	// library key type failed the assertion and the check reported
	// every pair as an unsupported type.
	type publicMatcher interface{ Equal(x crypto.PublicKey) bool }
	pub, ok := leaf.PublicKey.(publicMatcher)
	if !ok {
		return fmt.Errorf("unsupported public key type %T", leaf.PublicKey)
	}

	signer, ok := key.(crypto.Signer)
	if !ok {
		return errors.New("unsupported private key type")
	}

	if !pub.Equal(signer.Public()) {
		return errors.New("the private key does not match the certificate")
	}

	return nil
}

func parsePrivateKey(der []byte) (any, error) {
	if k, err := x509.ParsePKCS8PrivateKey(der); err == nil {
		return k, nil
	}

	if k, err := x509.ParsePKCS1PrivateKey(der); err == nil {
		return k, nil
	}

	if k, err := x509.ParseECPrivateKey(der); err == nil {
		return k, nil
	}

	return nil, errors.New("the private key is not PKCS#8, PKCS#1 or SEC 1")
}

// leafOf returns the first certificate in the bundle and how many
// there were.
func leafOf(certPEM string) (*x509.Certificate, int, error) {
	var leaf *x509.Certificate
	count := 0
	rest := []byte(certPEM)
	for {
		var block *pem.Block
		block, rest = pem.Decode(rest)
		if block == nil {
			break
		}

		if block.Type != "CERTIFICATE" {
			continue
		}

		count++
		if leaf != nil {
			continue
		}

		c, err := x509.ParseCertificate(block.Bytes)
		if err != nil {
			return nil, 0, fmt.Errorf("parsing the certificate: %w", err)
		}

		leaf = c
	}

	if leaf == nil {
		return nil, 0, errors.New("no certificate found in the PEM")
	}

	return leaf, count, nil
}

func keyAlgorithm(c *x509.Certificate) string {
	switch k := c.PublicKey.(type) {
	case *rsa.PublicKey:
		return fmt.Sprintf("RSA %d", k.N.BitLen())
	case *ecdsa.PublicKey:
		return "ECDSA " + k.Curve.Params().Name
	case ed25519.PublicKey:
		return "Ed25519"
	default:
		return c.PublicKeyAlgorithm.String()
	}
}

func colonHex(b []byte) string {
	s := strings.ToUpper(hex.EncodeToString(b))
	var out strings.Builder
	for i := 0; i < len(s); i += 2 {
		if i > 0 {
			out.WriteByte(':')
		}

		out.WriteString(s[i : i+2])
	}

	return out.String()
}
