// Mailyard, Copyright (c) 2021-2026 YouSysAdmin

// Package smtpserver is the per-project SMTP server record: the
// credentials and dial settings the delivery worker sends through.
package smtpserver

import (
	"crypto/tls"
	"strings"
	"time"

	"github.com/yousysadmin/mailyard/internal/core/transport"
)

// Statuses. Disabled servers are never picked for delivery. Invalid
// is set by a failed test-connection and cleared by a successful one.
const (
	StatusEnabled  = "enabled"
	StatusDisabled = "disabled"
	StatusInvalid  = "invalid"

	// StatusPending is a relay node that has enrolled itself and is
	// waiting for a platform admin to let it carry mail. Not a new
	// gate: ListEnabled already filters on status, so a pending node
	// is out of the pool through the mechanism that was already
	// there.
	StatusPending = "pending"
)

// Server is one outbound SMTP endpoint owned by a project. Password
// is stored encrypted (core/crypto) and never serialized to JSON.
// AllowedEmails restricts which sender addresses may use this server:
// empty allows any, entries are exact addresses or "*@domain"
// wildcards.
type Server struct {
	ID string `json:"id"`

	// omitempty because a Shared server embeds this and belongs to no
	// project. On a per-project server the column is NOT NULL, so the
	// field is never empty there and the output is unchanged.
	ProjectID  string `json:"project_id,omitempty"`
	CreatedBy  string `json:"created_by,omitempty"`
	Name       string `json:"name"`
	Host       string `json:"host"`
	Port       int    `json:"port"`
	Username   string `json:"username,omitempty"`
	Password   string `json:"-"`
	Encryption string `json:"encryption"`

	// SkipDKIM suppresses Mailyard's own DKIM signature for mail routed
	// through this server. For providers that rewrite Message-ID and
	// Date and re-sign the result (Amazon SES with Easy DKIM), the
	// local signature is guaranteed to arrive broken, so emitting it
	// is pure noise.
	SkipDKIM      bool     `json:"skip_dkim"`
	AllowedEmails []string `json:"allowed_emails"`

	// AllowedDomains restricts which sender DOMAINS may relay through
	// this server. Empty allows any, and the match is EXACT - a listed
	// name does not cover its subdomains, unlike domain verification.
	// SPF is why: a relay authorized in the record for example.com is
	// not thereby authorized for mail.example.com, so covering the
	// subdomain here would hand back exactly the failure this list
	// exists to prevent. List the subdomain to allow it.
	//
	// Applied in ADDITION to AllowedEmails, which is per address. Both
	// are asked by ResolveCandidates, on the pinned server, on every
	// member of a group and on the shared pool alike.
	AllowedDomains []string `json:"allowed_domains"`

	// GroupID is the pool this server belongs to. Never empty after
	// migration 00003 - a server always sits in exactly one group, and
	// a project always has a default one.
	GroupID string `json:"group_id,omitempty"`

	// Priority orders the server within its group, lowest first.
	// created_at breaks ties, so failover walks a total order.
	Priority        int        `json:"priority"`
	Status          string     `json:"status"`
	ValidationError string     `json:"validation_error,omitempty"`
	ValidatedAt     *time.Time `json:"validated_at,omitempty"`
	CreatedAt       time.Time  `json:"created_at"`

	// SESTopicARN is the SNS topic Amazon SES publishes this server's
	// bounce and complaint notifications to.
	//
	// It lives on the server, not in platform config, because SES is a
	// property of one server: a tenant configures their own SES
	// account as their own row, and a config key could only ever serve
	// an operator who owned the account themselves. Binding it here
	// binds the topic to a project, which is what lets a notification
	// be checked against the message it claims to be about.
	SESTopicARN string `json:"ses_topic_arn,omitempty"`

	// NodeID is non-empty when this row is a self-enrolled relay node.
	//
	// It lives on Server rather than on Shared because Server is the
	// type DELIVERY holds: resolveShared hands back the embedded
	// value, so anything declared only on Shared is invisible by the
	// time a message is being sent. And it has to be visible - a node
	// is reached over mutual TLS with no password, which is a
	// different dial from every other server.
	//
	// Read-only, populated by a join on relay_nodes. Nothing writes
	// this column, because there isn't one.
	NodeID string `json:"node_id,omitempty"`

	// LastSeenAt is the node's most recent heartbeat, joined in for
	// display. Nil for an ordinary server, which has no heartbeat.
	LastSeenAt *time.Time `json:"last_seen_at,omitempty"`

	// Provider is how this row is reached: a dial, or a provider's own
	// API. Empty means smtp, which is what every row was before this
	// column existed and is what keeps them behaving identically.
	//
	// A column rather than a second table, because ResolveCandidates is
	// the one answer to "what can carry this" and has three callers that
	// must agree. A parallel table of API-based routes would be a fourth
	// resolution path, and groups, priority, failover, allowed_emails
	// and the sender rules would all need saying twice.
	Provider string `json:"provider,omitempty"`

	// ProviderConfig is the provider's non-secret settings - a SES region,
	// a configuration set. Plain, not sealed: the console shows a region
	// without needing the encryption key, the same split cert_pem and
	// data already use.
	//
	// Credentials do not live here. They go in Username and Password,
	// where Password is already sealed and already means "empty is
	// ordinary" - so a provider taking a key pair needs no new sealed
	// column and no second path for secrets.
	ProviderConfig map[string]string `json:"provider_config,omitempty"`
}

