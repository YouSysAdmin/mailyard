// Mailyard, Copyright (c) 2021-2026 YouSysAdmin

package smtpclient

import (
	"bufio"
	"context"
	"errors"
	"net"
	"strings"
	"sync"
	"testing"
)

// fakeMX is a scriptable mail exchanger. Enough SMTP to exercise the
// direct path and nothing more.
type fakeMX struct {
	// greet replaces the 220 banner. Empty means a normal one.
	greet string

	// offerSTARTTLS advertises the extension.
	offerSTARTTLS bool

	// breakTLS answers 220 to STARTTLS and then talks plain text,
	// which is what a peer with a broken TLS stack looks like.
	breakTLS bool

	// mailReply overrides the MAIL FROM answer.
	mailReply string

	// rcptReply decides per recipient. Nil accepts everyone.
	rcptReply func(addr string) string

	// dataReply overrides the reply to end-of-data.
	dataReply string

	mu       sync.Mutex
	sessions int
	helo     string
	envFrom  string
	rcpts    []string
	data     []byte
}

func startFakeMX(t *testing.T, f *fakeMX) string {
	t.Helper()
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("listen: %v", err)
	}

	t.Cleanup(func() { _ = ln.Close() })

	go func() {
		for {
			conn, err := ln.Accept()
			if err != nil {
				return
			}

			go f.serve(conn)
		}
	}()
	_, port, _ := net.SplitHostPort(ln.Addr().String())

	return port
}

func (f *fakeMX) serve(conn net.Conn) {
	defer func() { _ = conn.Close() }()
	f.mu.Lock()
	f.sessions++
	f.mu.Unlock()

	r := bufio.NewReader(conn)
	w := func(s string) { _, _ = conn.Write([]byte(s + "\r\n")) }

	greet := f.greet
	if greet == "" {
		greet = "220 fake.example ESMTP"
	}

	w(greet)

	for {
		line, err := r.ReadString('\n')
		if err != nil {
			return
		}

		line = strings.TrimRight(line, "\r\n")
		verb, rest, _ := strings.Cut(line, " ")

		switch strings.ToUpper(verb) {
		case "EHLO":
			f.mu.Lock()
			f.helo = rest
			f.mu.Unlock()
			if f.offerSTARTTLS {
				w("250-fake.example")
				w("250 STARTTLS")
			} else {
				w("250 fake.example")
			}
		case "HELO":
			f.mu.Lock()
			f.helo = rest
			f.mu.Unlock()
			w("250 fake.example")
		case "STARTTLS":
			w("220 ready")
			if f.breakTLS {
				// Not a TLS ServerHello. The client's handshake fails.
				_, _ = conn.Write([]byte("this is not tls\r\n"))

				return
			}

			return
		case "MAIL":
			f.mu.Lock()
			f.envFrom = rest
			f.mu.Unlock()
			if f.mailReply != "" {
				w(f.mailReply)
				continue
			}

			w("250 ok")
		case "RCPT":
			addr := strings.Trim(strings.TrimSpace(rest[3:]), "<>")
			reply := "250 ok"
			if f.rcptReply != nil {
				reply = f.rcptReply(addr)
			}

			if strings.HasPrefix(reply, "2") {
				f.mu.Lock()
				f.rcpts = append(f.rcpts, addr)
				f.mu.Unlock()
			}

			w(reply)
		case "DATA":
			w("354 go ahead")
			var body []byte
			for {
				l, derr := r.ReadString('\n')
				if derr != nil {
					return
				}

				if l == ".\r\n" || l == ".\n" {
					break
				}

				body = append(body, l...)
			}

			f.mu.Lock()
			f.data = body
			f.mu.Unlock()
			if f.dataReply != "" {
				w(f.dataReply)
				continue
			}

			w("250 queued")
		case "QUIT":
			w("221 bye")

			return
		default:
			w("500 unknown")
		}
	}
}

func (f *fakeMX) snapshot() (helo, envFrom string, rcpts []string, data []byte, sessions int) {
	f.mu.Lock()
	defer f.mu.Unlock()

	return f.helo, f.envFrom, append([]string(nil), f.rcpts...), append([]byte(nil), f.data...), f.sessions
}

