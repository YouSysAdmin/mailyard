// Mailyard, Copyright (c) 2021-2026 YouSysAdmin

package email

import (
	"slices"
	"strings"
	"testing"

	"github.com/yousysadmin/mailyard/internal/core/smtpclient"
	emailmodel "github.com/yousysadmin/mailyard/internal/models/email"
)

// headerBlock returns the header section of a built message.
func headerBlock(t *testing.T, raw []byte) string {
	t.Helper()
	head, _, ok := strings.Cut(string(raw), "\r\n\r\n")
	if !ok {
		t.Fatalf("no header/body separator in:\n%s", raw)
	}

	return head
}

// buildAsProcessed takes a request the way Send stores it and the
// processor reads it back: the display headers ride the header map
// under the reserved keys and are lifted out before the build. A test
// that skipped the round trip would pass on a request the row cannot
// carry.
func buildAsProcessed(req *SendRequest) []byte {
	headers := withDisplayRecipients(req)
	msg := &smtpclient.Message{
		From:     req.From,
		To:       req.To,
		HeaderTo: headers[HeaderDisplayTo],
		Cc:       headers[HeaderDisplayCc],
		Subject:  req.Subject,
		Text:     req.Text,
	}

	return msg.Build()
}

// A send naming to alone is exactly what it was before cc and bcc
// existed: the envelope is the list and the builder writes it as To.
// The header map stays as the caller sent it, so nothing new is stored
// for the callers that were already here.
func TestAPlainSendIsUnchangedByTheFold(t *testing.T) {
	in := &sendInput{From: "you@example.com", To: []string{"a@example.com", "b@example.com"}, Subject: "s", Text: "t"}
	req, err := in.toRequest()
	if err != nil {
		t.Fatal(err)
	}

	if !slices.Equal(req.To, in.To) || req.HeaderTo != "" || req.Cc != "" {
		t.Fatalf("plain send was rewritten: To=%v HeaderTo=%q Cc=%q", req.To, req.HeaderTo, req.Cc)
	}

	if got := withDisplayRecipients(req); len(got) != 0 {
		t.Fatalf("a plain send stored display headers: %v", got)
	}

	head := headerBlock(t, buildAsProcessed(req))
	if !strings.Contains(head, "To: a@example.com, b@example.com\r\n") {
		t.Fatalf("envelope was not written as To:\n%s", head)
	}
}

// The property the fields exist for. A Bcc recipient is on RCPT TO
// and in no header, To names the to list and nothing else, Cc names
// the cc list - after the request has been through the header map the
// row stores.
func TestABccRecipientIsDeliveredAndNeverDisplayed(t *testing.T) {
	in := &sendInput{
		From:    "you@example.com",
		To:      []string{"Ann <a@example.com>"},
		Cc:      []string{"c@example.com"},
		Bcc:     []string{"hidden@example.com"},
		Subject: "s", Text: "t",
	}
	req, err := in.toRequest()
	if err != nil {
		t.Fatal(err)
	}

	want := []string{"Ann <a@example.com>", "c@example.com", "hidden@example.com"}
	if !slices.Equal(req.To, want) {
		t.Fatalf("envelope = %v, want %v", req.To, want)
	}

	head := headerBlock(t, buildAsProcessed(req))
	if strings.Contains(head, "hidden@example.com") {
		t.Fatalf("a bcc recipient was printed in the headers:\n%s", head)
	}

	if !strings.Contains(head, "To: Ann <a@example.com>\r\n") || !strings.Contains(head, "Cc: c@example.com\r\n") {
		t.Fatalf("To/Cc were not written from the caller's lists:\n%s", head)
	}

	// The sandbox renders the same message, off the request rather
	// than the row, and its raw record is what a developer reads to
	// see what would have gone out.
	if raw := captureMessage(req).Build(); strings.Contains(headerBlock(t, raw), "hidden@example.com") {
		t.Fatalf("the sandbox capture printed a bcc recipient:\n%s", raw)
	}
}

