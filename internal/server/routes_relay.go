// Mailyard, Copyright (c) 2021-2026 YouSysAdmin

package server

import (
	"github.com/gofiber/fiber/v3"

	"github.com/yousysadmin/mailyard/internal/core/env"
	"github.com/yousysadmin/mailyard/internal/domain/inbound"
	"github.com/yousysadmin/mailyard/internal/domain/relaynode"
)

// registerRelayEnrolment mounts the node-facing half of relay nodes and
// hands back the authority the console-facing half also needs.
//
// One function rather than a block in registerRoutes because these are
// the routes a NODE calls, and they are mounted only where
// relay_nodes.enabled is set - the pages that administer nodes are
// registered either way. The authority comes back either way too.
func registerRelayEnrolment(app *fiber.App, rt *env.Runtime) relaynode.CertAuthority {
	// ONE relay authority for the process. It caches the CA and the
	// shared worker pair in memory, and destroying the authority has to
	// clear the cache the SIGNING path reads - a second instance would
	// keep signing with a key the operator just deleted.
	ca := &relaynode.Authority{
		Store:      rt.Store.Certificate,
		CommonName: rt.Config.RelayNodes.CACommonName,
		Log:        rt.Log,
	}

	// The administration pages are registered either way, so the
	// authority is returned either way. What the switch governs is
	// whether anything can enrol.
	if !rt.Config.RelayNodes.Enabled {
		return ca
	}

	// Public like the SES webhook: a node has no session and no API
	// key, only a URL and the shared token. Rate limited because that
	// token is all there is in front of it.
	rnh := &relaynode.Handler{
		Runtime: rt,
		CA:      ca,
		// Built whatever inbound.enabled says. A node's MX is the
		// point of this installation not needing a port 25 of its
		// own, so gating the forwarding endpoint on the local
		// listener would refuse mail precisely in the deployment
		// the feature exists for.
		Ingest: inbound.NewService(rt),
	}
	enrol := app.Group("/api/relay-nodes", noStoreCache)
	// Register is the ONLY unauthenticated surface here - it is
	// where a shared token could be guessed - so it carries the
	// tight per-IP limit.
	enrol.Post("/register", perMinute(rt, rt.Config.RateLimit.LoginPerMinute, nil), rnh.Register)
	// Heartbeat and renew are NOT on that budget. They present a
	// per-node token, they are called on a timer, and a fleet
	// behind one address shares a source IP - putting them in a
	// bucket sized for human logins means a busy fleet locks
	// itself out of reporting and every node silently drops from
	// the pool. The generous ceiling is a runaway guard, not an
	// authentication measure.
	nodeChatter := perMinute(rt, rt.Config.RateLimit.RelayNodeChatterPerMinute, nil)
	enrol.Post("/heartbeat", nodeChatter, rnh.Heartbeat)
	enrol.Post("/renew", nodeChatter, rnh.Renew)
	enrol.Post("/report", nodeChatter, rnh.Report)
	// A pull node's claim is a long-poll, so one node makes at most a
	// couple a minute - the chatter budget is the right one.
	enrol.Post("/claim", nodeChatter, rnh.Claim)
	// Inbound is per MESSAGE, not per timer, so it gets a budget of
	// its own. A node fronting an MX for a busy domain would eat
	// the chatter bucket and take heartbeats down with it - and a
	// node that cannot heartbeat drops out of the delivery pool,
	// which turns a burst of arriving mail into an outage in the
	// other direction.
	enrol.Post("/inbound", perMinute(rt, rt.Config.RateLimit.RelayNodeInboundPerMinute, nil), rnh.Inbound)

	return ca
}
