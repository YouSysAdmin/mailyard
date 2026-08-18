package smtpclient

import (
	"bufio"
	"encoding/base64"
	"errors"
	"net"
	"net/textproto"
	"strings"
	"testing"
)

func TestBuildMessageAlternative(t *testing.T) {
	m := &Message{
		From:    "Sender <s@example.com>",
		To:      []string{"a@example.com", "b@example.com"},
		Subject: "Hello",
		HTML:    "<p>hi</p>",
		Text:    "hi",
	}
	out := string(m.Build())
	boundary := boundaryOf(t, out, "multipart/alternative")
	for _, want := range []string{
		"From: Sender <s@example.com>\r\n",
		"To: a@example.com, b@example.com\r\n",
		"Subject: Hello\r\n",
		"MIME-Version: 1.0\r\n",
		"Content-Type: text/plain; charset=\"UTF-8\"",
		"Content-Type: text/html; charset=\"UTF-8\"",
		"--" + boundary + "--",
	} {
		if !strings.Contains(out, want) {
			t.Errorf("message missing %q", want)
		}
	}
}

// boundaryOf pulls the delimiter out of the Content-Type header for
// mediaType. Boundaries are random per message, so tests read the one
// that was actually emitted instead of asserting a literal.
func boundaryOf(t *testing.T, msg, mediaType string) string {
	t.Helper()
	_, rest, ok := strings.Cut(msg, "Content-Type: "+mediaType+"; boundary=\"")
	if !ok {
		t.Fatalf("no %s part in message:\n%s", mediaType, msg)
	}

	b, _, ok := strings.Cut(rest, "\"")
	if !ok || b == "" {
		t.Fatalf("malformed boundary parameter for %s", mediaType)
	}

	return b
}

// A constant boundary lets template-rendered caller data close the
// part and inject a MIME section of its choosing, so two messages
// must never share one.
func TestBuildMessageBoundariesAreUnique(t *testing.T) {
	build := func() string {
		m := &Message{
			From: "s@example.com", To: []string{"r@example.com"},
			Subject: "x", HTML: "<p>hi</p>", Text: "hi",
		}

		return boundaryOf(t, string(m.Build()), "multipart/alternative")
	}
	if a, b := build(), build(); a == b {
		t.Fatalf("two messages shared boundary %q", a)
	}
}

func TestBuildMessageNonASCIIUsesQuotedPrintable(t *testing.T) {
	m := &Message{From: "s@example.com", To: []string{"r@example.com"}, Subject: "Grüße", Text: "Grüße aus Köln"}
	out := string(m.Build())
	if !strings.Contains(out, "Content-Transfer-Encoding: quoted-printable") {
		t.Error("non-ascii body must be quoted-printable")
	}

	if !strings.Contains(out, "=?UTF-8?q?") && !strings.Contains(out, "=?utf-8?q?") {
		t.Errorf("non-ascii subject must be Q-encoded, got %q", out)
	}

	if strings.Contains(out, "Grüße aus Köln") {
		t.Error("raw utf-8 body leaked into a quoted-printable part")
	}
}

func TestBuildMessageAttachments(t *testing.T) {
	content := base64.StdEncoding.EncodeToString([]byte(strings.Repeat("data", 100)))
	m := &Message{
		From: "s@example.com", To: []string{"r@example.com"}, Subject: "att", Text: "body",
		Attachments: []Attachment{{Filename: "file.txt", Content: content, ContentType: "text/plain"}},
	}
	out := string(m.Build())
	boundary := boundaryOf(t, out, "multipart/mixed")
	for _, want := range []string{
		`Content-Disposition: attachment; filename=file.txt`,
		"Content-Transfer-Encoding: base64",
		"--" + boundary + "--",
	} {
		if !strings.Contains(out, want) {
			t.Errorf("message missing %q", want)
		}
	}

	// base64 lines must wrap at 76 columns
	for line := range strings.SplitSeq(out, "\r\n") {
		if len(line) > 78 {
			t.Errorf("line longer than 78 bytes: %q", line)
		}
	}
}

func TestBuildMessageListUnsubscribe(t *testing.T) {
	m := &Message{
		From: "s@example.com", To: []string{"r@example.com"}, Subject: "x", Text: "y",
		ListUnsubscribeURL:    "https://mail.example.com/u/tok",
		ListUnsubscribeMailto: "mailto:unsub@example.com",
		ListUnsubscribePost:   true,
	}
	out := string(m.Build())
	if !strings.Contains(out, "List-Unsubscribe: <mailto:unsub@example.com>, <https://mail.example.com/u/tok>\r\n") {
		t.Errorf("list-unsubscribe header wrong:\n%s", out)
	}

	if !strings.Contains(out, "List-Unsubscribe-Post: List-Unsubscribe=One-Click\r\n") {
		t.Error("one-click header missing")
	}
}

