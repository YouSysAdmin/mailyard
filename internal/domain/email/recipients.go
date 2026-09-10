// Mailyard, Copyright (c) 2021-2026 YouSysAdmin

package email

import (
	"net/mail"
	"strings"

	"github.com/yousysadmin/mailyard/internal/core/smtpclient"
	emailmodel "github.com/yousysadmin/mailyard/internal/models/email"
)

// foldRecipients turns the caller's to, cc and bcc lists into the
// envelope and the two display headers.
//
// Without cc and bcc it hands back to and two empty strings, so a plain
// send is unchanged: Build keeps writing the envelope as the To header
// and the stored header map gains nothing. With either list the To and
// Cc headers are written from the lists the caller put there, address
// by address as written (display name included), and the envelope is
// all three - which is the one arrangement that puts a Bcc recipient
// on RCPT TO and in no header.
//
// The same mailbox in two lists is delivered once, the first occurrence
// wins. Two RCPT TO for one address is a duplicate copy on most
// receivers and a refusal on some.
func foldRecipients(to, cc, bcc []string) (envelope []string, headerTo, headerCc string) {
	if len(cc) == 0 && len(bcc) == 0 {
		return to, "", ""
	}

	seen := make(map[string]struct{}, len(to)+len(cc)+len(bcc))
	envelope = make([]string, 0, len(to)+len(cc)+len(bcc))
	for _, list := range [][]string{to, cc, bcc} {
		for _, addr := range list {
			key := mailboxKey(addr)
			if _, dup := seen[key]; dup {
				continue
			}

			seen[key] = struct{}{}
			envelope = append(envelope, addr)
		}
	}

	return envelope, strings.Join(to, ", "), strings.Join(cc, ", ")
}

// splitRecipients is the inverse of foldRecipients, read off a stored
// row: the To and Cc headers the message displayed, and as Bcc every
// envelope recipient neither header names. Without a display To the
// message was sent with to alone, and the envelope IS the To list.
//
// Matched on the bare mailbox, case-insensitively, so a display name
// on the header copy does not turn its owner into a Bcc recipient. A
// header that does not parse as a list is kept as one entry rather
// than dropped: it is what the recipient's client was handed.
func splitRecipients(e *emailmodel.Email) *Addressing {
	headerTo := e.Headers[HeaderDisplayTo]
	if headerTo == "" {
		return &Addressing{To: e.Recipients, Cc: []string{}, Bcc: []string{}}
	}

	out := &Addressing{
		To:  addressList(headerTo),
		Cc:  addressList(e.Headers[HeaderDisplayCc]),
		Bcc: []string{},
	}
	named := make(map[string]struct{}, len(out.To)+len(out.Cc))
	for _, list := range [][]string{out.To, out.Cc} {
		for _, addr := range list {
			named[mailboxKey(addr)] = struct{}{}
		}
	}

	for _, addr := range e.Recipients {
		if _, ok := named[mailboxKey(addr)]; !ok {
			out.Bcc = append(out.Bcc, addr)
		}
	}

	return out
}

// addressList splits a header value into its addresses, each written
// through the one mailbox formatter, or hands the value back whole when
// it does not parse. Never nil, so the wire carries [] rather than
// null.
func addressList(header string) []string {
	if header == "" {
		return []string{}
	}

	addrs, err := mail.ParseAddressList(header)
	if err != nil {
		return []string{header}
	}

	out := make([]string, 0, len(addrs))
	for _, a := range addrs {
		out = append(out, smtpclient.FormatAddress(a.Name, a.Address))
	}

	return out
}

// mailboxKey is the identity two spellings of one recipient share.
func mailboxKey(addr string) string {
	return strings.ToLower(smtpclient.EnvelopeAddress(addr))
}
