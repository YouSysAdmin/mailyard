// Mailyard, Copyright (c) 2021-2026 YouSysAdmin

package certificate

import (
	"github.com/yousysadmin/mailyard/internal/core/certgen"
	certmodel "github.com/yousysadmin/mailyard/internal/models/certificate"
)

// ----------------------------------------------------------------------------
// Requests
// ----------------------------------------------------------------------------

// subjectFields are the Subject an operator fills in.
//
// Embedded in both generate requests rather than repeated, because a
// certificate authority and a server certificate ask for exactly the
// same identity - they differ in what they are for, not in who they
// say they belong to.
//
// The lengths are X.520 limits. Encoding a longer value produces a
// distinguished name some verifiers reject, which is a failure that
// shows up as a handshake error naming nothing.
type subjectFields struct {
	// CommonName defaults to the first host for a leaf, and is required
	// for an authority, which has no host to fall back on.
	CommonName   string `json:"common_name" validate:"omitempty,max=64" normalize:"trim"`
	Organization string `json:"organization" validate:"omitempty,max=64" normalize:"trim"`
	Unit         string `json:"unit" validate:"omitempty,max=64" normalize:"trim"`

	// Country is the two-letter code and nothing else. A longer value
	// encodes as an invalid attribute rather than a wrong one.
	Country  string `json:"country" validate:"omitempty,len=2,alpha" normalize:"trim,upper"`
	State    string `json:"state" validate:"omitempty,max=64" normalize:"trim"`
	Locality string `json:"locality" validate:"omitempty,max=64" normalize:"trim"`
}

func (s subjectFields) subject() certgen.Subject {
	return certgen.Subject{
		CommonName:   s.CommonName,
		Organization: s.Organization,
		Unit:         s.Unit,
		Country:      s.Country,
		State:        s.State,
		Locality:     s.Locality,
	}
}

// uploadInput is a certificate an administrator already has. The key
// is write-only: it is sealed on arrival and no route ever returns it.
type uploadInput struct {
	Name string `json:"name" validate:"required,min=1,max=100,certname" normalize:"trim"`

	// CertPEM may carry the whole chain. Order matters to a client -
	// leaf first, then intermediates - and it is stored verbatim.
	CertPEM string `json:"certificate" validate:"required,min=1,max=131072"`
	KeyPEM  string `json:"private_key" validate:"required,min=1,max=131072"`
}

// generateInput asks the server to mint a certificate that serves TLS,
// self-signed or signed by one of this installation's own authorities.
type generateInput struct {
	Name    string        `json:"name"  validate:"required,min=1,max=100,certname" normalize:"trim"`
	Hosts   []string      `json:"hosts" validate:"required,min=1,max=50,dive,required,min=1,max=253"`
	Subject subjectFields `json:"subject"`

	// Algorithm is rsa, ecdsa or ed25519. Empty means rsa, which is
	// what this endpoint has always produced.
	Algorithm string `json:"algorithm" validate:"omitempty,oneof=rsa ecdsa ed25519"`

	// Issuer names a stored certificate authority to sign with. Empty
	// means self-signed, which is what this endpoint did before
	// authorities existed.
	Issuer string `json:"issuer" validate:"omitempty,min=1,max=100,certname" normalize:"trim"`

	// ValidityDays is capped at maxLeafValidityDays. Zero takes the
	// default.
	ValidityDays int `json:"validity_days" validate:"omitempty,min=1,max=398"`
}

// generateCARequest asks for an authority: something whose only job is
// to sign the certificates above, so that one certificate goes into a
// client's trust store instead of one per listener.
//
// No hosts, unlike a leaf. An authority is a trust anchor and does not
// serve a name, which is also why a listener assigned one refuses
// every client.
type generateCAInput struct {
	Name    string        `json:"name" validate:"required,min=1,max=100,certname" normalize:"trim"`
	Subject subjectFields `json:"subject"`

	// Algorithm is rsa or ecdsa. Not ed25519: several trust stores
	// refuse to install such a root, and a root nobody can install is
	// no use.
	Algorithm string `json:"algorithm" validate:"omitempty,oneof=rsa ecdsa"`

	// ValidityDays defaults to defaultCAValidityDays. An authority is
	// not bound by the 398-day rule that applies to what it signs.
	ValidityDays int `json:"validity_days" validate:"omitempty,min=1,max=7300"`
}