func rawMessage() *Raw {
	return &Raw{
		EnvelopeFrom: "bounces@mail.example.com",
		To:           []string{"user@example.org"},
		Data: []byte("DKIM-Signature: v=1; a=rsa-sha256; d=user.com; b=abc\r\n" +
			"From: News <news@user.com>\r\n" +
			"To: user@example.org\r\n" +
			"Subject: hello\r\n" +
			"\r\n" +
			"body\r\n"),
	}
}

func directCfg(port string) DirectConfig {
	return DirectConfig{
		HELO:    "node1.mail.example.com",
		Network: "tcp",
		DialContext: func(ctx context.Context, network, addr string) (net.Conn, error) {
			// Every target name resolves to the one fake server.
			_, _, _ = net.SplitHostPort(addr)
			d := &net.Dialer{}

			return d.DialContext(ctx, "tcp", net.JoinHostPort("127.0.0.1", port))
		},
	}
}

// The whole reason Raw exists. The worker signed these bytes and
// cannot re-sign them, so a relay that rewrites so much as a header
// order delivers a message with a broken signature.
func TestTheMessageIsForwardedByteForByte(t *testing.T) {
	f := &fakeMX{}
	port := startFakeMX(t, f)

	msg := rawMessage()
	res, err := SendDirect(t.Context(), directCfg(port), []string{"mx.example.org"}, msg)
	if err != nil {
		t.Fatalf("SendDirect: %v", err)
	}

	if len(res.Accepted) != 1 {
		t.Fatalf("accepted %v", res.Accepted)
	}

	_, _, _, data, _ := f.snapshot()
	if string(data) != string(msg.Data) {
		t.Errorf("the bytes changed in transit\n got: %q\nwant: %q", data, msg.Data)
	}
}

func TestTheEnvelopeAndHELOAreWhatWeConfigured(t *testing.T) {
	f := &fakeMX{}
	port := startFakeMX(t, f)

	if _, err := SendDirect(t.Context(), directCfg(port), []string{"mx.example.org"}, rawMessage()); err != nil {
		t.Fatalf("SendDirect: %v", err)
	}

	helo, envFrom, rcpts, _, _ := f.snapshot()
	if helo != "node1.mail.example.com" {
		t.Errorf("HELO was %q - large receivers match this against the PTR", helo)
	}

	if !strings.Contains(envFrom, "bounces@mail.example.com") {
		t.Errorf("MAIL FROM was %q", envFrom)
	}

	if len(rcpts) != 1 || rcpts[0] != "user@example.org" {
		t.Errorf("recipients were %v", rcpts)
	}
}

// A message to one domain routinely has some recipients accepted and
// some refused. Collapsing that into one error means either bouncing
// the whole batch or delivering it twice.
func TestRecipientsAreJudgedIndividually(t *testing.T) {
	f := &fakeMX{rcptReply: func(addr string) string {
		if addr == "gone@example.org" {
			return "550 5.1.1 no such user"
		}

		return "250 ok"
	}}
	port := startFakeMX(t, f)

	msg := rawMessage()
	msg.To = []string{"live@example.org", "gone@example.org"}

	res, err := SendDirect(t.Context(), directCfg(port), []string{"mx.example.org"}, msg)
	if err != nil {
		t.Fatalf("SendDirect: %v", err)
	}

	if len(res.Accepted) != 1 || res.Accepted[0] != "live@example.org" {
		t.Errorf("accepted %v", res.Accepted)
	}

	rej, ok := res.Rejected["gone@example.org"]
	if !ok {
		t.Fatalf("rejected map is %v", res.Rejected)
	}

	var se *SendError
	if !errors.As(rej, &se) || !se.Permanent() {
		t.Errorf("rejection is %v, want a permanent *SendError", rej)
	}

	if _, _, _, data, _ := f.snapshot(); len(data) == 0 {
		t.Error("nothing was sent, but one recipient was accepted")
	}
}

// Nobody accepted, so there is nothing to transmit - but this is not
// an error. Each recipient already carries its own permanent reason,
// and an error here would make the caller retry a refused batch.
func TestEveryRecipientRefusedSendsNoDataAndNoError(t *testing.T) {
	f := &fakeMX{rcptReply: func(string) string { return "550 5.1.1 no" }}
	port := startFakeMX(t, f)

	msg := rawMessage()
	msg.To = []string{"a@example.org", "b@example.org"}

	res, err := SendDirect(t.Context(), directCfg(port), []string{"mx.example.org"}, msg)
	if err != nil {
		t.Fatalf("SendDirect: %v", err)
	}

	if len(res.Accepted) != 0 || len(res.Rejected) != 2 {
		t.Fatalf("accepted %v rejected %v", res.Accepted, res.Rejected)
	}

	if _, _, _, data, _ := f.snapshot(); len(data) != 0 {
		t.Error("DATA was sent with no accepted recipient")
	}
}

