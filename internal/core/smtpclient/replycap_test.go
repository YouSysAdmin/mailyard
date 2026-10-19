// Mailyard, Copyright (c) 2021-2026 YouSysAdmin

package smtpclient

import (
	"bufio"
	"context"
	"errors"
	"net"
	"runtime"
	"strconv"
	"strings"
	"testing"
	"time"
)

// endlessReply serves a greeting and an EHLO like any server, then
// answers MAIL FROM with continuation lines that never end.
func endlessReply(t *testing.T) string {
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
		_, _ = conn.Write([]byte("220 tarpit ESMTP\r\n"))
		line := []byte("451-" + strings.Repeat("x", 1020) + "\r\n")
		for {
			cmd, err := r.ReadString('\n')
			if err != nil {
				return
			}

			if !strings.HasPrefix(cmd, "MAIL FROM") {
				_, _ = conn.Write([]byte("250-tarpit\r\n250 8BITMIME\r\n"))
				continue
			}

			for {
				if _, err := conn.Write(line); err != nil {
					return
				}
			}
		}
	}()

	return strconv.Itoa(ln.Addr().(*net.TCPAddr).Port)
}

// checkCut asserts the attempt failed on the reply cap, quickly, as a
// transient failure, and without holding what the peer streamed.
func checkCut(t *testing.T, run func(ctx context.Context) error) {
	t.Helper()
	ctx, cancel := context.WithTimeout(t.Context(), 3*time.Second)
	defer cancel()

	var before, after runtime.MemStats
	runtime.ReadMemStats(&before)
	start := time.Now()
	err := run(ctx)
	took := time.Since(start)
	runtime.ReadMemStats(&after)

	if !errors.Is(err, ErrReplyTooLarge) {
		t.Fatalf("err = %v, want ErrReplyTooLarge", err)
	}

	if took > time.Second {
		t.Errorf("cut only after %s", took)
	}

	if grew := after.TotalAlloc - before.TotalAlloc; grew > 8<<20 {
		t.Errorf("allocated %d bytes reading one reply", grew)
	}

	if _, ok := errors.AsType[*ConfigError](err); ok {
		t.Error("an oversized reply was reported as a server fault")
	}

	if se, ok := errors.AsType[*SendError](err); ok && se.Permanent() {
		t.Error("an oversized reply was reported as a permanent failure")
	}
}

func TestAnEndlessReplyIsCutOnAProjectServer(t *testing.T) {
	port, _ := strconv.Atoi(endlessReply(t))
	cfg := ServerConfig{Host: "127.0.0.1", Port: port, Encryption: EncryptionNone}

	checkCut(t, func(ctx context.Context) error {
		return Send(ctx, cfg, &Message{From: "s@example.com", To: []string{"r@example.com"}, Text: "x"})
	})
}

func TestAnEndlessReplyIsCutOnADirectDelivery(t *testing.T) {
	cfg := directCfg(endlessReply(t))

	checkCut(t, func(ctx context.Context) error {
		_, err := SendDirect(ctx, cfg, []string{"mx.example.org"}, rawMessage())

		return err
	})
}

// The cap is per reply. A body far larger than it is written, not read,
// and every reply after it is counted from zero.
func TestAMessageLargerThanTheReplyCapIsSent(t *testing.T) {
	var data strings.Builder
	addr := fakeSMTP(t, &data)
	cfg := ServerConfig{Host: "127.0.0.1", Port: addr.(*net.TCPAddr).Port, Encryption: EncryptionNone}
	body := strings.Repeat(strings.Repeat("y", 70)+"\n", 4*maxReplySize/70)
	msg := &Message{From: "s@example.com", To: []string{"r@example.com"}, Subject: "big", Text: body}
	if err := Send(t.Context(), cfg, msg); err != nil {
		t.Fatalf("send: %v", err)
	}

	if data.Len() < 4*maxReplySize {
		t.Errorf("server received %d bytes", data.Len())
	}
}