// IsNode reports whether this row is a self-enrolled relay node.
func (s *Server) IsNode() bool { return s.NodeID != "" }

// SkipsDKIM is the one answer to "does mail through this row go unsigned
// by us", and the delivery path asks only this.
//
// Two reasons it can be true. The operator ticked the box, or the
// PROVIDER signs the message itself - and the second is not a setting.
// SES rewrites Date and Message-ID, both in the signed header set, so a
// signature applied on the way to it is guaranteed to arrive broken. That
// made the checkbox a choice with one correct answer, and choosing wrong
// produced no visible symptom: a broken signature is ignored rather than
// punished, so the mail simply stopped being authenticated by us.
//
// Computed rather than trusted from the column, so a row written before
// this existed, or a PATCH clearing the flag, cannot bring the broken
// signature back.
func (s *Server) SkipsDKIM() bool {
	return s.SkipDKIM || transport.ReSigns(s.Provider)
}

// Normalize settles the fields that are derived rather than chosen, so
// the row that gets written and the object handed back to the caller say
// the same thing.
//
// Called by the store rather than by each handler. With the store
// writing the effective value and the handler returning the object it
// was given, the database says skip_dkim = true and the API
// answered false about the same row - the row was right and the response
// lied about it. Doing it here means every writer gets it, including one
// added later that would not have known to ask.
func (s *Server) Normalize() {
	s.SkipDKIM = s.SkipsDKIM()
	if s.Provider == "" {
		s.Provider = transport.ProviderSMTP
	}

	// A domain that arrived with whitespace or in capitals would be
	// stored as typed and then never match, because AllowsDomain
	// compares against a bare lowercase host. The list reaches here
	// from three writers and one of them is an ENVIRONMENT VARIABLE
	// split on commas - viper does not trim what it splits, so
	// "a.com, b.com" yields a second entry that begins with a space.
	// The failure is a send with no candidates at all, which is loud
	// but says nothing about a stray space.
	for i, d := range s.AllowedDomains {
		s.AllowedDomains[i] = strings.ToLower(strings.TrimSpace(strings.TrimPrefix(strings.TrimSpace(d), "@")))
	}
}

// Spec says how to reach this row, and is the one place that answers it.
//
// Everything a transport needs and nothing about the row it does not: no
// project, no group, no status. The TLS override is resolved by the
// caller, because for a relay node it needs a certificate this package
// cannot reach.
func (s *Server) Spec(nodeTLS *tls.Config) transport.Spec {
	return transport.Spec{
		Provider:   s.Provider,
		Host:       s.Host,
		Port:       s.Port,
		Username:   s.Username,
		Password:   s.Password,
		Encryption: s.Encryption,
		TLS:        nodeTLS,
		Options:    s.ProviderConfig,
	}
}

