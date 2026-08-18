// Mailyard, Copyright (c) 2021-2026 YouSysAdmin

package language

import (
	lmodel "github.com/yousysadmin/mailyard/internal/models/language"
)

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

type upsertInput struct {
	Code      string `json:"code"       validate:"required,min=2,max=10" normalize:"normalize"`
	Name      string `json:"name"       validate:"required,min=1,max=100" normalize:"trim"`
	IsDefault bool   `json:"is_default"`
}

// ----------------------------------------------------------------------------
// Responses
// ----------------------------------------------------------------------------

// ListResponse is the languages a project's templates may localize to.
type ListResponse struct {
	Languages []*lmodel.Language `json:"languages"`
}

// LanguageResponse is one language.
type LanguageResponse struct {
	Language *lmodel.Language `json:"language"`
}