// What a preference list is for.
func TestATransientFailureMovesToTheNextExchanger(t *testing.T) {
	down := &fakeMX{greet: "421 4.3.2 not accepting messages"}
	downPort := startFakeMX(t, down)
	up := &fakeMX{}
	upPort := startFakeMX(t, up)

	cfg := DirectConfig{
		HELO:    "node1.mail.example.com",
		Network: "tcp",
		DialContext: func(ctx context.Context, _, addr string) (net.Conn, error) {
			host, _, _ := net.SplitHostPort(addr)
			port := upPort
			if host == "busy.example.org" {
				port = downPort
			}

			d := &net.Dialer{}

			return d.DialContext(ctx, "tcp", net.JoinHostPort("127.0.0.1", port))
		},
	}

	res, err := SendDirect(t.Context(), cfg, []string{"busy.example.org", "ok.example.org"}, rawMessage())
	if err != nil {
		t.Fatalf("SendDirect: %v", err)
	}

	if res.Host != "ok.example.org" {
		t.Errorf("delivered via %q", res.Host)
	}
}

// A 5xx from the exchanger is the DOMAIN's answer. Its siblings are
// the same mail system and will say the same thing, so walking them
// is pure delay before an identical bounce.
func TestAPermanentRefusalDoesNotTrySiblings(t *testing.T) {
	first := &fakeMX{mailReply: "550 5.7.1 we do not accept your mail"}
	firstPort := startFakeMX(t, first)
	second := &fakeMX{}
	secondPort := startFakeMX(t, second)

	cfg := DirectConfig{
		HELO:    "node1.mail.example.com",
		Network: "tcp",
		DialContext: func(ctx context.Context, _, addr string) (net.Conn, error) {
			host, _, _ := net.SplitHostPort(addr)
			port := secondPort
			if host == "a.example.org" {
				port = firstPort
			}

			d := &net.Dialer{}

			return d.DialContext(ctx, "tcp", net.JoinHostPort("127.0.0.1", port))
		},
	}

	_, err := SendDirect(t.Context(), cfg, []string{"a.example.org", "b.example.org"}, rawMessage())
	if err == nil {
		t.Fatal("expected a permanent failure")
	}

	var se *SendError
	if !errors.As(err, &se) || !se.Permanent() {
		t.Errorf("err is %v, want a permanent *SendError", err)
	}

	if _, _, _, _, sessions := second.snapshot(); sessions != 0 {
		t.Errorf("the sibling was contacted %d times after a permanent refusal", sessions)
	}
}

// RFC 7435. A peer whose TLS is broken still gets the mail, because
// every other sender on the internet delivers it and refusing only
// loses the message.
func TestABrokenSTARTTLSFallsBackToCleartext(t *testing.T) {
	f := &fakeMX{offerSTARTTLS: true, breakTLS: true}
	port := startFakeMX(t, f)

	res, err := SendDirect(t.Context(), directCfg(port), []string{"mx.example.org"}, rawMessage())
	if err != nil {
		t.Fatalf("SendDirect: %v", err)
	}

	if res.TLS != TLSNone {
		t.Errorf("TLS outcome is %q, want %q", res.TLS, TLSNone)
	}

	if len(res.Accepted) != 1 {
		t.Errorf("accepted %v", res.Accepted)
	}

	if _, _, _, data, sessions := f.snapshot(); len(data) == 0 || sessions != 2 {
		t.Errorf("sessions=%d delivered=%d bytes - expected a redial in the clear", sessions, len(data))
	}
}

func TestNoTargetsIsAnError(t *testing.T) {
	if _, err := SendDirect(t.Context(), DirectConfig{}, nil, rawMessage()); err == nil {
		t.Error("an empty target list was accepted")
	}
}

func TestNoRecipientsIsAnError(t *testing.T) {
	msg := rawMessage()
	msg.To = nil
	if _, err := SendDirect(t.Context(), DirectConfig{}, []string{"mx.example.org"}, msg); err == nil {
		t.Error("a message with no recipients was accepted")
	}
}