// AllowsSender reports whether sender may relay through this server.
func (s *Server) AllowsSender(sender string) bool {
	if len(s.AllowedEmails) == 0 {
		return true
	}

	at := strings.LastIndex(sender, "@")
	for _, allowed := range s.AllowedEmails {
		if strings.EqualFold(allowed, sender) {
			return true
		}

		if strings.HasPrefix(allowed, "*") && at >= 0 && strings.EqualFold(allowed[1:], sender[at:]) {
			return true
		}
	}

	return false
}

// Security modes for a shared server. Strict is what stops one
// project relaying as another's domain through platform credentials.
const (
	// SecurityPermissive admits any sender the domain rules allow.
	SecurityPermissive = "permissive"

	// SecurityStrict additionally requires the sending project to have
	// proved ownership of the sender's domain.
	SecurityStrict = "strict"
)

// Shared is a platform-owned SMTP server, managed by a platform admin
// and usable by any project that has configured none of its own.
//
// A separate type over a separate table rather than a flag on Server.
// Every tenant query in this codebase scopes on project_id first, and
// smtp_servers.project_id is NOT NULL with a foreign key - a shared
// row would have to violate both, and the first missing scope clause
// would turn into one project reading another's server. Kept apart,
// a project-scoped query cannot return one of these by construction.
//
// It embeds Server because delivery does not care where a server came
// from: once picked, the same host, port and credentials are dialled
// the same way. ProjectID on the embedded value is always empty.
type Shared struct {
	Server

	// SecurityMode is permissive or strict. See SecurityStrict.
	SecurityMode string `json:"security_mode"`

	// PlatformOnly reserves this row for the platform's own mail -
	// invitations, password resets, signup confirmations - and keeps
	// tenant sends off it. resolveShared skips it and systemmail
	// prefers it.
	//
	// The default is false, which is right for a small install: one
	// shared server carries both, and there is nothing to configure.
	// Set it where platform mail has to leave from a different address
	// or reputation than the tenants relaying through the pool.
	PlatformOnly bool `json:"platform_only"`
}

// AllowsDomain reports whether sender's domain may relay through this
// server. This is the domain rule only - a shared row's strict mode is
// enforced by the caller, which is the only place that knows which
// project is sending.
//
// ONE method for both levels. It lived on Shared, so a project that
// split its relay nodes by domain had nothing to split them WITH:
// allowed_emails is per address, is empty by default, and a project
// sending from several domains under unset addresses got no
// restriction at all. Moving the field onto Server is what lets the
// same rule be written once and asked in one place.
func (s *Server) AllowsDomain(sender string) bool {
	if len(s.AllowedDomains) == 0 {
		return true
	}

	_, host, ok := strings.CutLast(sender, "@")
	if !ok || host == "" {
		return false
	}

	host = strings.ToLower(host)
	for _, d := range s.AllowedDomains {
		// Trimmed here as well as in Normalize, for the same reason
		// the "@" is: this is what the comparison tolerates, and a row
		// written before Normalize learned to clean the list must not
		// quietly stop matching.
		if strings.EqualFold(strings.TrimPrefix(strings.TrimSpace(d), "@"), host) {
			return true
		}
	}

	return false
}

// ValidSecurityMode reports whether mode is one this code honors. An
// unknown value must not silently degrade to permissive.
func ValidSecurityMode(mode string) bool {
	return mode == SecurityPermissive || mode == SecurityStrict
}

// Group is a named pool of a project's SMTP servers.
//
// Two jobs. It is what a send names instead of a server uuid, so an
// integration keeps working when the servers behind it are replaced.
// And it is the unit failover happens within: a transient failure
// moves to the next server in the same group, never to another.
//
// Every project has exactly one group with Default set, enforced by a
// partial unique index. A send that names no group uses it.
type Group struct {
	ID          string `json:"id"`
	ProjectID   string `json:"project_id"`
	Name        string `json:"name"`
	Slug        string `json:"slug"`
	Description string `json:"description,omitempty"`

	// Default marks the group used when a send names none. Exactly one
	// per project, and it cannot be deleted.
	Default   bool      `json:"is_default"`
	CreatedAt time.Time `json:"created_at"`

	// Servers is filled by list reads for display and is never stored.
	Servers []*Server `json:"servers,omitempty"`
}
