// Mailyard, Copyright (c) 2021-2026 YouSysAdmin

package dsn

import (
	"strings"
	"testing"
)

// crlf converts the readable test literals to wire format. Real
// reports arrive with CRLF line endings, and the multipart reader
// cares.
func crlf(s string) []byte {
	return []byte(strings.ReplaceAll(s, "\n", "\r\n"))
}

// A Gmail-shaped hard bounce: per-message group, one failed
// recipient, original headers attached.
const gmailBounce = `From: Mail Delivery Subsystem <mailer-daemon@googlemail.com>
To: bounces@mailyard.example.com
Subject: Delivery Status Notification (Failure)
Message-ID: <bounce123@mail.gmail.com>
Content-Type: multipart/report; report-type=delivery-status; boundary="000000b0a7"

--000000b0a7
Content-Type: text/plain; charset="UTF-8"

Your message wasn't delivered to nobody@gmail.com because the address couldn't be found.

--000000b0a7
Content-Type: message/delivery-status

Reporting-MTA: dns; googlemail.com
Received-From-MTA: dns; smtp.example.com
Arrival-Date: Thu, 06 Aug 2026 07:00:00 -0700 (PDT)

Final-Recipient: rfc822; nobody@gmail.com
Action: failed
Status: 5.1.1
Diagnostic-Code: smtp; 550-5.1.1 The email account that you tried to reach
 does not exist.

--000000b0a7
Content-Type: message/rfc822

From: noreply@example.com
To: nobody@gmail.com
Subject: hello
Message-ID: <original-abc@mailyard.example.com>

body
--000000b0a7--
`

func TestParseGmailHardBounce(t *testing.T) {
	rep, ok := Parse(crlf(gmailBounce))
	if !ok {
		t.Fatal("gmail bounce not recognized")
	}

	if rep.Kind != KindBounce {
		t.Fatalf("kind = %q", rep.Kind)
	}

	if len(rep.Recipients) != 1 {
		t.Fatalf("recipients = %d, want 1", len(rep.Recipients))
	}

	r := rep.Recipients[0]
	if r.Address != "nobody@gmail.com" {
		t.Errorf("address = %q", r.Address)
	}

	if !r.Hard() {
		t.Errorf("5.1.1 failed must be hard, got action=%q status=%q", r.Action, r.Status)
	}

	if !strings.Contains(r.Diagnostic, "does not exist") {
		t.Errorf("diagnostic lost the folded continuation: %q", r.Diagnostic)
	}

	if rep.OriginalMessageID != "original-abc@mailyard.example.com" {
		t.Errorf("original message id = %q", rep.OriginalMessageID)
	}

	if rep.ReportingMTA != "" {
		// Reporting-MTA is "dns; googlemail.com" - addressOf refuses
		// values without @, so this stays empty. Pin that so a future
		// change is a decision, not an accident.
		t.Errorf("reporting mta = %q, want empty for dns-typed value", rep.ReportingMTA)
	}
}

// A soft bounce must be recognized but not classified hard.
const softBounce = `From: postmaster@relay.example.net
To: bounces@mailyard.example.com
Content-Type: multipart/report; report-type=delivery-status; boundary="bb"

--bb
Content-Type: message/delivery-status

Reporting-MTA: dns; relay.example.net

Final-Recipient: rfc822; full@example.org
Action: delayed
Status: 4.2.2
Diagnostic-Code: smtp; 452 mailbox full

--bb--
`

func TestParseSoftBounce(t *testing.T) {
	rep, ok := Parse(crlf(softBounce))
	if !ok {
		t.Fatal("soft bounce not recognized")
	}

	if rep.Recipients[0].Hard() {
		t.Error("a delayed 4.2.2 must not be hard")
	}
}

// An MTA that skips the per-message group entirely: the first group
// already names the recipient.
const bareBounce = `From: postmaster@terse.example
To: bounces@mailyard.example.com
Content-Type: multipart/report; report-type=delivery-status; boundary="cc"

--cc
Content-Type: message/delivery-status

Final-Recipient: rfc822; gone@example.org
Action: failed

--cc--
`

func TestParseBounceWithoutPerMessageGroup(t *testing.T) {
	rep, ok := Parse(crlf(bareBounce))
	if !ok {
		t.Fatal("bare bounce not recognized")
	}

	if len(rep.Recipients) != 1 || rep.Recipients[0].Address != "gone@example.org" {
		t.Fatalf("recipients = %+v", rep.Recipients)
	}

	if !rep.Recipients[0].Hard() {
		t.Error("failed with no status code must be hard")
	}
}

// An ARF complaint (RFC 5965), the shape feedback loops send.
const arfComplaint = `From: complaints@feedback.example.net
To: bounces@mailyard.example.com
Subject: FW: complaint
Content-Type: multipart/report; report-type=feedback-report; boundary="dd"

--dd
Content-Type: text/plain

This is a complaint.

--dd
Content-Type: message/feedback-report

Feedback-Type: abuse
User-Agent: SomeFBL/1.0
Version: 1
Original-Rcpt-To: rfc822; victim@example.org

--dd
Content-Type: message/rfc822

From: noreply@example.com
To: victim@example.org
Message-ID: <orig-777@mailyard.example.com>
Subject: newsletter

body
--dd--
`

func TestParseARFComplaint(t *testing.T) {
	rep, ok := Parse(crlf(arfComplaint))
	if !ok {
		t.Fatal("arf complaint not recognized")
	}

	if rep.Kind != KindComplaint {
		t.Fatalf("kind = %q", rep.Kind)
	}

	r := rep.Recipients[0]
	if r.Address != "victim@example.org" || !r.Hard() {
		t.Errorf("recipient = %+v", r)
	}

	if rep.OriginalMessageID != "orig-777@mailyard.example.com" {
		t.Errorf("original message id = %q", rep.OriginalMessageID)
	}
}

// Ordinary mail, HTML mail, and broken input must all be "not a
// report" - misclassifying real mail as a bounce would suppress
// innocent addresses.
func TestParseRejectsNonReports(t *testing.T) {
	cases := map[string]string{
		"plain":     "From: a@b.c\nTo: d@e.f\nSubject: hi\n\nhello",
		"html":      "From: a@b.c\nContent-Type: text/html\n\n<p>hi</p>",
		"multipart": "From: a@b.c\nContent-Type: multipart/mixed; boundary=\"x\"\n\n--x\nContent-Type: text/plain\n\nhi\n--x--",
		"empty":     "",
		"garbage":   "not a message at all",
	}
	for name, raw := range cases {
		if _, ok := Parse(crlf(raw)); ok {
			t.Errorf("%s misclassified as a report", name)
		}
	}
}

// A report whose delivery-status part yields no recipients is not a
// report worth acting on.
func TestParseEmptyReport(t *testing.T) {
	empty := `From: postmaster@x.example
Content-Type: multipart/report; report-type=delivery-status; boundary="ee"

--ee
Content-Type: message/delivery-status

Reporting-MTA: dns; x.example

--ee--
`
	if _, ok := Parse(crlf(empty)); ok {
		t.Error("recipient-less report must not be actionable")
	}
}
