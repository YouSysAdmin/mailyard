// Mailyard, Copyright (c) 2021-2026 YouSysAdmin

package smtpclient

import (
	"bytes"
	"encoding/base64"
	"errors"
	"io"
	"mime"
	"mime/multipart"
	"net/mail"
	"strings"
	"testing"

	"github.com/ProtonMail/go-crypto/openpgp"

	"github.com/yousysadmin/mailyard/internal/core/mailsign"
)

// recordingSigner is a Signer that keeps what it was asked to sign, so
// a test can compare that against what the wire carries.
type recordingSigner struct {
	saw  []byte
	fail error
}

func (r *recordingSigner) Protocol() string { return "application/x-test-signature" }
func (r *recordingSigner) MicAlg() string   { return "sha-256" }
func (r *recordingSigner) Sign(entity []byte) (mailsign.Part, error) {
	r.saw = append([]byte(nil), entity...)
	if r.fail != nil {
		return mailsign.Part{}, r.fail
	}

	return mailsign.Part{
		ContentType: "application/x-test-signature", Filename: "sig.bin",
		TransferEncoding: "base64", Body: []byte("signature-bytes"),
	}, nil
}

// signedParts opens the multipart/signed wrapper and returns the two
// parts as the wire carries them: the entity bytes exactly, and the
// decoded signature.
func signedParts(t *testing.T, raw string) (entity, signature []byte, protocol string) {
	t.Helper()
	m, err := mail.ReadMessage(strings.NewReader(raw))
	if err != nil {
		t.Fatal(err)
	}

	mediaType, params, err := mime.ParseMediaType(m.Header.Get("Content-Type"))
	if err != nil || mediaType != "multipart/signed" {
		t.Fatalf("top level is %q (%v), want multipart/signed", mediaType, err)
	}

	body, _ := io.ReadAll(m.Body)
	// The signed entity is everything between the first boundary line
	// and the CRLF before the second, byte for byte. mime/multipart
	// would re-read the headers, so the slice is taken by hand.
	delim := []byte("\r\n--" + params["boundary"])
	first := bytes.Index(body, []byte("--"+params["boundary"]+"\r\n"))
	start := first + len(params["boundary"]) + 4
	end := bytes.Index(body[start:], delim)
	if first < 0 || end < 0 {
		t.Fatalf("boundaries not found in:\n%s", body)
	}

	entity = body[start : start+end]

	mr := multipart.NewReader(bytes.NewReader(body), params["boundary"])
	if _, err := mr.NextPart(); err != nil {
		t.Fatal(err)
	}

	sigPart, err := mr.NextPart()
	if err != nil {
		t.Fatal(err)
	}

	signature, _ = io.ReadAll(sigPart)
	if sigPart.Header.Get("Content-Transfer-Encoding") == "base64" {
		if signature, err = base64.StdEncoding.DecodeString(string(signature)); err != nil {
			t.Fatal(err)
		}
	}

	return entity, signature, params["protocol"]
}

