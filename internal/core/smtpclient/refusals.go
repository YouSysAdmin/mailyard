// Mailyard, Copyright (c) 2021-2026 YouSysAdmin

package smtpclient

import (
	"fmt"
	"strings"
)

// RecipientRefusals reports the recipients a server refused for good
// at RCPT TO on a message with more than one recipient, each with its
// own reply. One recipient's 550 says nothing about the others, so the
// message still goes to whoever was accepted.
//
// Delivered says somebody took it. Then this is not a failure of the
// message: it was sent, and only the refused addresses bounced. When
// nobody took it the message failed for good, and every refusal is
// still named.
type RecipientRefusals struct {
	Refusals  []*SendError
	Delivered bool
}

// Error renders the refusals for the email log.
func (r *RecipientRefusals) Error() string {
	parts := make([]string, len(r.Refusals))
	for i, f := range r.Refusals {
		parts[i] = f.Recipient + ": " + f.Error()
	}

	lead := "every recipient was refused"
	if r.Delivered {
		lead = fmt.Sprintf("delivered, but %d recipient(s) were refused", len(r.Refusals))
	}

	return lead + ": " + strings.Join(parts, ", ")
}

// Permanent is true when nobody took the message. A partial delivery
// is not a failure to retry.
func (r *RecipientRefusals) Permanent() bool { return !r.Delivered }

// RejectedRecipient is empty: more than one address may have been
// refused, and Refused names each of them.
func (r *RecipientRefusals) RejectedRecipient() string { return "" }

// Accepted reports whether the message reached anybody.
func (r *RecipientRefusals) Accepted() bool { return r.Delivered }

// Refused lists one error per refused recipient, each naming its
// address through RejectedRecipient.
func (r *RecipientRefusals) Refused() []error {
	out := make([]error, len(r.Refusals))
	for i, f := range r.Refusals {
		out[i] = f
	}

	return out
}
