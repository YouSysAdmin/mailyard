// Mailyard, Copyright (c) 2021-2026 YouSysAdmin

// Package transport is the delivery leg: how a rendered message leaves
// this installation.
//
// Everything above it - rendering, DKIM, the return path, quotas, the
// email log, failover across a group - is unchanged by which provider
// carries a message, and stays where it is. What this package abstracts
// is the last step, and it exists because that step stopped being one
// thing: an SMTP dial suits most providers and is the wrong shape for
// Amazon SES on an EC2 instance whose IAM role already grants sending.
// There, SMTP means minting credentials and storing a long-lived secret
// to reach a service the machine can already call.
//
// A provider is a file plus one line in builtin(). The registry is an
// explicit list rather than init() registration so the set of providers
// is readable in one place, and so nothing can add one invisibly.
package transport

import (
	"context"
	"crypto/tls"
	"fmt"
	"sort"
	"strings"

	"github.com/yousysadmin/mailyard/internal/core/smtpclient"
)

// Provider ids. Stored in smtp_servers.provider, so these strings are
// data and may not be renamed without a migration.
const (
	// ProviderSMTP is a dial, and the default: an empty column reads as
	// this, which is what keeps every row that predates providers
	// behaving exactly as it did.
	ProviderSMTP = "smtp"

	// ProviderSES is Amazon SES over its own API.
	ProviderSES = "ses"
)

// Spec is what a stored server row says about reaching a provider.
//
// One struct rather than one per provider, because the FAILOVER WALK
// holds a list of candidates that may not agree on their provider and
// has to treat them uniformly. Fields no provider of a given kind reads
// are simply empty - a SES row has no host, an SMTP row has no region.
type Spec struct {
	// Provider is empty for smtp, which is how a column defaulting to
	// 'smtp' and a zero value mean the same thing.
	Provider string

	Host       string
	Port       int
	Encryption string

	// Username and Password carry whatever credential the provider
	// takes: an SMTP login, or a SES access key id and secret.
	//
	// Reusing these two rather than adding provider-specific columns is
	// deliberate. Password is already sealed at rest by core/crypto and
	// already documented as empty-in-empty-out, so a provider that
	// authenticates with a key pair needs no new sealed column and no
	// second code path for secrets.
	Username string
	Password string

	// TLS overrides the connection's TLS settings. Set for a relay node,
	// where the transport IS the authentication. Meaningless to a
	// provider that speaks HTTPS to an API.
	TLS *tls.Config

	// Options are the provider's non-secret settings, from the row's
	// provider_config column: a SES region, a configuration set name.
	//
	// Plain rather than sealed, so the console can show a region without
	// the encryption key - the same split cert_pem and data already use
	// in the certificates table.
	Options map[string]string
}

// Option reads one option, empty when absent.
func (s Spec) Option(key string) string {
	if s.Options == nil {
		return ""
	}

	return s.Options[key]
}

// provider returns the id to dispatch on, treating empty as smtp.
func (s Spec) provider() string {
	if s.Provider == "" {
		return ProviderSMTP
	}

	return s.Provider
}

// Transport delivers messages through one provider.
type Transport interface {
	// Send delivers one message.
	//
	// An error implementing Failure is classified by the caller.
	// Anything else is treated as transient, which is the safe default:
	// a message retried once too often is recoverable, one failed
	// permanently on an error nobody classified is not.
	Send(ctx context.Context, msg *smtpclient.Message) error

	// Test proves the credentials and reachability without sending, so
	// the console button costs nothing and reaches nobody.
	Test(ctx context.Context) error
}

// Failure is a delivery error that knows whether it is worth retrying.
//
// An interface rather than a concrete type, because the two things that
// answer it answer for genuinely different reasons and one of them must
// not pretend to be the other. An SMTP server replies with a three digit
// code at a named stage - SES raises a typed exception and never names a
// recipient. Fabricating an SMTP code for SES would put a number in the
// bounce log that no server ever said.
type Failure interface {
	error

	// Permanent reports that retrying cannot help. The message is
	// failed and, when a recipient is named, a bounce is recorded.
	Permanent() bool

	// RejectedRecipient is the address the provider refused, and is
	// empty unless the provider genuinely named one.
	//
	// Load-bearing emptiness. A recipient here is suppressed for every
	// future send, so a provider that refused the message as a whole
	// must not offer the first address as though it were the cause -
	// that suppresses somebody on no evidence, and the suppression list
	// doubles as an inbound blocklist.
	RejectedRecipient() string
}