func TestValidateAttachments(t *testing.T) {
	ok := Attachment{Filename: "a.txt", Content: base64.StdEncoding.EncodeToString([]byte("hello"))}
	if err := ValidateAttachments([]Attachment{ok}, 100, 100); err != nil {
		t.Fatalf("valid attachment rejected: %v", err)
	}

	if err := ValidateAttachments([]Attachment{{Filename: "", Content: ""}}, 100, 100); err == nil {
		t.Error("missing filename accepted")
	}

	if err := ValidateAttachments([]Attachment{{Filename: "a", Content: "!!not base64!!"}}, 100, 100); err == nil {
		t.Error("bad base64 accepted")
	}

	if err := ValidateAttachments([]Attachment{ok}, 2, 100); err == nil {
		t.Error("oversize attachment accepted")
	}

	if err := ValidateAttachments([]Attachment{ok, ok, ok}, 100, 10); err == nil {
		t.Error("oversize total accepted")
	}
}

func TestSendErrorPermanent(t *testing.T) {
	base := &textproto.Error{Code: 550, Msg: "5.1.1 user unknown"}
	err := wrapSendError("RCPT TO", "x@example.com", base)
	var se *SendError
	if !errors.As(err, &se) {
		t.Fatal("expected *SendError")
	}

	if !se.Permanent() || se.Code != 550 || se.Recipient != "x@example.com" {
		t.Errorf("unexpected send error: %+v", se)
	}

	transient := wrapSendError("DATA", "", &textproto.Error{Code: 451, Msg: "try later"})
	errors.As(transient, &se)
	if se.Permanent() {
		t.Error("451 must not be permanent")
	}
}

func TestEnvelopeAddress(t *testing.T) {
	if got := EnvelopeAddress("Name <u@example.com>"); got != "u@example.com" {
		t.Errorf("got %q", got)
	}

	if got := EnvelopeAddress("u@example.com"); got != "u@example.com" {
		t.Errorf("got %q", got)
	}
}

// fakeSMTP accepts one plain SMTP session and captures the DATA
// payload.
func fakeSMTP(t *testing.T, data *strings.Builder) net.Addr {
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
		write := func(s string) { _, _ = conn.Write([]byte(s + "\r\n")) }
		write("220 fake ESMTP")
		inData := false
		for {
			line, err := r.ReadString('\n')
			if err != nil {
				return
			}

			line = strings.TrimRight(line, "\r\n")
			if inData {
				if line == "." {
					inData = false
					write("250 ok")
					continue
				}

				data.WriteString(line)
				data.WriteByte('\n')
				continue
			}

			switch {
			case strings.HasPrefix(line, "EHLO"), strings.HasPrefix(line, "HELO"):
				_, _ = conn.Write([]byte("250-fake\r\n250 8BITMIME\r\n"))
			case strings.HasPrefix(line, "MAIL FROM"):
				write("250 ok")
			case strings.HasPrefix(line, "RCPT TO"):
				write("250 ok")
			case line == "DATA":
				inData = true
				write("354 go ahead")
			case line == "QUIT":
				write("221 bye")

				return
			default:
				write("250 ok")
			}
		}
	}()

	return ln.Addr()
}

func TestSendPlainEndToEnd(t *testing.T) {
	var data strings.Builder
	addr := fakeSMTP(t, &data)
	tcp := addr.(*net.TCPAddr)

	cfg := ServerConfig{Host: "127.0.0.1", Port: tcp.Port, Encryption: EncryptionNone}
	msg := &Message{From: "s@example.com", To: []string{"r@example.com"}, Subject: "e2e", Text: "hello"}
	if err := Send(cfg, msg); err != nil {
		t.Fatalf("send: %v", err)
	}

	if !strings.Contains(data.String(), "Subject: e2e") || !strings.Contains(data.String(), "hello") {
		t.Errorf("server did not receive the message, got:\n%s", data.String())
	}
}

// Which recipient a rejection names, and when it names none.
//
// Recipient is filled on every stage for the error message, and on MAIL
// FROM it holds the sender. The caller suppresses whatever this returns
// for every future send, and the suppression list doubles as an inbound
// blocklist - so a 5xx on the envelope must not take the project's own
// sender or bounce address out of service.
//
// Kept here rather than in the domain layer, where every new provider has
// to remember an SMTP stage name.
func TestOnlyARecipientRejectionNamesARecipient(t *testing.T) {
	for name, tc := range map[string]struct {
		err  *SendError
		want string
	}{
		"rcpt to names the recipient": {
			&SendError{Stage: "RCPT TO", Recipient: "bob@example.net", Code: 550},
			"bob@example.net",
		},
		"mail from holds the SENDER and must name nobody": {
			&SendError{Stage: "MAIL FROM", Recipient: "bounces@mail.example.com", Code: 550},
			"",
		},
		"data names nobody": {
			&SendError{Stage: "DATA", Recipient: "bob@example.net", Code: 552},
			"",
		},
		"a dial failure names nobody": {
			&SendError{Stage: "DIAL"},
			"",
		},
	} {
		if got := tc.err.RejectedRecipient(); got != tc.want {
			t.Errorf("%s: RejectedRecipient() = %q, want %q", name, got, tc.want)
		}
	}
}
