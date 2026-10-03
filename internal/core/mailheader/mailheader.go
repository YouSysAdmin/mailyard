// Mailyard, Copyright (c) 2021-2026 YouSysAdmin

// Package mailheader is the one rulebook for custom message headers:
// which names are valid, which are reserved for the builder, how a
// project's defaults merge under a message's own, and what the
// submission listener may forward from a client's message.
//
// Four packages ask these questions - the send service, the project
// and campaign endpoints that store headers, and the submission
// listener - and a second copy of the reserved list in any of them is
// how one surface ends up accepting what another refuses.
package mailheader

import (
	"fmt"
	"maps"
	"slices"
	"strings"
)

// MaxCustom is how many custom headers one source may carry. The cap
// applies to each source on its own - a project's defaults, a
// campaign's, a message's - not to the merged result.
const MaxCustom = 20

// MaxNameLen and MaxValueLen bound one header. The builder folds long
// lines, so these bound what a project's default replicates onto every
// row it sends rather than a line length. The value cap leaves room for
// a References chain, which is the longest header anybody forwards on
// purpose.
const (
	MaxNameLen  = 128
	MaxValueLen = 4096
)

// ControlPrefix is Mailyard's own header namespace. Everything under
// it is either written by the builder (X-Mailyard-Email-Id) or read as
// an instruction by the submission listener, so a caller may not set
// one and a forwarded message never carries one out.
const ControlPrefix = "x-mailyard-"

// ResentPrefix covers the Resent-* trace block of RFC 5322 section
// 3.6.6, which says a message was re-sent by somebody - a claim about
// who handled it that the builder never makes on a caller's behalf.
const ResentPrefix = "resent-"

// MIMEPrefix covers every Content-* header. The builder owns the MIME
// structure - type, encoding, and the top-level Content-Location that
// would rebase every relative URL in the HTML - so a caller sets none
// of it.
const MIMEPrefix = "content-"

// reserved are the headers the builder owns, plus the ones a sender
// could use to speak for somebody else or redirect mail: Sender names
// who sent it on the author's behalf, the two receipt headers ask the
// recipient's client to mail an address of the caller's choosing, and
// Errors-To asks an old MTA to send bounces there.
var reserved = map[string]struct{}{
	"from": {}, "to": {}, "cc": {}, "bcc": {}, "subject": {}, "date": {},
	"mime-version": {}, "list-unsubscribe": {}, "list-unsubscribe-post": {},
	"return-path": {}, "message-id": {}, "received": {}, "dkim-signature": {},
	"reply-to": {}, "sender": {}, "disposition-notification-to": {},
	"return-receipt-to": {}, "errors-to": {},
}

// Reserved reports whether name belongs to the builder, either by
// being in the fixed set or by sitting under ControlPrefix, MIMEPrefix
// or ResentPrefix. Case insensitive.
func Reserved(name string) bool {
	lower := strings.ToLower(name)
	if _, ok := reserved[lower]; ok {
		return true
	}

	return strings.HasPrefix(lower, ControlPrefix) || strings.HasPrefix(lower, MIMEPrefix) ||
		strings.HasPrefix(lower, ResentPrefix)
}

// ValidName reports whether name is an RFC 5322 field name: one or
// more printable US-ASCII characters other than the colon. Leading or
// trailing whitespace fails - it is not part of any name, and a
// receiver that trims it would match a reserved header this check
// would otherwise have missed.
func ValidName(name string) bool {
	if name == "" {
		return false
	}

	for i := range len(name) {
		ch := name[i]
		if ch < 33 || ch > 126 || ch == ':' {
			return false
		}
	}

	return true
}

// Kind says why Validate refused a header.
type Kind int

const (
	// KindInvalidName is a name that is not RFC 5322 ftext.
	KindInvalidName Kind = iota + 1
	// KindReserved is a name the builder owns.
	KindReserved
	// KindInvalidValue is a value carrying a control character.
	KindInvalidValue
	// KindTooMany is more than MaxCustom entries.
	KindTooMany
	// KindTooLong is a name or value past its cap.
	KindTooLong
	// KindDuplicate is a name given twice in one set, in any case.
	KindDuplicate
)

