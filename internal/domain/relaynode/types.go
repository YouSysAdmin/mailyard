// Mailyard, Copyright (c) 2021-2026 YouSysAdmin

package relaynode

import nodemodel "github.com/yousysadmin/mailyard/internal/models/relaynode"

// The wire types of this domain: what requests carry in and what
// responses carry out, in one file.
//
// They live here rather than beside the handlers that use them so a
// reader answering "what does this endpoint accept and return" has one
// place to look. The types in internal/models are the stored shapes -
// these are what crosses the wire, and the two are allowed to differ.

// ----------------------------------------------------------------------------
// Requests - what a NODE sends under /api/relay-nodes
// ----------------------------------------------------------------------------

type registerInput struct {
	// Token is the shared enrolment secret from relay_nodes config.
	Token string `json:"token" validate:"required"`
	// CSR is the node's certificate request. It carries the public
	// half of a key the node generated and never sends.
	CSR string `json:"csr" validate:"required"`
	// Hostname is the name workers will dial. May be an address.
	Hostname string `json:"hostname" validate:"required" normalize:"trim,lower"`
	Port     int    `json:"port"`
	Name     string `json:"name" normalize:"trim"`
	Version  string `json:"version" normalize:"trim"`
	// Mode is listen or pull - see relaynode.Node.Mode. Empty is listen.
	Mode string `json:"mode" validate:"omitempty,oneof=listen pull" normalize:"trim,lower"`
	// ServerGroup is the slug of the group a PROJECT node joins.
	// Empty means the project's default group.
	ServerGroup string `json:"server_group" normalize:"trim,lower"`

	// Domains is the sender-domain scope the node asks to be held to,
	// and becomes allowed_domains on the row enrolment creates. Empty
	// carries any.
	//
	// Accepting a restriction from the node is safe where accepting a
	// permission would not be: this can only NARROW what the enrolment
	// token already allows, and the machine that will carry the mail is
	// the one place that knows which SPF records name it.
	// No normalize tag: applyNormalizeTags reads one only for a STRING
	// FIELD of a struct, so on a slice it is silently inert. The
	// cleaning happens in Server.Normalize, which every writer of the
	// row goes through - console edit and enrolment alike.
	Domains []string `json:"domains" validate:"omitempty,dive,min=1,max=253"`
}

type heartbeatInput struct {
	NodeID  string `json:"node_id" validate:"required"`
	Token   string `json:"token" validate:"required"`
	Version string `json:"version" normalize:"trim"`
	// InboundDomainsETag is the fingerprint of the accept list this
	// node already holds. Sending it back is what keeps the list off
	// the wire on the heartbeats where nothing changed, which is
	// nearly all of them.
	//
	// Empty means "I have nothing", so a node that has never asked
	// gets the list on its first beat.
	InboundDomainsETag string `json:"inbound_domains_etag" normalize:"trim"`
	// Mode, reported on every beat so a node reconfigured from listen
	// to pull is assigned to rather than dialled from the next message.
	Mode string `json:"mode" validate:"omitempty,oneof=listen pull" normalize:"trim,lower"`
	// What the node reports about its receiving half. A CLAIM, unlike
	// the address we saw it connect from - see relaynode.Beat.
	InboundEnabled bool `json:"inbound_enabled"`
	InboundQueued  int  `json:"inbound_queued"`
}

type inboundInput struct {
	NodeID string `json:"node_id" validate:"required"`
	Token  string `json:"token" validate:"required"`
	// EnvelopeFrom may legitimately be empty: a null return path is
	// exactly what a bounce report carries, and bounces are the
	// reason this endpoint exists.
	EnvelopeFrom string   `json:"envelope_from" normalize:"trim,lower"`
	EnvelopeTo   []string `json:"envelope_to" validate:"required,min=1"`
	// ClientIP and HELO are what the NODE saw. They cannot be
	// recovered from the bytes, and SPF is computed from them - see
	// Inbound for what that means about trusting a node.
	ClientIP string `json:"client_ip" normalize:"trim"`
	HELO     string `json:"helo" normalize:"trim"`
	// RawB64 is the message exactly as the node received it. Base64
	// because this rides the same JSON control channel as everything
	// else a node says, and a second transport for one endpoint would
	// be a second thing to get through a proxy.
	RawB64 string `json:"raw_b64" validate:"required"`
}

type renewInput struct {
	NodeID string `json:"node_id" validate:"required"`
	Token  string `json:"token" validate:"required"`
	CSR    string `json:"csr" validate:"required"`
}

