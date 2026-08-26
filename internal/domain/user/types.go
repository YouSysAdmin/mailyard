// Mailyard, Copyright (c) 2021-2026 YouSysAdmin

package user

import (
	projmodel "github.com/yousysadmin/mailyard/internal/models/project"
	usermodel "github.com/yousysadmin/mailyard/internal/models/user"
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
	Email    string `json:"email"      validate:"required,email,max=320"    normalize:"normalize"`
	Password string `json:"password"   validate:"omitempty,min=12,max=256,bcryptlen"   normalize:"trim"`

	// Admin is the whole of platform administration - it replaced a
	// role string and a super_user boolean that meant the same thing.
	Admin bool `json:"admin"`
}

// updateInput is the PATCH /api/users/:id body. String fields use
// empty = "leave unchanged" - booleans need the pointer to tell
// "absent" apart from "set to false".
type updateInput struct {
	Email    string `json:"email"      validate:"omitempty,email,max=320"   normalize:"normalize"`
	Password string `json:"password"   validate:"omitempty,min=12,max=256,bcryptlen"   normalize:"trim"`
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
