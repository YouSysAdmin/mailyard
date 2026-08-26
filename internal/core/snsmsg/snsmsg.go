// Mailyard, Copyright (c) 2021-2026 YouSysAdmin

// Package snsmsg parses and authenticates Amazon SNS HTTPS
// deliveries.
//
// Nothing here knows what the payload means. SNS is a transport, and
// this package's whole job is to answer "did Amazon really send this,
// unmodified" before anything downstream reads a byte of it.
package snsmsg

import (
	"crypto/x509"
	"encoding/base64"
	"encoding/json/v2"
	"encoding/pem"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"regexp"
	"strings"
	"sync"
	"time"
)

// Message types SNS posts to an HTTPS subscription.
const (
	TypeSubscriptionConfirmation = "SubscriptionConfirmation"
	TypeNotification             = "Notification"
	TypeUnsubscribeConfirmation  = "UnsubscribeConfirmation"
)

// Message is one SNS delivery.
type Message struct {
	Type             string `json:"Type"`
	MessageID        string `json:"MessageId"`
	TopicARN         string `json:"TopicArn"`
	Subject          string `json:"Subject"`
	Message          string `json:"Message"`
	Timestamp        string `json:"Timestamp"`
	SignatureVersion string `json:"SignatureVersion"`
	Signature        string `json:"Signature"`
	SigningCertURL   string `json:"SigningCertURL"`

	// SubscribeURL and Token appear on the confirmation types only.
	SubscribeURL string `json:"SubscribeURL"`
	Token        string `json:"Token"`
}

// Parse decodes the body without authenticating it. The result is
// untrusted until Verifier.Verify has passed.
func Parse(body []byte) (*Message, error) {
	var m Message
	if err := json.Unmarshal(body, &m); err != nil {
		return nil, fmt.Errorf("snsmsg: decode: %w", err)
	}

	switch m.Type {
	case TypeSubscriptionConfirmation, TypeNotification, TypeUnsubscribeConfirmation:
		return &m, nil
	default:
		return nil, fmt.Errorf("snsmsg: unknown message type %q", m.Type)
	}
}

// signingCertHost is the only shape of host a certificate may be
// fetched from.
//
// This check is not defence in depth, it IS the defence. The message
// names the URL of the key that verifies it, so without pinning the
// host an attacker signs with their own key, points here at their own
// certificate, and every signature they produce validates. Regional
// endpoints and the China partition are the legitimate variants.
var signingCertHost = regexp.MustCompile(`^sns\.[a-z0-9-]+\.amazonaws\.com(\.cn)?$`)

// canonicalFields is the field order AWS documents for the string to
// sign, per message type. Subject is included only when present.
var canonicalFields = map[string][]string{
	TypeNotification:             {"Message", "MessageId", "Subject", "Timestamp", "TopicArn", "Type"},
	TypeSubscriptionConfirmation: {"Message", "MessageId", "SubscribeURL", "Timestamp", "Token", "TopicArn", "Type"},
	TypeUnsubscribeConfirmation:  {"Message", "MessageId", "SubscribeURL", "Timestamp", "Token", "TopicArn", "Type"},
}

func (m *Message) field(name string) string {
	switch name {
	case "Message":
		return m.Message
	case "MessageId":
		return m.MessageID
	case "Subject":
		return m.Subject
	case "SubscribeURL":
		return m.SubscribeURL
	case "Timestamp":
		return m.Timestamp
	case "Token":
		return m.Token
	case "TopicArn":
		return m.TopicARN
	case "Type":
		return m.Type
	}

	return ""
}

// canonical builds the string SNS signed: name\nvalue\n per field, in
// the documented order, skipping an absent Subject.
func (m *Message) canonical() ([]byte, error) {
	fields, ok := canonicalFields[m.Type]
	if !ok {
		return nil, fmt.Errorf("snsmsg: unknown message type %q", m.Type)
	}

	var b strings.Builder
	for _, name := range fields {
		v := m.field(name)
		if name == "Subject" && v == "" {
			continue
		}

		b.WriteString(name)
		b.WriteByte('\n')
		b.WriteString(v)
		b.WriteByte('\n')
	}

	return []byte(b.String()), nil
}

// Verifier authenticates messages, caching the signing certificates
// it fetches. Safe for concurrent use.
type Verifier struct {
	// HTTP fetches signing certificates. Build it with
	// safedial.Client so a certificate URL cannot be turned into a
	// request at something on the local network.
	HTTP *http.Client

	// MaxAge bounds how old a message's Timestamp may be. Zero means
	// no bound. The signature proves Amazon SENT the message, not
	// WHEN it was presented: a captured notification stayed
	// replayable for as long as the signing certificate was valid,
	// and each replay filed another bounce row. An hour is generous
	// for SNS delivery retries and short enough that a capture is
	// worthless by the time anyone could use it. A timestamp more
	// than five minutes in the future is refused too, since that is
	// not late delivery but a clock nobody should trust.
	MaxAge time.Duration

	mu    sync.RWMutex
	certs map[string]*x509.Certificate
}

// ErrUntrusted is returned for anything that fails authentication:
// a bad signature, a certificate URL that is not Amazon's, an
// unsupported signature version. One error, because a caller has
// exactly one thing to do about any of them.
var ErrUntrusted = errors.New("snsmsg: message is not authentic")

