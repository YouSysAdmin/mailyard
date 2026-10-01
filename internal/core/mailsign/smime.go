// Mailyard, Copyright (c) 2021-2026 YouSysAdmin

package mailsign

import (
	"crypto"
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rsa"
	"crypto/sha256"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/asn1"
	"encoding/hex"
	"encoding/pem"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/smallstep/pkcs7"
	"software.sslmate.com/src/go-pkcs12"
)

const (
	smimeProtocol = "application/pkcs7-signature"
	smimeMicAlg   = "sha-256"
)

// oidEmailAddress is the legacy Subject emailAddress attribute, which
// older issuers put the address in instead of a SAN.
var oidEmailAddress = asn1.ObjectIdentifier{1, 2, 840, 113549, 1, 9, 1}

// ImportSMIME takes a PEM certificate chain, leaf first, and the leaf's
// private key. The key must be unencrypted PEM: the stdlib has no
// decoder for an encrypted PKCS#8 block and the legacy DEK-Info form
// is weak enough that supporting it would mean recommending it, so a
// protected key arrives as PKCS#12 through ImportPKCS12 instead.
func ImportSMIME(certPEM, keyPEM, email string) (Material, error) {
	chain, err := parseChain(certPEM)
	if err != nil {
		return Material{}, err
	}

	key, err := parseKey(keyPEM)
	if err != nil {
		return Material{}, err
	}

	return smimeMaterial(chain, key, email)
}

// ImportPKCS12 takes a .p12 or .pfx file and its password, which is
// how a public CA hands a mail certificate over.
func ImportPKCS12(der []byte, password, email string) (Material, error) {
	key, leaf, cas, err := pkcs12.DecodeChain(der, password)
	if err != nil {
		if errors.Is(err, pkcs12.ErrIncorrectPassword) {
			return Material{}, ErrPassphrase
		}

		return Material{}, fmt.Errorf("mailsign: not a pkcs12 file: %w", err)
	}

	signer, ok := key.(crypto.Signer)
	if !ok {
		return Material{}, errors.New("mailsign: the pkcs12 key cannot sign")
	}

	return smimeMaterial(append([]*x509.Certificate{leaf}, cas...), signer, email)
}

func parseChain(certPEM string) ([]*x509.Certificate, error) {
	var chain []*x509.Certificate
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

		c, err := x509.ParseCertificate(block.Bytes)
		if err != nil {
			return nil, fmt.Errorf("mailsign: parse certificate: %w", err)
		}

		chain = append(chain, c)
	}

	if len(chain) == 0 {
		return nil, errors.New("mailsign: no certificate in the PEM")
	}

	return chain, nil
}

func parseKey(keyPEM string) (crypto.Signer, error) {
	block, _ := pem.Decode([]byte(keyPEM))
	if block == nil {
		return nil, errors.New("mailsign: no PEM block in the private key")
	}

	if block.Type == "ENCRYPTED PRIVATE KEY" || block.Headers["Proc-Type"] != "" {
		return nil, errors.New("mailsign: the private key is passphrase protected, import it as a .p12 file instead")
	}

	var key any
	var err error
	switch block.Type {
	case "PRIVATE KEY":
		key, err = x509.ParsePKCS8PrivateKey(block.Bytes)
	case "RSA PRIVATE KEY":
		key, err = x509.ParsePKCS1PrivateKey(block.Bytes)
	case "EC PRIVATE KEY":
		key, err = x509.ParseECPrivateKey(block.Bytes)
	default:
		return nil, fmt.Errorf("mailsign: unsupported private key block %q", block.Type)
	}

	if err != nil {
		return nil, fmt.Errorf("mailsign: parse private key: %w", err)
	}

	signer, ok := key.(crypto.Signer)
	if !ok {
		return nil, errors.New("mailsign: the private key cannot sign")
	}

	return signer, nil
}

// smimeMaterial checks the pair is usable as this sender and renders
// it for storage.
func smimeMaterial(chain []*x509.Certificate, key crypto.Signer, email string) (Material, error) {
	leaf := chain[0]
	if err := checkLeaf(leaf, email); err != nil {
		return Material{}, err
	}

	pub, ok := leaf.PublicKey.(interface{ Equal(crypto.PublicKey) bool })
	if !ok || !pub.Equal(key.Public()) {
		return Material{}, errors.New("mailsign: the private key does not match the certificate")
	}

	switch key.(type) {
	case *rsa.PrivateKey, *ecdsa.PrivateKey:
	default:
		return Material{}, errors.New("mailsign: S/MIME needs an RSA or ECDSA key, mail clients do not verify other kinds")
	}

	der, err := x509.MarshalPKCS8PrivateKey(key)
	if err != nil {
		return Material{}, fmt.Errorf("mailsign: encode private key: %w", err)
	}

	var pubPEM strings.Builder
	for _, c := range chain {
		pubPEM.Write(pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: c.Raw}))
	}

	return Material{
		Kind:    KindSMIME,
		Private: string(pem.EncodeToMemory(&pem.Block{Type: "PRIVATE KEY", Bytes: der})),
		Public:  pubPEM.String(),
	}, nil
}