// Error is one refused header. Name is empty for KindTooMany.
type Error struct {
	Name string
	Kind Kind
}

// Error renders the refusal for a caller.
func (e *Error) Error() string {
	switch e.Kind {
	case KindInvalidName:
		return fmt.Sprintf("header %q is not a valid header name", e.Name)
	case KindReserved:
		return fmt.Sprintf("header %q is reserved and cannot be overridden", e.Name)
	case KindInvalidValue:
		return fmt.Sprintf("header %q contains invalid characters", e.Name)
	case KindDuplicate:
		return fmt.Sprintf("header %q is given more than once, header names are compared without regard to case", e.Name)
	case KindTooLong:
		return fmt.Sprintf("header %q is too long: a name is at most %d characters, a value %d", e.Name, MaxNameLen, MaxValueLen)
	default:
		return fmt.Sprintf("at most %d custom headers", MaxCustom)
	}
}

// Validate checks a set of custom headers and returns the first
// refusal, or nil. The name is checked before the reserved lookup and
// against ftext rather than a character blocklist: "Bcc " with a
// trailing space is not reserved, and a lenient receiver folds the
// space away and honours it as Bcc.
func Validate(headers map[string]string) *Error {
	if len(headers) > MaxCustom {
		return &Error{Kind: KindTooMany}
	}

	seen := make(map[string]struct{}, len(headers))
	for name, value := range headers {
		if !ValidName(name) {
			return &Error{Name: name, Kind: KindInvalidName}
		}

		lower := strings.ToLower(name)
		if _, dup := seen[lower]; dup {
			return &Error{Name: name, Kind: KindDuplicate}
		}

		seen[lower] = struct{}{}

		if Reserved(name) {
			return &Error{Name: name, Kind: KindReserved}
		}

		if len(name) > MaxNameLen || len(value) > MaxValueLen {
			return &Error{Name: name, Kind: KindTooLong}
		}

		if !validValue(value) {
			return &Error{Name: name, Kind: KindInvalidValue}
		}
	}

	return nil
}

// validValue refuses every control character but the tab. CR and LF
// are the injection - a second header of the caller's choosing - and
// a NUL or an escape is a value a C-based receiver truncates or a
// terminal reinterprets, so the builder never gets to write one.
func validValue(value string) bool {
	for i := range len(value) {
		if ch := value[i]; (ch < 32 && ch != '\t') || ch == 127 {
			return false
		}
	}

	return true
}

// Merge lays own over defaults. A default whose name matches an own
// name case-insensitively is dropped, so the caller's spelling is the
// one written. Nil when both are empty, so a message without headers
// stores what it always stored. Neither input is mutated.
func Merge(defaults, own map[string]string) map[string]string {
	if len(defaults) == 0 {
		return own
	}

	out := make(map[string]string, len(defaults)+len(own))
	for name, value := range defaults {
		if !hasFold(own, name) {
			out[name] = value
		}
	}

	maps.Copy(out, own)

	return out
}

// Forwardable is the subset of a client message's headers that the
// submission listener passes on: everything that is not reserved and
// not on the project's drop list. A message's own From, To, Date,
// Message-ID and Content-Type are structural and the builder writes
// its own, so they are skipped rather than refused. Nil when nothing
// survives.
func Forwardable(parsed map[string]string, drop []string) map[string]string {
	var out map[string]string
	for name, value := range parsed {
		if Reserved(name) || listedFold(drop, name) {
			continue
		}

		if out == nil {
			out = make(map[string]string)
		}

		out[name] = value
	}

	return out
}

func hasFold(m map[string]string, name string) bool {
	for k := range m {
		if strings.EqualFold(k, name) {
			return true
		}
	}

	return false
}

func listedFold(list []string, name string) bool {
	return slices.ContainsFunc(list, func(k string) bool { return strings.EqualFold(k, name) })
}
