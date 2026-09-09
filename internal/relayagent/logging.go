// Mailyard, Copyright (c) 2021-2026 YouSysAdmin

package relayagent

import (
	"fmt"
	"io"
	"log/slog"
	"strings"
	"sync"
)

// The library's logging, rerouted into ours.
//
// go-smtp defaults ErrorLog to a std-log writer on stderr, so a failed
// TLS handshake - a worker presenting the wrong certificate, a
// stranger probing the port - was reported in a format nothing else on
// this node uses, or lost entirely under a collector reading json. And
// a PROTOCOL refusal was reported to nobody at all: a malformed MAIL
// FROM is answered 501 inside the library, the backend hooks never
// run, and the node's log stayed empty while the platform's said the
// node refused it. Debugging that means reading the log of the wrong
// process, which is how it was actually found.

// smtpLogger adapts go-smtp's ErrorLog interface onto slog.
type smtpLogger struct {
	log      *slog.Logger
	listener string
}

// NewSMTPLogger builds the ErrorLog for one listener. The listener
// name is on every line because a node runs up to two of these and
// "error handling 1.2.3.4" answers half the question.
func NewSMTPLogger(log *slog.Logger, listener string) *smtpLogger {
	return &smtpLogger{log: log, listener: listener}
}

// Printf implements go-smtp's Logger.
func (l *smtpLogger) Printf(format string, v ...any) {
	l.log.Error("relay node: smtp server error", "listener", l.listener,
		"err", strings.TrimSpace(fmt.Sprintf(format, v...)))
}

// Println implements go-smtp's Logger.
func (l *smtpLogger) Println(v ...any) {
	l.log.Error("relay node: smtp server error", "listener", l.listener,
		"err", strings.TrimSpace(fmt.Sprintln(v...)))
}

// ProtocolTrace returns a writer for go-smtp's Debug hook: every line
// of the SMTP conversation, both directions, as a debug log entry.
//
// This is the ONLY place a protocol-level refusal is visible from this
// side - the library answers a malformed command itself and tells no
// hook - so it is what turns "the platform says the node said 501"
// into the actual line the node refused. Wired up only when the node
// runs at debug level: the tee carries message BODIES too, which is
// exactly right while an operator is staring at a broken handover and
// wrong every other day of the year.
func ProtocolTrace(log *slog.Logger, listener string) io.Writer {
	return &traceWriter{log: log, listener: listener}
}

// traceWriter splits the tee into lines and caps each one, so a body
// does not become one megabyte-long log entry.
type traceWriter struct {
	log      *slog.Logger
	listener string

	mu  sync.Mutex
	buf []byte
}

const traceLineCap = 512

// Write implements io.Writer over the raw protocol stream.
func (w *traceWriter) Write(p []byte) (int, error) {
	w.mu.Lock()
	defer w.mu.Unlock()
	w.buf = append(w.buf, p...)
	for {
		i := strings.IndexByte(string(w.buf), '\n')
		if i < 0 {
			break
		}

		line := strings.TrimRight(string(w.buf[:i]), "\r")
		w.buf = w.buf[i+1:]
		if len(line) > traceLineCap {
			line = line[:traceLineCap] + "..."
		}

		w.log.Debug("relay node: smtp", "listener", w.listener, "line", line)
	}

	return len(p), nil
}
