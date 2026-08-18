// Mailyard, Copyright (c) 2021-2026 YouSysAdmin

package smtpserver

import (
	"time"

	"github.com/yousysadmin/mailyard/internal/core/transport"
	smodel "github.com/yousysadmin/mailyard/internal/models/smtpserver"
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

// createInput is the POST body. Password is optional (open relays or
// IP-authenticated servers have none).
type createInput struct {
	Name string `json:"name" validate:"required,min=1,max=100" normalize:"trim"`

	// Provider is how the row is reached. Empty means smtp, so an
	// existing client that never heard of providers keeps working.
	Provider string `json:"provider" validate:"omitempty,provider" normalize:"trim,lower"`

	// Host and Port are required only for a provider that DIALS. A row
	// reached through an API has neither, and demanding a hostname for
	// one would mean inventing a placeholder that a later change reads
	// as real.
	Host       string `json:"host"           validate:"required_if=Provider smtp,omitempty,hostname_rfc1123" normalize:"normalize"`
	Port       int    `json:"port"           validate:"required_if=Provider smtp,omitempty,min=1,max=65535"`
	Username   string `json:"username"       validate:"omitempty,max=320"      normalize:"trim"`
	Password   string `json:"password"       validate:"omitempty,max=1024"`
	Encryption string `json:"encryption"     validate:"omitempty,oneof=none starttls ssl"`
	SkipDKIM   bool   `json:"skip_dkim"`

	// ProviderConfig is the provider's non-secret settings. Validated by
	// the provider itself when the transport is opened, not here - the
	// set of keys belongs to the provider, and a list of them repeated
	// in a validation tag is the copy that drifts.
	ProviderConfig map[string]string `json:"provider_config" validate:"omitempty,max=20"`

	// SESTopicARN is where Amazon SES publishes this server's bounce
	// and complaint notifications. Only meaningful for SES.
	SESTopicARN   string   `json:"ses_topic_arn" validate:"omitempty,max=512" normalize:"trim"`
	AllowedEmails []string `json:"allowed_emails" validate:"omitempty,dive,min=3,max=320"`

	// GroupID places the server in a pool. Empty joins the project's
	// default group, which is what every server did before groups
	// existed.
	GroupID  string `json:"group_id" validate:"omitempty,max=64"`
	Priority int    `json:"priority" validate:"omitempty,min=0,max=10000"`
}

// updateInput is the PATCH body. Empty strings leave the field
// unchanged, so a password can be rotated but never blanked through
// PATCH (delete and recreate the server for that).
type updateInput struct {
	Name          string    `json:"name"           validate:"omitempty,min=1,max=100" normalize:"trim"`
	Host          string    `json:"host"           validate:"omitempty,hostname_rfc1123"  normalize:"normalize"`
	Port          int       `json:"port"           validate:"omitempty,min=1,max=65535"`
	Username      *string   `json:"username"       validate:"omitzero,max=320"`
	Password      *string   `json:"password"       validate:"omitzero,max=1024"`
	Encryption    string    `json:"encryption"     validate:"omitempty,oneof=none starttls ssl"`
	SkipDKIM      *bool     `json:"skip_dkim"`
	SESTopicARN   *string   `json:"ses_topic_arn" validate:"omitzero,max=512" normalize:"trim"`
	AllowedEmails *[]string `json:"allowed_emails" validate:"omitzero,dive,min=3,max=320"`
	GroupID       string    `json:"group_id" validate:"omitempty,max=64"`
	Priority      *int      `json:"priority" validate:"omitzero,min=0,max=10000"`

	// Provider is not patchable, deliberately. Switching a live row from
	// a dial to an API leaves the credentials meaning something else -
	// an SMTP login read as an access key - and the fields that are now
	// wrong are the ones a PATCH leaves unchanged. Delete and recreate,
	// which is one action and says what is happening.
	//
	// ProviderConfig IS patchable: a region or a configuration set is
	// exactly the thing an operator corrects.
	ProviderConfig *map[string]string `json:"provider_config" validate:"omitzero,max=20"`
}

type groupCreateInput struct {
	Name string `json:"name" validate:"required,min=1,max=100" normalize:"trim"`

	// Slug is what a send names. Derived from the name when omitted.
	Slug        string `json:"slug"        validate:"omitempty,min=1,max=100" normalize:"normalize"`
	Description string `json:"description" validate:"omitempty,max=500"      normalize:"trim"`
}

type groupUpdateInput struct {
	Name        string  `json:"name"        validate:"omitempty,min=1,max=100" normalize:"trim"`
	Slug        string  `json:"slug"        validate:"omitempty,min=1,max=100" normalize:"normalize"`
	Description *string `json:"description" validate:"omitzero,max=500"`

	// MakeDefault promotes this group. There is no way to UNSET the
	// flag: a project must always have exactly one default, so the
	// only legal move is to hand it to another group.
	MakeDefault bool `json:"make_default"`
}

type sharedCreateInput struct {
	Name string `json:"name" validate:"required,min=1,max=100" normalize:"trim"`

	// Provider, and host/port required only when it dials - see createInput for why.
	Provider   string `json:"provider" validate:"omitempty,provider" normalize:"trim,lower"`
	Host       string `json:"host"            validate:"required_if=Provider smtp,omitempty,hostname_rfc1123" normalize:"normalize"`
	Port       int    `json:"port"            validate:"required_if=Provider smtp,omitempty,min=1,max=65535"`
	Username   string `json:"username"        validate:"omitempty,max=320"        normalize:"trim"`
	Password   string `json:"password"        validate:"omitempty,max=1024"`
	Encryption string `json:"encryption"      validate:"omitempty,oneof=none starttls ssl"`
	SkipDKIM   bool   `json:"skip_dkim"`

	// SESTopicARN is where Amazon SES publishes this server's bounce
	// and complaint notifications. Only meaningful for SES.
	SESTopicARN    string   `json:"ses_topic_arn" validate:"omitempty,max=512" normalize:"trim"`
	AllowedEmails  []string `json:"allowed_emails"  validate:"omitempty,dive,min=3,max=320"`
	AllowedDomains []string `json:"allowed_domains" validate:"omitempty,dive,min=1,max=253"`
	SecurityMode   string   `json:"security_mode"   validate:"omitempty,oneof=permissive strict"`
	Priority       int      `json:"priority"        validate:"omitempty,min=0,max=10000"`

	// PlatformOnly reserves this server for the platform's own mail
	// and keeps every tenant off it.
	PlatformOnly   bool              `json:"platform_only"`
	ProviderConfig map[string]string `json:"provider_config" validate:"omitempty,max=20"`
}

type sharedUpdateInput struct {
	Name           string    `json:"name"            validate:"omitempty,min=1,max=100"   normalize:"trim"`
	Host           string    `json:"host"            validate:"omitempty,hostname_rfc1123" normalize:"normalize"`
	Port           int       `json:"port"            validate:"omitempty,min=1,max=65535"`
	Username       *string   `json:"username"        validate:"omitzero,max=320"`
	Password       *string   `json:"password"        validate:"omitzero,max=1024"`
	Encryption     string    `json:"encryption"      validate:"omitempty,oneof=none starttls ssl"`
	SkipDKIM       *bool     `json:"skip_dkim"`
	SESTopicARN    *string   `json:"ses_topic_arn" validate:"omitzero,max=512" normalize:"trim"`
	AllowedEmails  *[]string `json:"allowed_emails"  validate:"omitzero,dive,min=3,max=320"`
	AllowedDomains *[]string `json:"allowed_domains" validate:"omitzero,dive,min=1,max=253"`
	SecurityMode   string    `json:"security_mode"   validate:"omitempty,oneof=permissive strict"`
	Priority       *int      `json:"priority"        validate:"omitzero,min=0,max=10000"`
	Status         string    `json:"status"          validate:"omitempty,oneof=enabled disabled"`
	PlatformOnly   *bool     `json:"platform_only"`

	// ProviderConfig is patchable, Provider is not - see updateInput.
	ProviderConfig *map[string]string `json:"provider_config" validate:"omitzero,max=20"`
}

// ----------------------------------------------------------------------------
// Responses
// ----------------------------------------------------------------------------

// ListResponse is the project's own delivery servers.
type ListResponse struct {
	SMTPServers []*smodel.Server `json:"smtp_servers"`

	// Providers is what this build can send through, and what each one
	// asks for.
	//
	// Returned with the list rather than from a route of its own, and
	// generated from the transport registry rather than copied into the
	// console: a form built from a hand-kept list in TypeScript offers
	// the wrong fields the day a provider is added, and the write side
	// then refuses what the form just collected.
	Providers []transport.Descriptor `json:"providers"`
}

// ServerResponse is one server. The password is sealed at rest and
// carries json:"-", so it never reaches here.
type ServerResponse struct {
	SMTPServer *smodel.Server `json:"smtp_server"`
}

// GroupListResponse is the project's named delivery pools.
type GroupListResponse struct {
	Groups []*smodel.Group `json:"smtp_server_groups"`
}

// GroupResponse is one pool.
type GroupResponse struct {
	Group *smodel.Group `json:"smtp_server_group"`
}

// SharedListResponse is the platform-owned pool. Admin only: a project
// gets delivery through it and never sight of it.
type SharedListResponse struct {
	SharedSMTPServers []*smodel.Shared `json:"shared_smtp_servers"`

	// Providers, for the same reason as on the project listing: the pool
	// form is the same form, and a platform-owned SES row is the case
	// that lets platform mail leave through an instance role with no
	// stored secret.
	Providers []transport.Descriptor `json:"providers"`
}

// SharedResponse is one platform-owned server.
type SharedResponse struct {
	SharedSMTPServer *smodel.Shared `json:"shared_smtp_server"`
}

// TestResponse is the outcome of dialling a server.
//
// A failed connection test is a 200 with Ok false rather than an error
// status: the request succeeded, and what failed is the thing being
// tested. Error carries the dial or auth message verbatim, because a
// paraphrase of an SMTP rejection helps nobody.
type TestResponse struct {
	Ok    bool   `json:"ok"`
	Error string `json:"error,omitempty"`
}

// SharedTestResponse also reports the validation state the test wrote
// back, so the screen does not have to re-read the row.
type SharedTestResponse struct {
	Ok          bool       `json:"ok"`
	Status      string     `json:"status"`
	ValidatedAt *time.Time `json:"validated_at"`
}