// checkLeaf refuses what a mail client would refuse later, when the
// mail has already gone out. A certificate for another address signs
// as somebody else, one without the mail EKU shows as invalid, and an
// expired one shows as expired - every one of these is reported to the
// operator at import, not to the recipient at read time.
func checkLeaf(leaf *x509.Certificate, email string) error {
	if !certNamesAddress(leaf, email) {
		return fmt.Errorf("mailsign: the certificate is not issued to %s", email)
	}

	if len(leaf.ExtKeyUsage) > 0 {
		ok := false
		for _, eku := range leaf.ExtKeyUsage {
			if eku == x509.ExtKeyUsageEmailProtection || eku == x509.ExtKeyUsageAny {
				ok = true
			}
		}

		if !ok {
			return errors.New("mailsign: the certificate is not issued for email protection")
		}
	}

	if leaf.KeyUsage != 0 && leaf.KeyUsage&(x509.KeyUsageDigitalSignature|x509.KeyUsageContentCommitment) == 0 {
		return errors.New("mailsign: the certificate key usage does not allow signing")
	}

	now := time.Now()
	if now.After(leaf.NotAfter) {
		return fmt.Errorf("mailsign: the certificate expired on %s", leaf.NotAfter.Format(time.DateOnly))
	}

	if now.Before(leaf.NotBefore) {
		return fmt.Errorf("mailsign: the certificate is not valid before %s", leaf.NotBefore.Format(time.DateOnly))
	}

	return nil
}

func certNamesAddress(c *x509.Certificate, email string) bool {
	for _, a := range c.EmailAddresses {
		if strings.EqualFold(a, email) {
			return true
		}
	}

	for _, n := range c.Subject.Names {
		if s, ok := n.Value.(string); ok && n.Type.Equal(oidEmailAddress) && strings.EqualFold(s, email) {
			return true
		}
	}

	return false
}

// smimeSigner signs with one certificate and carries its chain.
type smimeSigner struct {
	leaf    *x509.Certificate
	parents []*x509.Certificate
	key     crypto.Signer
}

func newSMIMESigner(keyPEM, certPEM string) (Signer, error) {
	chain, err := parseChain(certPEM)
	if err != nil {
		return nil, err
	}

	key, err := parseKey(keyPEM)
	if err != nil {
		return nil, err
	}

	return &smimeSigner{leaf: chain[0], parents: chain[1:], key: key}, nil
}

func (s *smimeSigner) Protocol() string { return smimeProtocol }
func (s *smimeSigner) MicAlg() string   { return smimeMicAlg }

// Sign produces a detached CMS SignedData over the entity. The chain
// rides inside it, which is how a client that has never seen this
// sender learns the certificate and can verify the next message
// without being told anything.
func (s *smimeSigner) Sign(entity []byte) (Part, error) {
	sd, err := pkcs7.NewSignedData(entity)
	if err != nil {
		return Part{}, fmt.Errorf("mailsign: smime sign: %w", err)
	}

	sd.SetDigestAlgorithm(pkcs7.OIDDigestAlgorithmSHA256)
	if err := sd.AddSignerChain(s.leaf, s.key, s.parents, pkcs7.SignerInfoConfig{}); err != nil {
		return Part{}, fmt.Errorf("mailsign: smime sign: %w", err)
	}

	sd.Detach()
	der, err := sd.Finish()
	if err != nil {
		return Part{}, fmt.Errorf("mailsign: smime sign: %w", err)
	}

	return Part{
		ContentType:      smimeProtocol + `; name="smime.p7s"`,
		Filename:         "smime.p7s",
		TransferEncoding: "base64",
		Body:             der,
	}, nil
}

// describeSMIME reads the leaf of a PEM chain.
func describeSMIME(certPEM string) (Description, error) {
	chain, err := parseChain(certPEM)
	if err != nil {
		return Description{}, err
	}

	leaf := chain[0]
	sum := sha256.Sum256(leaf.Raw)
	notAfter := leaf.NotAfter

	return Description{
		Kind:        KindSMIME,
		Fingerprint: strings.ToUpper(hex.EncodeToString(sum[:])),
		Algorithm:   certAlgorithm(leaf),
		Subject:     subjectLine(leaf.Subject, leaf.EmailAddresses),
		Issuer:      leaf.Issuer.String(),
		NotAfter:    &notAfter,
	}, nil
}

func certAlgorithm(c *x509.Certificate) string {
	switch k := c.PublicKey.(type) {
	case *rsa.PublicKey:
		return fmt.Sprintf("RSA-%d", k.N.BitLen())
	case *ecdsa.PublicKey:
		if k.Curve == elliptic.P256() {
			return "ECDSA P-256"
		}

		return "ECDSA " + k.Curve.Params().Name
	default:
		return c.PublicKeyAlgorithm.String()
	}
}

// subjectLine is the subject as a person reads it: the common name,
// or the address when the issuer put nothing else in.
func subjectLine(subject pkix.Name, emails []string) string {
	if subject.CommonName != "" {
		return subject.String()
	}

	if len(emails) > 0 {
		return emails[0]
	}

	return subject.String()
}
