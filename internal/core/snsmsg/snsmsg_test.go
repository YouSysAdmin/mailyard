// Mailyard, Copyright (c) 2021-2026 YouSysAdmin

package snsmsg

import (
	"crypto"
	"crypto/rand"
	"crypto/rsa"
	"crypto/sha256"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/base64"
	"errors"
	"math/big"
	"net/http"
	"strings"
	"testing"
	"time"
)

// signingPair mints a throwaway certificate and returns a verifier
// preloaded with it, so a test can sign like SNS does without
// reaching the network.
func signingPair(t *testing.T, certURL string) (*rsa.PrivateKey, *Verifier) {
	t.Helper()
	key, err := rsa.GenerateKey(rand.Reader, 2048)
	if err != nil {
		t.Fatal(err)
	}

	tmpl := &x509.Certificate{
		SerialNumber: big.NewInt(1),
		Subject:      pkix.Name{CommonName: "sns.amazonaws.com"},
		NotBefore:    time.Now().Add(-time.Hour),
		NotAfter:     time.Now().Add(time.Hour),
	}
	der, err := x509.CreateCertificate(rand.Reader, tmpl, tmpl, &key.PublicKey, key)
	if err != nil {
		t.Fatal(err)
	}

	cert, err := x509.ParseCertificate(der)
	if err != nil {
		t.Fatal(err)
	}

	return key, &Verifier{certs: map[string]*x509.Certificate{certURL: cert}}
}

func sign(t *testing.T, key *rsa.PrivateKey, m *Message) {
	t.Helper()
	canonical, err := m.canonical()
	if err != nil {
		t.Fatal(err)
	}

	sum := sha256.Sum256(canonical)
	sig, err := rsa.SignPKCS1v15(rand.Reader, key, crypto.SHA256, sum[:])
	if err != nil {
		t.Fatal(err)
	}

	m.Signature = base64.StdEncoding.EncodeToString(sig)
	m.SignatureVersion = "2"
}

func notification(certURL string) *Message {
	return &Message{
		Type:           TypeNotification,
		MessageID:      "11111111-2222-3333-4444-555555555555",
		TopicARN:       "arn:aws:sns:eu-west-1:123456789012:mailyard-ses",
		Subject:        "Amazon SES Email Event Notification",
		Message:        `{"notificationType":"Bounce"}`,
		Timestamp:      "2026-08-07T12:00:00.000Z",
		SigningCertURL: certURL,
	}
}

const goodCertURL = "https://sns.eu-west-1.amazonaws.com/SimpleNotificationService-abc.pem"

func TestVerifyAcceptsASignedNotification(t *testing.T) {
	key, v := signingPair(t, goodCertURL)
	m := notification(goodCertURL)
	sign(t, key, m)

	if err := v.Verify(m); err != nil {
		t.Fatalf("a correctly signed message was rejected: %v", err)
	}
}

// Every signed field is covered, so touching any of them breaks the
// signature. Message is the one that carries the payload, so it is
// the one an attacker would want to change.
func TestVerifyRejectsATamperedField(t *testing.T) {
	for _, tamper := range []struct {
		name string
		with func(*Message)
	}{
		{"message body", func(m *Message) { m.Message = `{"notificationType":"Complaint"}` }},
		{"topic", func(m *Message) { m.TopicARN = "arn:aws:sns:eu-west-1:999:elsewhere" }},
		{"timestamp", func(m *Message) { m.Timestamp = "2020-01-01T00:00:00.000Z" }},
		{"subject", func(m *Message) { m.Subject = "something else" }},
		{"message id", func(m *Message) { m.MessageID = "00000000-0000-0000-0000-000000000000" }},
	} {
		t.Run(tamper.name, func(t *testing.T) {
			key, v := signingPair(t, goodCertURL)
			m := notification(goodCertURL)
			sign(t, key, m)
			tamper.with(m)

			if err := v.Verify(m); !errors.Is(err, ErrUntrusted) {
				t.Fatalf("a tampered %s verified, got %v", tamper.name, err)
			}
		})
	}
}

