// Mailyard, Copyright (c) 2021-2026 YouSysAdmin

// Package dsn recognizes and parses the two machine-readable failure
// reports that come back to an envelope sender: delivery status
// notifications (RFC 3464, bounces) and abuse feedback reports
// (RFC 5965, complaints).
//
// It exists so the inbound pipeline can turn a report addressed to
// the project's bounce address into bounce records and
// suppressions instead of filing it as just another message. Parsing
// is deliberately lenient: real MTAs disagree on details, and a
// report that yields SOME recipients is worth more than an error.
package dsn

import (
	"bufio"
	"bytes"
	"io"
	"mime"
	"mime/multipart"
	"net/mail"
	"net/textproto"
	"strings"
)

// Report kinds.
const (
	KindBounce    = "bounce"
	KindComplaint = "complaint"
)

// Recipient is one failed (or complained-about) address in a report.
type Recipient struct {
	// Address is the recipient the original message failed for.
	Address string

	// Action is the DSN action field lower-cased: failed, delayed,
	// delivered, relayed, expanded. Complaints have no action and
	// carry "complaint".
	Action string

	// Status is the RFC 3463 code, like "5.1.1". Empty on complaints.
	Status string

	// Diagnostic is the human-readable reason, usually the remote
	// server's SMTP reply.
	Diagnostic string
}

// Hard reports whether this recipient's failure is permanent: a
// failed action with a 5.x.x status (or no status at all - an MTA
// that says "failed" without a code still means it).
func (r Recipient) Hard() bool {
	if r.Action == "complaint" {
		return true
	}

	if r.Action != "failed" {
		return false
	}

	return r.Status == "" || strings.HasPrefix(r.Status, "5")
}

// Report is one parsed failure report.
type Report struct {
	Kind string

	// Recipients lists the addresses the report is about. Bounces from
	// well-behaved MTAs carry one per-recipient block per failed
	// address - complaints usually name one.
	Recipients []Recipient

	// OriginalMessageID is the Message-ID of the message the report
	// concerns, when the report included the original headers. With
	// brackets stripped.
	OriginalMessageID string

	// ReportingMTA names who generated the report, when stated.
	ReportingMTA string

	// OriginalHeaders are the headers of the message the report
	// concerns, keyed lowercase, when the report returned them.
	//
	// RFC 3464 says a DSN SHOULD carry the original message or at
	// least its headers, and this is what makes a report attributable:
	// the sender stamped an identifier there, and it comes back.
	// Message-ID is not enough on its own - providers rewrite it.
	OriginalHeaders map[string]string
}

// Parse inspects a raw RFC 5322 message and, when it is a DSN or an
// ARF report, extracts the failure data. ok=false means "not a
// report" - a perfectly normal message the caller stores as usual.
func Parse(raw []byte) (*Report, bool) {
	msg, err := mail.ReadMessage(bytes.NewReader(raw))
	if err != nil {
		return nil, false
	}

	ct := msg.Header.Get("Content-Type")
	mediaType, params, err := mime.ParseMediaType(ct)
	if err != nil || !strings.EqualFold(mediaType, "multipart/report") {
		return nil, false
	}

	reportType := strings.ToLower(params["report-type"])
	boundary := params["boundary"]
	if boundary == "" {
		return nil, false
	}

	switch reportType {
	case "delivery-status":
		return parseParts(msg.Body, boundary, "message/delivery-status", parseDeliveryStatus)
	case "feedback-report":
		return parseParts(msg.Body, boundary, "message/feedback-report", parseFeedbackReport)
	default:
		return nil, false
	}
}

// parseParts walks the multipart/report container, feeding the
// machine-readable part to parse and pulling the original Message-ID
// out of any message/rfc822 (or text/rfc822-headers) part.
func parseParts(body io.Reader, boundary, wantType string, parse func([]byte) *Report) (*Report, bool) {
	mr := multipart.NewReader(body, boundary)
	var report *Report
	var origID string
	var origHeaders map[string]string
	for {
		part, err := mr.NextPart()
		if err != nil {
			break
		}

		ct, _, _ := mime.ParseMediaType(part.Header.Get("Content-Type"))
		ct = strings.ToLower(ct)
		// Reports are small by construction, but an adversarial or
		// broken sender can put anything in a part, so cap the read.
		content, err := io.ReadAll(io.LimitReader(part, 1<<20))
		if err != nil {
			continue
		}

		switch ct {
		case wantType:
			report = parse(content)
		case "message/rfc822", "text/rfc822-headers":
			if h := headersOf(content); h != nil {
				origHeaders = h
				origID = strings.Trim(strings.TrimSpace(h["message-id"]), "<>")
			}
		}
	}

	if report == nil || len(report.Recipients) == 0 {
		return nil, false
	}

	report.OriginalMessageID = origID
	report.OriginalHeaders = origHeaders

	return report, true
}