type reportInput struct {
	NodeID   string          `json:"node_id" validate:"required"`
	Token    string          `json:"token" validate:"required"`
	Outcomes []reportOutcome `json:"outcomes"`
}

type reportOutcome struct {
	// EmailID is smtpclient.HeaderEmailID, read by the node out of the
	// message it was handed. It is what says which send this is about.
	EmailID   string `json:"email_id"`
	Recipient string `json:"recipient"`
	Delivered bool   `json:"delivered"`
	// Permanent distinguishes a refusal from having run out of time.
	// Both are final, only one is the address's fault.
	Permanent bool   `json:"permanent"`
	Reason    string `json:"reason"`
}

// --- responses ---

// nodeView is what the console shows. It joins the identity to its
// delivery row, because "is it approved" and "is it alive" live in
// different places and an operator is asking one question.

// claimInput is a pull node asking for its assigned mail.
type claimInput struct {
	NodeID string `json:"node_id" validate:"required"`
	Token  string `json:"token" validate:"required"`
	// Max bounds the batch, WaitSeconds how long to park the request
	// when nothing is assigned yet. Both are capped by the platform.
	Max         int `json:"max" validate:"omitempty,min=1,max=100"`
	WaitSeconds int `json:"wait_seconds" validate:"omitempty,min=0,max=120"`
	// Holding lists the messages the node still has in its spool. The
	// platform pushes their expiry out, so a node that is alive keeps
	// its assignments and one that is not loses them to the next
	// candidate after relay_nodes.assignment_ttl.
	Holding []string `json:"holding" validate:"omitempty,max=1000,dive,uuid"`
}

// claimedMessage is one assigned message, bytes included.
type claimedMessage struct {
	ID           string   `json:"id"`
	EnvelopeFrom string   `json:"envelope_from"`
	Recipients   []string `json:"recipients"`
	// RawB64 is the finished message - signed, headers set - which the
	// node transmits byte for byte.
	RawB64 string `json:"raw_base64"`
}

// claimOutput is the batch.
type claimOutput struct {
	Messages []claimedMessage `json:"messages"`
}

// ----------------------------------------------------------------------------
// Responses
// ----------------------------------------------------------------------------

// nodeView is what the console shows. It joins the identity to its
// delivery row, because "is it approved" and "is it alive" live in
// different places and an operator is asking one question.
type nodeView struct {
	*nodemodel.Node
	Host   string `json:"host"`
	Port   int    `json:"port"`
	Status string `json:"status"`

	// Alive is the same judgement the delivery path makes, computed
	// here from the same window so the console cannot say a node is
	// fine while the pool is skipping it.
	Alive bool `json:"alive"`
}

// listOutput answers both listings - the platform's and a project's own.
// One type because the page is the same page, and a second one would
// let the two drift into showing different things about one machine.
type listOutput struct {
	Nodes []nodeView `json:"relay_nodes"`

	// SPFInclude is the fragment an operator has to add to the SPF
	// record of whichever domain their bounce address is on.
	//
	// Surfaced here because it is the step nothing else will remind
	// them of: a node delivers from its own address, the receiver
	// checks that address against the return path's SPF, and a node
	// missing from the record fails SPF on the operator's own domain.
	SPFInclude string `json:"spf_include"`

	// MXHosts are the approved nodes running an MX, in the order they
	// should appear in an MX record set.
	//
	// Surfaced for the same reason as SPFInclude: nothing else in the
	// product will tell an operator that a node receiving mail is
	// useless until DNS points at it, and the symptom - bounces that
	// simply stop appearing - looks exactly like nothing bouncing.
	MXHosts []string `json:"mx_hosts"`

	// AutoApprove reflects the setting, so the page can explain why a
	// node did or did not start delivering on its own.
	AutoApprove bool `json:"auto_approve"`

	// Enabled reports whether relay_nodes.enabled is set.
	//
	// This listing works either way - it is behind requireAdmin, not
	// public - but with the feature off no node can enrol, so an
	// empty list means "not turned on" rather than "none yet". Saying
	// which is the difference between a page that explains itself and
	// one that looks broken.
	Enabled bool `json:"enabled"`
}

// ResetAuthorityResponse reports what a reset destroyed.
type ResetAuthorityResponse struct {
	NodesUnenrolled int    `json:"nodes_unenrolled"`
	Message         string `json:"message"`
}

// StatusResponse reports a node's new enabled state after a flip.
type StatusResponse struct {
	Status string `json:"status"`
}
