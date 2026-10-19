// Mailyard, Copyright (c) 2021-2026 YouSysAdmin

package smtpclient

import (
	"context"
	"net"
	"testing"
	"time"
)

// A peer that accepts the connection and then says nothing is cut
// by the context, whatever stage the conversation is at.
func TestASilentPeerIsCutByTheContext(t *testing.T) {
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

		// Hold the connection open and never send the banner.
		<-time.After(5 * time.Second)
		_ = conn.Close()
	}()

	ctx, cancel := context.WithTimeout(t.Context(), 300*time.Millisecond)
	defer cancel()

	cfg := ServerConfig{Host: "127.0.0.1", Port: ln.Addr().(*net.TCPAddr).Port, Encryption: EncryptionNone}
	start := time.Now()
	err = Send(ctx, cfg, &Message{From: "s@example.com", To: []string{"r@example.com"}, Text: "x"})
	if err == nil {
		t.Fatal("send against a silent peer succeeded")
	}

	if took := time.Since(start); took > 3*time.Second {
		t.Errorf("send returned only after %s", took)
	}
}

// A peer that keeps talking never trips the idle cut, so the
// conversation deadline is what ends it, well before the caller's own.
func TestAPeerThatKeepsTalkingIsCutByTheConversationDeadline(t *testing.T) {
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
		for range 100 {
			if _, err := conn.Write([]byte("220-still here\r\n")); err != nil {
				return
			}

			time.Sleep(50 * time.Millisecond)
		}
	}()

	ctx, cancel := context.WithTimeout(t.Context(), 30*time.Second)
	defer cancel()

	cfg := ServerConfig{
		Host: "127.0.0.1", Port: ln.Addr().(*net.TCPAddr).Port, Encryption: EncryptionNone,
		ConversationTimeout: 300 * time.Millisecond,
	}
	start := time.Now()
	err = Send(ctx, cfg, &Message{From: "s@example.com", To: []string{"r@example.com"}, Text: "x"})
	if err == nil {
		t.Fatal("send against a tarpit succeeded")
	}

	if took := time.Since(start); took > 2*time.Second {
		t.Errorf("send returned only after %s, want the conversation deadline", took)
	}
}

// A connection test against a tarpit is cut by the conversation
// deadline too, not only a send.
func TestAConnectionTestIsCutByTheConversationDeadline(t *testing.T) {
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
		for range 100 {
			if _, err := conn.Write([]byte("220-still here\r\n")); err != nil {
				return
			}

			time.Sleep(50 * time.Millisecond)
		}
	}()

	ctx, cancel := context.WithTimeout(t.Context(), 30*time.Second)
	defer cancel()

	cfg := ServerConfig{
		Host: "127.0.0.1", Port: ln.Addr().(*net.TCPAddr).Port, Encryption: EncryptionNone,
		ConversationTimeout: 300 * time.Millisecond,
	}
	start := time.Now()
	if err := TestConnection(ctx, cfg); err == nil {
		t.Fatal("connection test against a tarpit succeeded")
	}

	if took := time.Since(start); took > 2*time.Second {
		t.Errorf("connection test returned only after %s, want the conversation deadline", took)
	}
}
