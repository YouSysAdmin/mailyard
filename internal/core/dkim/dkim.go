// Mailyard, Copyright (c) 2021-2026 YouSysAdmin

// Package dkim mints per-domain signing keys and signs outbound mail.
//
// Signing is not optional polish for a sending platform. Unsigned mail
// arriving from an IP the receiver has no history with is spam-foldered
// by Gmail and Microsoft as a matter of routine, and DKIM is the only
// one of the three authentication mechanisms that survives forwarding
// (SPF breaks the moment a mailing list re-sends, because the envelope
// sender changes). It is also what DMARC alignment is usually built on.
//
// One keypair per verified domain, private key sealed by
// internal/core/crypto before it reaches the database. Per domain
// rather than one key for the installation: a shared key makes every
// tenant's reputation and every tenant's compromise the same event.
package dkim

import (
	"bytes"
	"crypto"
	"crypto/rand"
	"crypto/rsa"
	"crypto/x509"
	"encoding/base64"
	"encoding/pem"
	"errors"
	"fmt"
	"io"
	"strings"

	msgdkim "github.com/emersion/go-msgauth/dkim"
)

// DefaultSelector names the DNS record holding the public key:
// <selector>._domainkey.<domain>. A constant rather than a knob
// because rotation is done by publishing a new selector alongside the
// old one, which needs a per-domain stored value, not a global
// setting - see Domain.DKIMSelector.
const DefaultSelector = "mailyard"

// keyBits is the RSA modulus size.
//
// 2048 rather than Ed25519 (RFC 8463): Ed25519 keys are shorter and
// faster, but receiver support is still patchy enough that signing
// with one alone means some receivers see no valid signature at all,
// which is worse than not signing. 1024 is still widely accepted but
// is below what current guidance considers sound, and 2048 is the
// largest that reliably fits a single DNS TXT string without the
// operator having to split it.
const keyBits = 2048

// signedHeaders is the header set covered by the signature.
//
// From is mandatory (RFC 6376). The rest are the headers a receiver
// uses to decide what the message IS - change any of them in transit
// and the signature must break. Deliberately absent: Received, and
// anything else a relay legitimately adds, since including those would
// make every ordinary hop invalidate the signature.
//
// Listing a header that is absent is allowed and is the recommended
// defence against an attacker ADDING one downstream, so this stays a
// fixed list rather than being narrowed per message.
var signedHeaders = []string{
	"From", "To", "Cc", "Subject", "Date", "Message-ID",
	"MIME-Version", "Content-Type", "Content-Transfer-Encoding",
	"List-Unsubscribe", "List-Unsubscribe-Post",
	// The bounce attribution hook. Signed for the reason in the note
	// above: naming it here is what stops somebody ADDING one in
	// transit and having a bounce filed against a message they picked.
	"X-Mailyard-Email-Id",
}

// GenerateKey mints a keypair for one domain. The private key comes
// back as PKCS#8 PEM for the caller to encrypt and store, the public
// key as the bare base64 DER that goes in the DNS record.
func GenerateKey() (privatePEM, publicBase64 string, err error) {
	key, err := rsa.GenerateKey(rand.Reader, keyBits)
	if err != nil {
		return "", "", fmt.Errorf("dkim: generate key: %w", err)
	}

	der, err := x509.MarshalPKCS8PrivateKey(key)
	if err != nil {
		return "", "", fmt.Errorf("dkim: marshal private key: %w", err)
	}

	privatePEM = string(pem.EncodeToMemory(&pem.Block{Type: "PRIVATE KEY", Bytes: der}))

	pubDER, err := x509.MarshalPKIXPublicKey(&key.PublicKey)
	if err != nil {
		return "", "", fmt.Errorf("dkim: marshal public key: %w", err)
	}

	return privatePEM, base64.StdEncoding.EncodeToString(pubDER), nil
}

// TXTHost is the DNS name the public key is published at.
func TXTHost(selector, domain string) string {
	if selector == "" {
		selector = DefaultSelector
	}

	return selector + "._domainkey." + domain
}