// The message names the URL of the key that verifies it. Without the
// host check an attacker signs with their own key, points here at
// their own certificate, and every signature they make validates - so
// this refusal has to happen before any request is made.
func TestVerifyRefusesACertificateURLThatIsNotAmazon(t *testing.T) {
	for _, bad := range []string{
		"https://evil.example.com/cert.pem",
		"https://sns.eu-west-1.amazonaws.com.evil.com/cert.pem",
		"http://sns.eu-west-1.amazonaws.com/cert.pem",
		"https://sns.eu-west-1.amazonaws.com/redirect?to=evil",
		"https://s3.eu-west-1.amazonaws.com/anyones-bucket/cert.pem",
	} {
		t.Run(bad, func(t *testing.T) {
			v := &Verifier{HTTP: refusingClient(t)}
			m := notification(bad)
			m.SignatureVersion = "2"
			m.Signature = base64.StdEncoding.EncodeToString([]byte("whatever"))

			if err := v.Verify(m); !errors.Is(err, ErrUntrusted) {
				t.Fatalf("accepted certificate url %q, got %v", bad, err)
			}
		})
	}
}

// refusingClient fails the test if anything tries to make a request,
// which is how the "before any fetch" part is asserted rather than
// assumed.
func refusingClient(t *testing.T) *http.Client {
	t.Helper()

	return &http.Client{Transport: roundTripFunc(func(r *http.Request) (*http.Response, error) {
		t.Errorf("a request was made to %s before the url was rejected", r.URL)

		return nil, errors.New("blocked")
	})}
}

type roundTripFunc func(*http.Request) (*http.Response, error)

func (f roundTripFunc) RoundTrip(r *http.Request) (*http.Response, error) { return f(r) }

func TestVerifyRejectsAnUnknownSignatureVersion(t *testing.T) {
	_, v := signingPair(t, goodCertURL)
	m := notification(goodCertURL)
	m.SignatureVersion = "3"

	if err := v.Verify(m); !errors.Is(err, ErrUntrusted) {
		t.Fatalf("accepted signature version 3, got %v", err)
	}
}

// A confirmation signs a different field set, including SubscribeURL
// and Token. Getting the order wrong would make every confirmation
// fail, and the subscription would never activate.
func TestSubscriptionConfirmationUsesItsOwnFieldSet(t *testing.T) {
	key, v := signingPair(t, goodCertURL)
	m := &Message{
		Type:           TypeSubscriptionConfirmation,
		MessageID:      "11111111-2222-3333-4444-555555555555",
		TopicARN:       "arn:aws:sns:eu-west-1:123456789012:mailyard-ses",
		Message:        "You have chosen to subscribe to the topic",
		SubscribeURL:   "https://sns.eu-west-1.amazonaws.com/?Action=ConfirmSubscription",
		Token:          "2336412f37fb",
		Timestamp:      "2026-08-07T12:00:00.000Z",
		SigningCertURL: goodCertURL,
	}
	sign(t, key, m)

	if err := v.Verify(m); err != nil {
		t.Fatalf("a signed subscription confirmation was rejected: %v", err)
	}

	// Subject is not in this type's field set, so setting one must not
	// change the signature.
	canonical, _ := m.canonical()
	if strings.Contains(string(canonical), "Subject") {
		t.Error("the confirmation canonical string includes Subject")
	}
}

func TestParseRejectsAnUnknownType(t *testing.T) {
	if _, err := Parse([]byte(`{"Type":"SomethingElse"}`)); err == nil {
		t.Fatal("an unknown message type parsed")
	}

	if _, err := Parse([]byte(`not json`)); err == nil {
		t.Fatal("garbage parsed")
	}
}

func TestVerifyRefusesAStaleOrFutureNotification(t *testing.T) {
	for _, tc := range []struct {
		name string
		ts   time.Time
		ok   bool
	}{
		{"just now", time.Now(), true},
		{"half an hour ago", time.Now().Add(-30 * time.Minute), true},
		{"two hours ago", time.Now().Add(-2 * time.Hour), false},
		{"an hour ahead", time.Now().Add(time.Hour), false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			key, v := signingPair(t, goodCertURL)
			v.MaxAge = time.Hour
			m := notification(goodCertURL)
			m.Timestamp = tc.ts.UTC().Format("2006-01-02T15:04:05.000Z")
			sign(t, key, m)
			err := v.Verify(m)
			if tc.ok && err != nil {
				t.Fatalf("a fresh notification was refused: %v", err)
			}

			if !tc.ok && err == nil {
				t.Fatal("a notification outside the window was accepted")
			}
		})
	}
}
