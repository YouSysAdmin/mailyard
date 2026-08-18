// Mailyard, Copyright (c) 2021-2026 YouSysAdmin

// Package relaynode is the identity and liveness of a self-enrolled
// relay node.
//
// A node is two things at once and the split matters. It IS a
// delivery row - a shared_smtp_servers row for a platform node, an
// smtp_servers row for a project's own - because delivery must have
// one answer to "what can carry this message". And it has an identity
// here: a control token, a version, the address we saw it from, and
// the last time it said anything.
//
// Keeping the second out of the first is what lets a node attach to
// either delivery table without the rule for "is this node alive"
// being written twice.
package relaynode

import "time"

// Node is one enrolled relay node.
type Node struct {
	ID string `json:"id"`

	// ProjectID is empty for a platform node, which joins the shared
	// pool. Otherwise the project that enrolled it.
	ProjectID string `json:"project_id,omitempty"`

	// ServerID is the delivery row this node is. Which table it lives
	// in follows from ProjectID.
	ServerID string `json:"server_id"`

	// TokenHash is hex(sha256) of the control token. Never
	// serialized: handing it out lets the reader impersonate the node.
	TokenHash string `json:"-"`
	Name      string `json:"name"`
	Version   string `json:"version,omitempty"`

	// PublicIP is what the control plane observed, not what the node
	// claimed. This is the address an operator has to authorize in
	// SPF.
	PublicIP   string     `json:"public_ip,omitempty"`
	LastSeenAt *time.Time `json:"last_seen_at,omitempty"`
	CreatedAt  time.Time  `json:"created_at"`

	// The receiving half, for a node that also runs an MX.
	//
	// InboundEnabled and InboundQueued are what the node REPORTS on
	// its heartbeat. LastInboundAt is what we OBSERVED - stamped when
	// a forward is accepted, like PublicIP, because a node saying when
	// it last succeeded is a node grading its own work.
	InboundEnabled bool       `json:"inbound_enabled"`
	InboundQueued  int        `json:"inbound_queued"`
	LastInboundAt  *time.Time `json:"last_inbound_at,omitempty"`
}

// Platform reports whether this node serves the shared pool rather
// than one project.
func (n *Node) Platform() bool { return n.ProjectID == "" }

// Fresh reports whether the node has reported recently enough to be
// handed mail.
func (n *Node) Fresh(within time.Duration, now time.Time) bool {
	if n.LastSeenAt == nil {
		return false
	}

	return now.Sub(*n.LastSeenAt) <= within
}

// StaleAfter is how long a node may go without a heartbeat before the
// pool stops offering it.
//
// Generous on purpose. A node is routinely on another continent, and
// losing the control plane for a minute says nothing about whether it
// can still deliver - so this is sized to tolerate a bad link rather
// than to notice a crash quickly.
const StaleAfter = 10 * time.Minute

// Beat is what a node reports about itself on a heartbeat.
//
// A struct rather than a growing argument list, and the split matters
// more than the ergonomics: everything in here is the node's CLAIM.
// PublicIP is not - it is the address we saw it connect from - and
// LastInboundAt is not either, which is why neither is settable by
// the node and LastInboundAt is written by TouchInbound instead.
type Beat struct {
	Version string

	// InboundEnabled says the node runs an MX. InboundQueued is how
	// much received mail it is holding: a number that only grows is
	// the diagnosis, because it means the node is fine and the link
	// back here is not.
	InboundEnabled bool
	InboundQueued  int
}
