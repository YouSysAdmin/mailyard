// Mailyard, Copyright (c) 2021-2026 YouSysAdmin

package sender

import (
	smodel "github.com/yousysadmin/mailyard/internal/models/sender"
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

type createInput struct {
	Email string `json:"email" validate:"required,email,max=254" normalize:"normalize"`
	Name  string `json:"name"  validate:"omitempty,max=100" normalize:"trim"`
}

// signingInput is the POST /senders/:id/signing body. Which fields
// matter depends on kind: a PGP key is generated when nothing is
// given, or imported from private_key. An S/MIME pair arrives as a
// PEM certificate chain plus private_key, or as pkcs12 (base64 of
// the .p12 file). passphrase opens either when it is protected.
type signingInput struct {
	Kind        string `json:"kind"        validate:"required,oneof=pgp smime"`
	Mode        string `json:"mode"        validate:"omitempty,oneof=generate import"`
	PrivateKey  string `json:"private_key" validate:"omitempty,max=65536" normalize:"trim"`
	Certificate string `json:"certificate" validate:"omitempty,max=65536" normalize:"trim"`
	PKCS12      string `json:"pkcs12"      validate:"omitempty,max=131072" normalize:"trim"`
	Passphrase  string `json:"passphrase"  validate:"omitempty,max=1024"`
}

// signingFlagsInput is the PATCH /senders/:id/signing body. Pointers,
// so a field left out keeps its value.
type signingFlagsInput struct {
	Sign      *bool `json:"sign"`
	AttachKey *bool `json:"attach_key"`
}

// ----------------------------------------------------------------------------
// Responses
// ----------------------------------------------------------------------------

// ListResponse is the project's approved From addresses.
// updateInput is the display name. The address is the identity and
// cannot change: a different address is a different sender, registered
// against its own verified domain.
type updateInput struct {
	Name *string `json:"name" validate:"omitzero,max=100"`
}

type ListResponse struct {
	Senders []*smodel.Sender `json:"senders"`
}

// SenderResponse is one approved address.
type SenderResponse struct {
	Sender *smodel.Sender `json:"sender"`
}

// PublicKeyResponse is the public half of a sender's signing key: an
// armored OpenPGP key or a PEM certificate chain, leaf first.
type PublicKeyResponse struct {
	Kind      string `json:"kind"`
	PublicKey string `json:"public_key"`
}
