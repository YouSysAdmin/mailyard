// Mailyard, Copyright (c) 2021-2026 YouSysAdmin

package smtpclient

import (
	"bufio"
	"errors"
	"net"
	"strings"
	"sync"
	"testing"
)

// rcptSMTP is a server that answers RCPT TO from reply, keyed by the
// bare address, and records who DATA was sent to.
func rcptSMTP(t *testing.T, reply map[string]string) (net.Addr, func() (accepted []string, gotData bool)) {
	t.Helper()
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}

	t.Cleanup(func() { _ = ln.Close() })

	var mu sync.Mutex
	var accepted []string
	gotData := false
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
					mu.Lock()
					gotData = true
					mu.Unlock()
					write("250 ok")
				}

				continue
			}

			switch {
			case strings.HasPrefix(line, "EHLO"), strings.HasPrefix(line, "HELO"):
				_, _ = conn.Write([]byte("250-fake\r\n250 8BITMIME\r\n"))
			case strings.HasPrefix(line, "RCPT TO:"):
				addr := strings.Trim(strings.TrimPrefix(line, "RCPT TO:"), "<>")
				if rep, ok := reply[addr]; ok {
					write(rep)

					continue
				}

				mu.Lock()
				accepted = append(accepted, addr)
				mu.Unlock()
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

	return ln.Addr(), func() ([]string, bool) {
		mu.Lock()
		defer mu.Unlock()

		return accepted, gotData
	}
}

func sendTo(t *testing.T, addr net.Addr, to ...string) error {
	t.Helper()
	cfg := ServerConfig{Host: "127.0.0.1", Port: addr.(*net.TCPAddr).Port, Encryption: EncryptionNone}

	return Send(t.Context(), cfg, &Message{From: "s@example.com", To: to, Subject: "s", Text: "t"})
}

// One recipient refused for good does not stop the message reaching
// the others, and the refusal names that recipient alone.
func TestOneRefusedRecipientDoesNotFailTheOthers(t *testing.T) {
	addr, seen := rcptSMTP(t, map[string]string{"gone@x.test": "550 5.1.1 no such user"})

	err := sendTo(t, addr, "ann@x.test", "gone@x.test", "bob@x.test")
	r, ok := errors.AsType[*RecipientRefusals](err)
	if !ok {
		t.Fatalf("Send = %v, want the refusals reported", err)
	}

	if !r.Accepted() || r.Permanent() {
		t.Errorf("accepted=%v permanent=%v, want a delivered message", r.Accepted(), r.Permanent())
	}

	if len(r.Refused()) != 1 || r.Refusals[0].RejectedRecipient() != "gone@x.test" {
		t.Errorf("refused = %v, want gone@x.test alone", r.Refusals)
	}

	accepted, gotData := seen()
	if !gotData || len(accepted) != 2 {
		t.Errorf("data=%v accepted=%v, want the message sent to the two others", gotData, accepted)
	}
}

// Everybody refused is a permanent failure that still names each one.
func TestEveryRecipientRefusedNamesEach(t *testing.T) {
	addr, seen := rcptSMTP(t, map[string]string{
		"a@x.test": "550 5.1.1 no such user", "b@x.test": "553 5.1.3 bad address",
	})

	err := sendTo(t, addr, "a@x.test", "b@x.test")
	r, ok := errors.AsType[*RecipientRefusals](err)
	if !ok || r.Accepted() || !r.Permanent() || len(r.Refused()) != 2 {
		t.Fatalf("Send = %v, want a permanent failure naming both", err)
	}

	if r.RejectedRecipient() != "" {
		t.Errorf("RejectedRecipient = %q, want empty - Refused names each", r.RejectedRecipient())
	}

	if _, gotData := seen(); gotData {
		t.Error("DATA was sent with no recipient accepted")
	}
}

// A temporary refusal at RCPT TO fails the attempt whole, so the retry
// offers every recipient again, and a single recipient keeps the plain
// error it always had.
func TestATemporaryRefusalFailsTheAttempt(t *testing.T) {
	addr, _ := rcptSMTP(t, map[string]string{"busy@x.test": "451 4.3.0 try later"})
	err := sendTo(t, addr, "ann@x.test", "busy@x.test")
	se, ok := errors.AsType[*SendError](err)
	if !ok || se.Permanent() {
		t.Errorf("Send = %v, want the transient error", err)
	}

	addr, _ = rcptSMTP(t, map[string]string{"gone@x.test": "550 5.1.1 no such user"})
	err = sendTo(t, addr, "gone@x.test")
	if se, ok := errors.AsType[*SendError](err); !ok || se.RejectedRecipient() != "gone@x.test" {
		t.Errorf("Send to one refused recipient = %v, want the plain refusal", err)
	}
}
