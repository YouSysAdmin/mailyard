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

// The node-facing request bodies live in types_ee.go, beside the
// handlers that read them - the community build registers none of those
// routes, so the shapes would be dead here.

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

	// Available reports whether this build carries relay nodes at all.
	//
	// A different question from Enabled, and the page says different
	// things about them: not-enabled is a switch the operator owns,
	// not-available is an edition. Both answer the same empty table,
	// and telling an operator to set a config key that this binary
	// will refuse to boot with is the worst of the three outcomes.
	Available bool `json:"available"`
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