// What the signer saw is what the wire carries, with the whole body
// inside it: text parts, the attachment, and the public key file.
func TestASignedMessageWrapsTheWholeEntityTheSignerSaw(t *testing.T) {
	s := &recordingSigner{}
	m := &Message{
		From: "Billing <billing@example.com>", To: []string{"a@example.com"},
		Subject: "s", HTML: "<p>a line ending in a space \nsecond line</p>", Text: "plain",
		Attachments: []Attachment{{Filename: "a.txt", Content: "aGk=", ContentType: "text/plain"}},
		Signing: &Signing{Signer: s, PublicKey: &Attachment{
			Filename: "OpenPGP_0xABCD.asc", ContentType: "application/pgp-keys", Content: "a2V5",
		}},
	}
	raw := build(t, m)
	entity, signature, protocol := signedParts(t, raw)
	if !bytes.Equal(entity, s.saw) {
		t.Fatalf("the wire entity differs from what was signed:\nwire:\n%s\nsigned:\n%s", entity, s.saw)
	}

	if protocol != "application/x-test-signature" || string(signature) != "signature-bytes" {
		t.Errorf("protocol %q signature %q", protocol, signature)
	}

	if !strings.Contains(raw, "micalg=sha-256") {
		t.Error("micalg missing")
	}

	for _, want := range []string{"multipart/mixed", "multipart/alternative", "name=a.txt", "name=OpenPGP_0xABCD.asc"} {
		if !bytes.Contains(entity, []byte(want)) {
			t.Errorf("the signed entity lacks %q", want)
		}
	}

	// Canonical: no bare LF anywhere in the entity, and the text parts
	// are quoted-printable although they are plain ASCII, so a trailing
	// space is encoded rather than left for a hop to strip.
	if bytes.Contains(bytes.ReplaceAll(entity, []byte("\r\n"), nil), []byte("\n")) {
		t.Errorf("a bare LF in the signed entity:\n%q", entity)
	}

	if strings.Count(string(entity), "Content-Transfer-Encoding: quoted-printable") != 2 {
		t.Errorf("both text parts should be quoted-printable:\n%s", entity)
	}

	if !bytes.Contains(entity, []byte("space=20\r\nsecond")) {
		t.Errorf("the trailing space was not protected:\n%s", entity)
	}
}

// A signer that fails is a message that is not built, never one that
// goes out unsigned.
func TestASignerThatFailsRefusesTheBuild(t *testing.T) {
	m := &Message{
		From: "b@example.com", To: []string{"a@example.com"}, Subject: "s", Text: "t",
		Signing: &Signing{Signer: &recordingSigner{fail: errors.New("hsm away")}},
	}
	if _, err := m.Build(); err == nil || !strings.Contains(err.Error(), "hsm away") {
		t.Fatalf("Build: %v", err)
	}
}

// The Autocrypt header names the address and carries the key, folded.
func TestTheAutocryptHeaderIsFoldedAndNamesTheSender(t *testing.T) {
	key := strings.Repeat("A", 200)
	m := &Message{
		From: "Billing <billing@example.com>", To: []string{"a@example.com"}, Subject: "s", Text: "t",
		Signing: &Signing{AutocryptKey: key},
	}
	raw := build(t, m)
	msg, err := mail.ReadMessage(strings.NewReader(raw))
	if err != nil {
		t.Fatal(err)
	}

	got := msg.Header.Get("Autocrypt")
	if !strings.HasPrefix(got, "addr=billing@example.com; keydata=") || !strings.Contains(strings.ReplaceAll(got, " ", ""), key) {
		t.Fatalf("Autocrypt: %q", got)
	}

	for _, line := range strings.Split(raw, "\r\n") {
		if len(line) > 78 && !strings.HasPrefix(line, "Content-Type") {
			t.Errorf("a header line longer than 78: %q", line)
		}
	}

	// No signer, so the body is the plain entity.
	if strings.Contains(raw, "multipart/signed") {
		t.Error("an Autocrypt header alone must not wrap the body")
	}
}

// End to end with the real thing: a PGP signed message verifies with
// the key the signer was built from, against the entity as the wire
// carries it.
func TestAPGPSignedMessageVerifies(t *testing.T) {
	material, err := mailsign.GeneratePGP("Billing", "billing@example.com")
	if err != nil {
		t.Fatal(err)
	}

	signer, err := mailsign.New(material)
	if err != nil {
		t.Fatal(err)
	}

	m := &Message{
		From: "billing@example.com", To: []string{"a@example.com"}, Subject: "s",
		HTML: "<p>Привіт</p>", Text: "Привіт",
		Signing: &Signing{Signer: signer},
	}
	raw := build(t, m)
	entity, signature, protocol := signedParts(t, raw)
	if protocol != "application/pgp-signature" {
		t.Fatalf("protocol %q", protocol)
	}

	ring, err := openpgp.ReadArmoredKeyRing(strings.NewReader(material.Public))
	if err != nil {
		t.Fatal(err)
	}

	if _, err := openpgp.CheckArmoredDetachedSignature(ring, bytes.NewReader(entity), bytes.NewReader(signature), nil); err != nil {
		t.Fatalf("the signature does not verify against the wire entity: %v\n%s", err, raw)
	}
}