// ----------------------------------------------------------------------------
// Responses
// ----------------------------------------------------------------------------

// Managed is one administrator-managed certificate, with what it says
// about itself. The private half is never part of this.
type Managed struct {
	Name string `json:"name"`

	// Details is nil when the stored certificate will not parse,
	// which the console shows as a problem rather than an empty row.
	Details *certmodel.Details `json:"details,omitempty"`

	// UsedBy names the listeners actually SERVING it, so a delete can
	// say what it would break.
	UsedBy []string `json:"used_by,omitempty"`

	// Dormant names listeners assigned to it that do not terminate TLS,
	// so the assignment is recorded and nothing presents it.
	//
	// Separate from UsedBy because merging them was a lie the console
	// repeated: it showed a certificate as in use while openssl showed
	// the listener speaking plaintext, and the delete was refused on the
	// strength of it.
	Dormant   []string `json:"dormant,omitempty"`
	CreatedAt string   `json:"created_at"`
	UpdatedAt string   `json:"updated_at"`
}

// The step of the chain a listener is actually presenting.
const (
	// ServingManaged is an assigned certificate, the only step an
	// administrator chooses.
	ServingManaged = "managed"

	// ServingACME is a certificate from the CA, for the configured hosts
	// that have one cached. Never assignable: it is resolved per
	// handshake from the SNI name, so pinning one to a listener would
	// break every other name that listener answers for.
	ServingACME = "acme"

	// ServingSelfSigned is the last step, which needs no configuration
	// and is why a listener always has something to present.
	ServingSelfSigned = "selfsigned"

	// ServingNone is a listener that does not terminate TLS.
	ServingNone = "none"
)

// ListenerState is what one listener presents RIGHT NOW, resolved down
// the same chain a handshake walks: assigned certificate, then ACME,
// then the self-signed pair.
//
// It exists because the page could only report the ASSIGNMENT, and the
// assignment is empty in the ordinary successful case. An operator who
// turned ACME on, ordered for their host and got it read "Nothing
// assigned" on every listener, with an empty Managed table and no way
// to select the certificate they had just been issued - while that
// certificate was on the wire. Everything was working and the only page
// that could say so said nothing.
//
// Reported by the server rather than assembled in the console, for the
// same reason TerminatesTLS is one function: a second copy of the chain
// in TypeScript is a second answer, and it would be wrong in exactly
// the cases that matter - an assigned row that has been deleted, a
// configured host with no certificate yet.
type ListenerState struct {
	Listener string `json:"listener"`

	// TLS is whether it terminates TLS at all, from
	// <listener>.tls.enabled. A config fact rather than a stored one: it
	// is a port-level decision, and the common deployment terminates TLS
	// at a proxy in front.
	TLS bool `json:"tls"`

	// Assigned is the managed certificate pointed at this listener,
	// empty when it falls through. Still reported separately from
	// Serving, because an assignment that is not being served is the
	// interesting case - a deleted row, or a listener with no handshake.
	Assigned string `json:"assigned,omitempty"`
	Serving  string `json:"serving"`

	// ServingNames are the names Serving covers. One for a managed
	// certificate, the configured hosts holding a cached certificate for
	// ACME, and the derived host for the self-signed pair.
	//
	// A LIST because ACME is per-SNI: a listener can serve a real
	// certificate for one name and the self-signed pair for the next
	// connection, and a single string would have to pick one and lie
	// about the other.
	ServingNames []string `json:"serving_names,omitempty"`

	// Fallback is what this listener presents when nothing is assigned -
	// acme when a configured host has a certificate cached, selfsigned
	// otherwise. Equal to Serving unless a managed certificate is
	// assigned and servable.
	//
	// Reported separately because it is what the unassigned choice in
	// the console has to be LABELLED with. "Nothing assigned (ACME, then
	// self-signed)" described the mechanism and named nothing, so the one
	// certificate an operator had just been issued appeared in no
	// selector on the page - not as an option, not as an answer. This
	// makes that option read "Automatic - Let's Encrypt: mail.faria.org".
	Fallback      string   `json:"fallback"`
	FallbackNames []string `json:"fallback_names,omitempty"`
}

