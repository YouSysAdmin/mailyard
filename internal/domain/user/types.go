// Mailyard, Copyright (c) 2021-2026 YouSysAdmin

package user

import (
	projmodel "github.com/yousysadmin/mailyard/internal/models/project"
	usermodel "github.com/yousysadmin/mailyard/internal/models/user"
	"strings"
)

// The wire types of this domain: what requests carry in and what
// responses carry out, in one file.
//
// They live here rather than beside the handlers that use them so a
// reader answering "what does this endpoint accept and return" has one
// place to look. The types in internal/models are the stored shapes -
// these are what crosses the wire, and the two are allowed to differ.

// PasskeyResetResponse says how many credentials went, so the console
// can report "3 passkeys removed" rather than a bare success on an
// account that had none.
type PasskeyResetResponse struct {
	Removed int `json:"removed"`
}

// ----------------------------------------------------------------------------
// Requests
// ----------------------------------------------------------------------------

// createInput is the POST /api/users body. Password is optional:
// OIDC-only accounts have none and can only sign in through the IdP.
type createInput struct {
	Email    string `json:"email"      validate:"required,email,max=254"    normalize:"normalize"`
	Password string `json:"password"   validate:"omitempty,notblank,min=12,max=256"`

	// Admin is the whole of platform administration.
	Admin bool `json:"admin"`
}

// updateInput is the PATCH /api/users/:id body. String fields use
// empty = "leave unchanged" - booleans need the pointer to tell
// "absent" apart from "set to false".
type updateInput struct {
	Email    string `json:"email"      validate:"omitempty,email,max=254"   normalize:"normalize"`
	Password string `json:"password"   validate:"omitempty,notblank,min=12,max=256"`
	Admin    *bool  `json:"admin"`
	Disabled *bool  `json:"disabled"`

	// EmailVerified lets an admin unstick a self-registered account
	// that cannot receive the link (or was created while system mail
	// was misconfigured).
	EmailVerified *bool `json:"email_verified"`
}

// ----------------------------------------------------------------------------
// Responses
// ----------------------------------------------------------------------------

// ListResponse is every account on the installation.
type ListResponse struct {
	Users []*usermodel.User `json:"users"`

	// Total counts what the filters match, whether or not a page was
	// asked for.
	Total int `json:"total"`
}

// UserResponse is one account. Password hash and TOTP secret carry
// json:"-" on the model, so neither reaches here.
type UserResponse struct {
	User *usermodel.User `json:"user"`
}

// RevokedResponse reports how many sessions an admin ended.
type RevokedResponse struct {
	Revoked int64 `json:"revoked"`
}

// ProjectsResponse lists what an account touches, so an admin can see
// it before disabling or deleting them.
type ProjectsResponse struct {
	Projects []*projmodel.Project `json:"projects"`
}

// trimPassword trims a password that has content and leaves one made
// only of whitespace as sent, so notblank refuses it rather than a trim
// turning it into "no password".
func trimPassword(p string) string {
	if t := strings.TrimSpace(p); t != "" {
		return t
	}

	return p
}

// Normalize implements validation.Normalizer.
func (in *createInput) Normalize() { in.Password = trimPassword(in.Password) }

// Normalize implements validation.Normalizer.
func (in *updateInput) Normalize() { in.Password = trimPassword(in.Password) }