// TXTValue is the record contents for a public key.
func TXTValue(publicBase64 string) string {
	return "v=DKIM1; k=rsa; p=" + publicBase64
}

// Signer signs messages for one domain.
type Signer struct {
	domain   string
	selector string
	key      crypto.Signer
}

// NewSigner parses a stored private key. Pass the DECRYPTED PEM --
// this package does not know about the crypto service, so callers
// unseal first.
func NewSigner(domain, selector, privatePEM string) (*Signer, error) {
	if domain == "" {
		return nil, errors.New("dkim: domain required")
	}

	if selector == "" {
		selector = DefaultSelector
	}

	block, _ := pem.Decode([]byte(privatePEM))
	if block == nil {
		return nil, errors.New("dkim: private key is not valid PEM")
	}

	parsed, err := x509.ParsePKCS8PrivateKey(block.Bytes)
	if err != nil {
		// Keys imported from elsewhere are commonly PKCS#1.
		if k1, err1 := x509.ParsePKCS1PrivateKey(block.Bytes); err1 == nil {
			parsed = k1
		} else {
			return nil, fmt.Errorf("dkim: parse private key: %w", err)
		}
	}

	signer, ok := parsed.(crypto.Signer)
	if !ok {
		return nil, fmt.Errorf("dkim: private key of type %T cannot sign", parsed)
	}

	return &Signer{domain: domain, selector: selector, key: signer}, nil
}

// Sign returns raw with a DKIM-Signature header prepended.
//
// relaxed/relaxed canonicalization: simple/simple breaks on any
// whitespace change in transit, which real relays make, and relaxed
// body canonicalization is what almost every sender uses for exactly
// that reason.
func (s *Signer) Sign(raw []byte) ([]byte, error) {
	var out bytes.Buffer
	err := msgdkim.Sign(&out, bytes.NewReader(raw), &msgdkim.SignOptions{
		Domain:                 s.domain,
		Selector:               s.selector,
		Signer:                 s.key,
		Hash:                   crypto.SHA256,
		HeaderCanonicalization: msgdkim.CanonicalizationRelaxed,
		BodyCanonicalization:   msgdkim.CanonicalizationRelaxed,
		HeaderKeys:             signedHeaders,
	})
	if err != nil {
		return nil, fmt.Errorf("dkim: sign: %w", err)
	}

	return out.Bytes(), nil
}

// Domain is the domain this signer signs as (the d= tag).
func (s *Signer) Domain() string { return s.domain }

// LookupTXT resolves DNS TXT records. Injectable so verification can
// be tested without DNS, and so the same stub serves this package and
// the domain record checks.
type LookupTXT func(domain string) ([]string, error)

// maxVerifications bounds how many signatures we will check on one
// message. Each costs a DNS lookup and a public-key operation, so an
// unbounded count is a cheap way for a sender to make us do expensive
// work - and no legitimate message carries more than a handful.
const maxVerifications = 5

// Verify checks every DKIM signature on a message and reports the
// domains that passed. A message with no signature yields no results
// and no error - absence is not failure, DMARC decides what that
// means.
//
// lookup may be nil, in which case the system resolver is used.
func Verify(r io.Reader, lookup LookupTXT) ([]Result, error) {
	verifications, err := msgdkim.VerifyWithOptions(r, &msgdkim.VerifyOptions{
		LookupTXT:        lookup,
		MaxVerifications: maxVerifications,
	})
	if err != nil {
		return nil, fmt.Errorf("dkim: verify: %w", err)
	}

	out := make([]Result, 0, len(verifications))
	for _, v := range verifications {
		res := Result{Domain: strings.ToLower(v.Domain), Valid: v.Err == nil}
		if v.Err != nil {
			res.Error = v.Err.Error()
		}

		out = append(out, res)
	}

	return out, nil
}

// Result is one signature's outcome.
type Result struct {
	Domain string `json:"domain"`
	Valid  bool   `json:"valid"`
	Error  string `json:"error,omitempty"`
}