// ListResponse is the managed set plus what each listener presents.
type ListResponse struct {
	Certificates []Managed `json:"certificates"`

	// Listeners replaced an assignments map plus a list of the listeners
	// with TLS off. Three fields describing one row, of which the two
	// that were there could not answer what the row was actually
	// serving.
	Listeners []ListenerState `json:"listeners"`
}

// ManagedResponse returns one certificate.
type ManagedResponse struct {
	Certificate Managed `json:"certificate"`
}

// SystemResponse lists the certificates the INSTALLATION holds rather
// than ones an admin uploaded: the ACME cache, the self-signed pair,
// the relay authority. Read-only, and the reason the expiry page can
// answer "what is about to break" rather than "what did you upload".
type SystemResponse struct {
	Certificates []System `json:"certificates"`
}

// System is one such entry.
type System struct {
	Scope   string             `json:"scope"`
	Name    string             `json:"name"`
	Details *certmodel.Details `json:"details,omitempty"`
}

// renewInput names the ACME host to reissue.
type renewInput struct {
	Host string `json:"host" validate:"required,hostname_rfc1123"`
}

// ACMEResponse is what ACME is configured to do and what it holds.
type ACMEResponse struct {
	// Enabled reflects the acme_enabled setting, so the page can explain
	// why an empty host list means "not turned on" rather than "none
	// yet".
	Enabled bool `json:"enabled"`

	// Email and DirectoryURL are echoed so the page does not have to read
	// the settings endpoint as well.
	Email        string `json:"email"`
	DirectoryURL string `json:"directory_url"`

	// Staging says the directory is not Let's Encrypt production, so the
	// console can mark what it issues as untrusted on purpose.
	Staging bool       `json:"staging"`
	Hosts   []ACMEHost `json:"hosts"`

	// Suggested is a hostname worth adding that is not configured yet -
	// the one in server.public_url. Empty when it is already listed, or
	// when no public URL is set.
	//
	// A suggestion rather than a default: ordering is an outbound call
	// against a rate limit, so what gets asked for is named by a person.
	Suggested string `json:"suggested,omitempty"`

	// ChallengeAddr reports the HTTP-01 listener, empty when validation
	// happens over tls-alpn-01 and no second port is involved.
	ChallengeAddr string `json:"challenge_addr,omitempty"`

	// TLSTerminatedHere says whether the HTTP listener does its own
	// handshake. Without it, tls-alpn-01 cannot work and http-01 is the
	// only route - which is the difference between "just press Order" and
	// "you also need port 80".
	TLSTerminatedHere bool `json:"tls_terminated_here"`
}

// ACMEHost is one configured hostname and the certificate cached for
// it, absent until the first issuance succeeds.
type ACMEHost struct {
	Host    string             `json:"host"`
	Details *certmodel.Details `json:"details,omitempty"`
}

// MessageResponse is a plain acknowledgement.
type MessageResponse struct {
	Message string `json:"message"`
}

// PEMResponse hands back the PUBLIC half of a certificate, which is
// how an operator gets an authority out of here and into their
// clients' trust stores.
//
// JSON rather than a file download, deliberately. Every /api/v1 route
// is described in OpenAPI and generates a method in three SDKs, and a
// documented route whose body is not JSON makes the generated Go
// client try to unmarshal a PEM. The console turns this into a
// download itself.
type PEMResponse struct {
	Name string `json:"name"`
	PEM  string `json:"pem"`
}