// Descriptor describes a provider to the console, so the form is built
// from what the server supports rather than from a copy of it in
// TypeScript - a list that drifts the day a provider is added.
type Descriptor struct {
	ID    string `json:"id"`
	Label string `json:"label"`

	// Dial says the provider connects to a host and port, so the form
	// shows host, port and encryption and validation requires them.
	Dial bool `json:"dial"`

	// Options are the non-secret settings this provider reads.
	Options []OptionField `json:"options,omitempty"`

	// CredentialHint explains what username and password mean here, and
	// what leaving them empty does.
	CredentialHint string `json:"credential_hint,omitempty"`

	// ReSigns says the provider rewrites headers and DKIM-signs the
	// result itself, so a signature applied here cannot survive.
	//
	// A property of the PROVIDER, not a choice on the row. Amazon SES
	// rewrites Date and Message-ID, both of which are in the signed
	// header set, so our signature is guaranteed to arrive broken - and
	// there is no configuration under which that is what somebody wanted.
	// Leaving it as a checkbox meant offering a choice with one correct
	// answer, and the wrong answer merely produced noise nobody could see
	// without reading DMARC reports.
	ReSigns bool `json:"re_signs"`
}

// OptionField is one entry in provider_config.
type OptionField struct {
	Key      string `json:"key"`
	Label    string `json:"label"`
	Required bool   `json:"required"`
	Hint     string `json:"hint,omitempty"`
}

// factory opens one provider from a spec.
type factory func(Spec) (Transport, error)

// builtin is the whole set of providers. Adding one is a file beside
// this and a line here.
func builtin() map[string]factory {
	return map[string]factory{
		ProviderSMTP: openSMTP,
		ProviderSES:  openSES,
	}
}

// descriptors describes each of them for the console.
func descriptors() []Descriptor {
	return []Descriptor{smtpDescriptor(), sesDescriptor()}
}

// Open builds the transport a spec names.
//
// An unknown provider is an error rather than a fall back to smtp. A row
// naming a provider this binary does not have is either a downgrade or a
// typo, and silently dialling its host - which for a SES row is empty -
// would turn that into a connection error naming nothing.
func Open(spec Spec) (Transport, error) {
	open, ok := builtin()[spec.provider()]
	if !ok {
		return nil, fmt.Errorf("unknown mail provider %q", spec.Provider)
	}

	return open(spec)
}

// Known reports whether a provider id exists, for write-side validation.
func Known(provider string) bool {
	if provider == "" {
		return true
	}

	_, ok := builtin()[provider]

	return ok
}

// Providers lists what this binary can send through, in a stable order.
func Providers() []Descriptor {
	out := descriptors()
	sort.Slice(out, func(i, j int) bool { return out[i].ID < out[j].ID })

	return out
}

// ValidateOptions refuses a spec missing an option its provider requires.
//
// Called on the write path, because Open is called on the delivery path
// and that is too late: a SES row with no region was created as enabled,
// looked healthy in the console, and failed every message with an error
// only the log carried. Found by pressing the button.
//
// Declarative, from the descriptor, rather than by opening the transport:
// opening a SES client with no key pair walks the credential chain, which
// can mean an IMDS round trip with a timeout, and a create request should
// not hang on that.
func ValidateOptions(provider string, options map[string]string) error {
	for _, d := range descriptors() {
		if d.ID != provider && !(provider == "" && d.ID == ProviderSMTP) {
			continue
		}

		for _, opt := range d.Options {
			if !opt.Required {
				continue
			}

			if strings.TrimSpace(options[opt.Key]) == "" {
				return fmt.Errorf("%s requires %s", d.Label, opt.Key)
			}
		}

		return nil
	}

	return fmt.Errorf("unknown mail provider %q", provider)
}

// ReSigns reports whether this provider signs the message itself, so our
// own signature must not be applied.
//
// Asked of the provider rather than read off the row, which is what makes
// it unforgeable: an existing row with the flag off, or a PATCH turning
// it off, cannot produce a broken signature. Removing the possibility
// beats adding a refusal - the same reasoning permOnSending rests on.
func ReSigns(provider string) bool {
	for _, d := range descriptors() {
		if d.ID == provider || (provider == "" && d.ID == ProviderSMTP) {
			return d.ReSigns
		}
	}

	return false
}

// Dials reports whether a provider connects to a host and port, which
// is what decides whether host and port are required on the row.
func Dials(provider string) bool {
	for _, d := range descriptors() {
		if d.ID == provider || (provider == "" && d.ID == ProviderSMTP) {
			return d.Dial
		}
	}

	return false
}

// rawMessage renders the bytes a provider transmits.
//
// Build then Sign, in that order and nothing between, because that is
// what an SMTP send does (smtpclient.sendViaClient) and a signature over
// different bytes than the ones sent is worse than no signature: a
// receiver treats a broken one as unsigned but a mismatch is invisible
// until somebody checks DMARC reports.
//
// Shared so a second provider cannot get that order wrong.
func rawMessage(msg *smtpclient.Message) ([]byte, error) {
	raw := msg.Build()
	if msg.Sign == nil {
		return raw, nil
	}

	signed, err := msg.Sign(raw)
	if err != nil {
		return nil, fmt.Errorf("dkim sign: %w", err)
	}

	return signed, nil
}