// One RCPT TO per mailbox, whichever lists name it. Matched on the
// address, so a display name on one copy does not make it a second
// recipient.
func TestTheSameMailboxInTwoListsIsDeliveredOnce(t *testing.T) {
	envelope, headerTo, headerCc := foldRecipients(
		[]string{"Ann <a@example.com>", "b@example.com"},
		[]string{"A@EXAMPLE.COM"},
		[]string{"b@example.com", "z@example.com"},
	)
	want := []string{"Ann <a@example.com>", "b@example.com", "z@example.com"}
	if !slices.Equal(envelope, want) {
		t.Fatalf("envelope = %v, want %v", envelope, want)
	}

	// The headers say what the caller wrote, deduplication is an
	// envelope matter.
	if headerTo != "Ann <a@example.com>, b@example.com" || headerCc != "A@EXAMPLE.COM" {
		t.Fatalf("headers = %q / %q", headerTo, headerCc)
	}
}

// Every address in every list is a recipient, so the ceiling and the
// address check see all of them. A refused Bcc header points at the
// field that exists now.
func TestCcAndBccAreValidatedAsRecipients(t *testing.T) {
	svc := &Service{}
	svc.Sending.MaxRecipients = 2

	in := &sendInput{From: "you@example.com", To: []string{"a@example.com"}, Bcc: []string{"not an address"}, Subject: "s", Text: "t"}
	req, _ := in.toRequest()
	if err := svc.ValidateShape(req); err == nil || !strings.Contains(err.Error(), "not an address") {
		t.Fatalf("an unparseable bcc address was accepted: %v", err)
	}

	in = &sendInput{From: "you@example.com", To: []string{"a@example.com"}, Cc: []string{"b@example.com"}, Bcc: []string{"c@example.com"}, Subject: "s", Text: "t"}
	req, _ = in.toRequest()
	if err := svc.ValidateShape(req); err == nil || !strings.Contains(err.Error(), "too many recipients") {
		t.Fatalf("the ceiling did not count cc and bcc: %v", err)
	}

	in = &sendInput{From: "you@example.com", To: []string{"a@example.com"}, Subject: "s", Text: "t", Headers: map[string]string{"Bcc": "x@example.com"}}
	req, _ = in.toRequest()
	if err := svc.ValidateShape(req); err == nil || !strings.Contains(err.Error(), "use bcc") {
		t.Fatalf("a Bcc header refusal does not name the field: %v", err)
	}
}

// The log shows who was named and who only received, read back off the
// row: the display headers are the named lists, the rest of the
// envelope is Bcc. A row with no display To was sent with to alone.
func TestTheLogSplitsTheEnvelopeBackIntoLists(t *testing.T) {
	plain := &emailmodel.Email{Recipients: []string{"a@example.com", "b@example.com"}}
	got := splitRecipients(plain)
	if !slices.Equal(got.To, plain.Recipients) || len(got.Cc) != 0 || len(got.Bcc) != 0 {
		t.Fatalf("plain send split = %+v", got)
	}

	if got.Cc == nil || got.Bcc == nil {
		t.Fatal("an empty list must be [] on the wire, not null")
	}

	in := &sendInput{
		From:    "you@example.com",
		To:      []string{"Ann <a@example.com>"},
		Cc:      []string{"c@example.com"},
		Bcc:     []string{"hidden@example.com", "Two <h2@example.com>"},
		Subject: "s", Text: "t",
	}
	req, _ := in.toRequest()
	row := &emailmodel.Email{Recipients: req.To, Headers: withDisplayRecipients(req)}
	got = splitRecipients(row)
	if !slices.Equal(got.To, []string{`"Ann" <a@example.com>`}) {
		t.Errorf("To = %v", got.To)
	}

	if !slices.Equal(got.Cc, []string{"c@example.com"}) {
		t.Errorf("Cc = %v", got.Cc)
	}

	if !slices.Equal(got.Bcc, []string{"hidden@example.com", "Two <h2@example.com>"}) {
		t.Errorf("Bcc = %v", got.Bcc)
	}

	// A header copy spelled differently from the envelope is the same
	// mailbox, and a header naming somebody the envelope skipped is
	// shown as written, not invented into a delivery.
	sub := &emailmodel.Email{
		Recipients: []string{"a@example.com", "bcc@example.com"},
		Headers:    map[string]string{HeaderDisplayTo: "A@Example.com, left-out@example.com"},
	}
	got = splitRecipients(sub)
	if !slices.Equal(got.To, []string{"A@Example.com", "left-out@example.com"}) || !slices.Equal(got.Bcc, []string{"bcc@example.com"}) {
		t.Errorf("submission split = %+v", got)
	}
}
