// Mailyard, Copyright (c) 2021-2026 YouSysAdmin

package data

// The wire types of this domain: what requests carry in and what
// responses carry out, in one file.
//
// They live here rather than beside the handlers that use them so a
// reader answering "what does this endpoint accept and return" has one
// place to look. The types in internal/models are the stored shapes -
// these are what crosses the wire, and the two are allowed to differ.

// ----------------------------------------------------------------------------
// Requests
// ----------------------------------------------------------------------------

// deleteContactsInput names one address, or every contact when empty.
type deleteContactsInput struct {
	Email string `json:"email" validate:"omitempty,email,max=320" normalize:"normalize"`

	// ConfirmAll must be true to erase every contact. Without it an
	// empty body is treated as a mistake rather than as consent to
	// delete the whole project's contact history.
	ConfirmAll bool `json:"confirm_all"`
}

// deleteLogsInput bounds an email log erase by age.
type deleteLogsInput struct {
	// OlderThanDays keeps anything newer. Zero means every row, which
	// requires ConfirmAll.
	OlderThanDays int `json:"older_than_days" validate:"omitempty,min=0,max=36500"`

	// Email narrows to erase to one recipient or sender.
	Email      string `json:"email" validate:"omitempty,email,max=320" normalize:"normalize"`
	ConfirmAll bool   `json:"confirm_all"`
}

// ----------------------------------------------------------------------------
// Responses
// ----------------------------------------------------------------------------

// ExportResponse wraps the project snapshot.
//
// The sections inside Export are []any because they are composed from
// the domain stores directly - that composition is what stops the
// export drifting from the API. What guarantees no secret appears in
// it is the models carrying json:"-", not anything here.
type ExportResponse struct {
	Export *Export `json:"export"`
}

// ErasureResponse reports what a deletion removed.
//
// Message is phrased for a person: erasure is usually run because
// somebody asked for it in writing, and the operator answering them
// needs a sentence rather than a count. Rows in flight are never
// touched, so a number lower than expected is not a failure.
type ErasureResponse struct {
	Deleted int64  `json:"deleted"`
	Message string `json:"message"`
}