// Verify checks the signature against the certificate the message
// names, having first checked that the certificate is Amazon's.
func (v *Verifier) Verify(m *Message) error {
	// Version 2 only, which is SHA-256. Version 1 is SHA-1: SNS has
	// signed with v2 since 2022, a topic's version cannot be lowered,
	// and a receiver that takes SHA-1 is one waiting for the day a
	// chosen-prefix collision meets a fixed canonical string.
	var algo x509.SignatureAlgorithm
	switch m.SignatureVersion {
	case "2":
		algo = x509.SHA256WithRSA
	default:
		return fmt.Errorf("%w: signature version %q", ErrUntrusted, m.SignatureVersion)
	}

	cert, err := v.certificate(m.SigningCertURL)
	if err != nil {
		return err
	}

	sig, err := base64.StdEncoding.DecodeString(m.Signature)
	if err != nil {
		return fmt.Errorf("%w: signature is not base64", ErrUntrusted)
	}

	canonical, err := m.canonical()
	if err != nil {
		return fmt.Errorf("%w: %s", ErrUntrusted, err)
	}

	if err := cert.CheckSignature(algo, canonical, sig); err != nil {
		return fmt.Errorf("%w: %s", ErrUntrusted, err)
	}

	if v.MaxAge > 0 {
		ts, err := time.Parse(time.RFC3339Nano, m.Timestamp)
		if err != nil {
			return fmt.Errorf("%w: timestamp is not RFC 3339", ErrUntrusted)
		}

		now := time.Now()
		if now.Sub(ts) > v.MaxAge || ts.Sub(now) > futureSkew {
			return fmt.Errorf("%w: timestamp %s is outside the accepted window", ErrUntrusted, m.Timestamp)
		}
	}

	return nil
}

// futureSkew is how far ahead of our clock a Timestamp may sit before
// it is refused as something other than late delivery.
const futureSkew = 5 * time.Minute

// withinValidity reports whether the certificate is usable at t.
//
// x509.Certificate.CheckSignature verifies the MATHS and nothing else -
// it does not consult NotBefore or NotAfter - so this is the only thing
// that makes an expired signing certificate stop being accepted.
func withinValidity(cert *x509.Certificate, t time.Time) bool {
	return !t.Before(cert.NotBefore) && !t.After(cert.NotAfter)
}

// certificate fetches and caches the signing certificate.
func (v *Verifier) certificate(rawURL string) (*x509.Certificate, error) {
	if err := checkCertURL(rawURL); err != nil {
		return nil, err
	}

	// A cached certificate is only reused while it is still VALID.
	//
	// The cache had no expiry and nothing checked the validity window, so
	// a certificate that expired or was rotated away kept verifying
	// signatures for the whole life of the process - which for a
	// long-running server is indefinitely. Amazon rotating its signing
	// key is the ordinary case, and re-fetching on a miss is what this
	// function already does.
	now := time.Now()
	v.mu.RLock()
	cert, ok := v.certs[rawURL]
	v.mu.RUnlock()
	if ok && withinValidity(cert, now) {
		return cert, nil
	}

	client := v.HTTP
	if client == nil {
		client = &http.Client{Timeout: 10 * time.Second}
	}

	resp, err := client.Get(rawURL)
	if err != nil {
		return nil, fmt.Errorf("snsmsg: fetch signing certificate: %w", err)
	}

	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("snsmsg: signing certificate returned %d", resp.StatusCode)
	}

	// A certificate is a couple of kilobytes. The cap is here because
	// the response is only as trustworthy as the host check above.
	raw, err := io.ReadAll(io.LimitReader(resp.Body, 1<<16))
	if err != nil {
		return nil, fmt.Errorf("snsmsg: read signing certificate: %w", err)
	}

	block, _ := pem.Decode(raw)
	if block == nil {
		return nil, fmt.Errorf("%w: signing certificate is not PEM", ErrUntrusted)
	}

	cert, err = x509.ParseCertificate(block.Bytes)
	if err != nil {
		return nil, fmt.Errorf("%w: signing certificate: %s", ErrUntrusted, err)
	}

	// Checked on the way in as well as on the way out of the cache.
	// CheckSignature does not look at the validity window, so without
	// this an expired certificate served from the pinned host would be
	// accepted as readily as a current one.
	if !withinValidity(cert, now) {
		return nil, fmt.Errorf("%w: signing certificate is outside its validity window (%s to %s)",
			ErrUntrusted, cert.NotBefore.Format(time.RFC3339), cert.NotAfter.Format(time.RFC3339))
	}

	v.mu.Lock()
	if v.certs == nil {
		v.certs = make(map[string]*x509.Certificate)
	}

	v.certs[rawURL] = cert
	v.mu.Unlock()

	return cert, nil
}

// checkCertURL runs before any request is made, which is the whole
// point of it.
func checkCertURL(rawURL string) error {
	u, err := url.Parse(rawURL)
	if err != nil {
		return fmt.Errorf("%w: signing certificate url is unparseable", ErrUntrusted)
	}

	if u.Scheme != "https" {
		return fmt.Errorf("%w: signing certificate url is not https", ErrUntrusted)
	}

	if !signingCertHost.MatchString(u.Hostname()) {
		return fmt.Errorf("%w: signing certificate url host %q is not amazon", ErrUntrusted, u.Hostname())
	}

	if !strings.HasSuffix(u.Path, ".pem") {
		return fmt.Errorf("%w: signing certificate url does not name a pem", ErrUntrusted)
	}

	return nil
}
