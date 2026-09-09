// Mailyard, Copyright (c) 2021-2026 YouSysAdmin

// Package relayca is the certificate authority that identifies relay
// nodes to the delivery workers and the workers to the nodes.
//
// The worker-to-node hop crosses the public internet, and the obvious
// alternative is worse than it looks: a username and password in a
// shared row is a long-lived secret every worker holds, and it
// authenticates in ONE direction - the node learns nothing about who
// connected.
//
// A private CA answers both directions with one mechanism, and
// revoking a node is un-enrolling it rather than rotating a secret
// every other node also holds.
//
// go-tlsutils supplies the transport half but nothing that ISSUES
// certificates, so the signing lives here.
package relayca
