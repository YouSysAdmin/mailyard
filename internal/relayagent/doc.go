// Mailyard, Copyright (c) 2021-2026 YouSysAdmin

// Package relayagent is the process that runs ON a relay node.
//
// It is the only part of Mailyard that is an MTA. Everything else
// hands a message to a configured host - a tenant's postfix, SES, a
// server in the shared pool. A node is the last hop: it resolves the
// recipient's mail exchangers and delivers to them from its own
// address, which is the whole point, because that address is what a
// receiver judges.
//
// It holds no database credentials and no encryption key. It knows a
// URL, an enrolment token for its first run, and afterwards a
// certificate. Nothing reaches into it except delivery workers over
// mutual TLS, so it can live on any network anywhere.
//
// Not to be confused with internal/domain/relaynode, which is the
// control plane's view of the fleet, or internal/domain/relay, which
// is the submission listener tenants send mail to.
package relayagent
