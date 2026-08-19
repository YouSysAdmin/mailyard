// Mailyard, Copyright (c) 2021-2026 YouSysAdmin

//go:build !enterprise

package server

import (
	"github.com/gofiber/fiber/v3"

	"github.com/yousysadmin/mailyard/internal/core/env"
	"github.com/yousysadmin/mailyard/internal/domain/relaynode"
)

// The community build enrols nothing, so it mounts nothing.
//
// Not a registered route answering 404, and not one refusing with a
// message about editions: the five node-facing routes are absent.
// That follows the rule relay_nodes.enabled already states for the same
// surface - off means the routes do not exist rather than merely
// refusing, because an unused public surface is one nobody watches.
//
// The console-facing routes in routes.go are registered either way.
// They are what the pages call, they answer honestly about a fleet that
// cannot exist here, and TestTheConsoleCallsRoutesThatExist would fail
// if they went: the console bundle is the same bundle in both builds.
//
// Nil is the authority, and every caller of it already handles that -
// there is no key to sign with, which is the point.
func registerRelayEnrolment(_ *fiber.App, _ *env.Runtime) relaynode.CertAuthority {
	return nil
}