// parseDeliveryStatus reads the message/delivery-status body: one
// per-message header group followed by one group per recipient,
// separated by blank lines (RFC 3464 section 2).
func parseDeliveryStatus(content []byte) *Report {
	rep := &Report{Kind: KindBounce}
	for i, group := range splitGroups(content) {
		h := readHeaderGroup(group)
		if i == 0 {
			// The per-message group. Some MTAs skip it, in which case
			// this is already a per-recipient group - detected by it
			// naming a recipient.
			rep.ReportingMTA = addressOf(h.Get("Reporting-MTA"))
			if h.Get("Final-Recipient") == "" && h.Get("Original-Recipient") == "" {
				continue
			}
		}

		addr := addressOf(h.Get("Final-Recipient"))
		if addr == "" {
			addr = addressOf(h.Get("Original-Recipient"))
		}
		if addr == "" {
			continue
		}

		rep.Recipients = append(rep.Recipients, Recipient{
			Address:    strings.ToLower(addr),
			Action:     strings.ToLower(strings.TrimSpace(h.Get("Action"))),
			Status:     strings.TrimSpace(h.Get("Status")),
			Diagnostic: cleanDiagnostic(h.Get("Diagnostic-Code")),
		})
	}

	return rep
}

// parseFeedbackReport reads the message/feedback-report body
// (RFC 5965): a single header group naming the complained-about
// recipient.
func parseFeedbackReport(content []byte) *Report {
	rep := &Report{Kind: KindComplaint}
	for _, group := range splitGroups(content) {
		h := readHeaderGroup(group)
		addr := addressOf(h.Get("Original-Rcpt-To"))
		if addr == "" {
			addr = addressOf(h.Get("Removal-Recipient"))
		}
		if addr == "" {
			continue
		}

		fbType := strings.ToLower(strings.TrimSpace(h.Get("Feedback-Type")))
		if fbType == "" {
			fbType = "abuse"
		}

		rep.Recipients = append(rep.Recipients, Recipient{
			Address:    strings.ToLower(addr),
			Action:     "complaint",
			Diagnostic: "feedback-type: " + fbType,
		})
	}

	return rep
}

// splitGroups divides a delivery-status body into blank-line-separated
// header groups, unfolding continuation lines.
func splitGroups(content []byte) [][]byte {
	normalized := bytes.ReplaceAll(content, []byte("\r\n"), []byte("\n"))
	var groups [][]byte
	for g := range bytes.SplitSeq(normalized, []byte("\n\n")) {
		if len(bytes.TrimSpace(g)) > 0 {
			groups = append(groups, g)
		}
	}

	return groups
}

// readHeaderGroup parses one group of Key: value lines leniently.
func readHeaderGroup(group []byte) textproto.MIMEHeader {
	h := textproto.MIMEHeader{}
	var lastKey string
	sc := bufio.NewScanner(bytes.NewReader(group))
	sc.Buffer(make([]byte, 0, 64*1024), 64*1024)
	for sc.Scan() {
		line := sc.Text()
		if line == "" {
			continue
		}

		// Folded continuation of the previous field.
		if (strings.HasPrefix(line, " ") || strings.HasPrefix(line, "\t")) && lastKey != "" {
			h.Set(lastKey, h.Get(lastKey)+" "+strings.TrimSpace(line))
			continue
		}

		key, value, found := strings.Cut(line, ":")
		if !found {
			continue
		}

		lastKey = textproto.CanonicalMIMEHeaderKey(strings.TrimSpace(key))
		h.Set(lastKey, strings.TrimSpace(value))
	}

	return h
}

// addressOf extracts the address from a typed DSN field like
// "rfc822; user@example.com" (the type prefix is optional in the
// wild). Angle brackets are stripped.
func addressOf(v string) string {
	v = strings.TrimSpace(v)
	if v == "" {
		return ""
	}

	if _, rest, found := strings.Cut(v, ";"); found {
		v = rest
	}

	v = strings.Trim(strings.TrimSpace(v), "<>")
	if !strings.Contains(v, "@") {
		return ""
	}

	return v
}

// cleanDiagnostic strips the "smtp;" type prefix and collapses the
// whitespace folding leaves behind.
func cleanDiagnostic(v string) string {
	if _, rest, found := strings.Cut(v, ";"); found {
		v = rest
	}

	return strings.Join(strings.Fields(v), " ")
}

// headersOf reads the headers of an embedded original message, which
// arrives either as the full message/rfc822 or as its headers alone.
// Keys are lowercased so a caller does not have to guess how the
// reporting MTA capitalized them.
func headersOf(content []byte) map[string]string {
	msg, err := mail.ReadMessage(bytes.NewReader(append(content, '\n')))
	if err != nil {
		return nil
	}

	out := make(map[string]string, len(msg.Header))
	for k, v := range msg.Header {
		if len(v) > 0 {
			out[strings.ToLower(k)] = v[0]
		}
	}

	return out
}
