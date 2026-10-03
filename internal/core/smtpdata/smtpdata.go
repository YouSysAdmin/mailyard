// Mailyard, Copyright (c) 2021-2026 YouSysAdmin

// Package smtpdata answers a failure reading a DATA stream, for every
// SMTP listener this binary runs: submission, the MX and both relay
// node listeners.
package smtpdata

import (
	"errors"

	"github.com/emersion/go-smtp"
)

// ReadError is the reply for err, returned while reading DATA.
//
// A message over the size limit or with a line over the line limit is
// refused permanently, since sending it again cannot succeed and a 4xx
// would have the client retry it for days. Anything else is a real
// read failure and stays temporary.
func ReadError(err error) *smtp.SMTPError {
	if se, ok := errors.AsType[*smtp.SMTPError](err); ok && !se.Temporary() {
		return se
	}

	if errors.Is(err, smtp.ErrTooLongLine) {
		return &smtp.SMTPError{Code: 554, EnhancedCode: smtp.EnhancedCode{5, 6, 0}, Message: "message has a line longer than the limit"}
	}

	return &smtp.SMTPError{Code: 451, EnhancedCode: smtp.EnhancedCode{4, 3, 0}, Message: "read error"}
}
