// Mailyard, Copyright (c) 2021-2026 YouSysAdmin

//go:build !enterprise

package relaynode

import "github.com/yousysadmin/mailyard/internal/core/apidoc"

// The community build registers no node-facing routes, so it describes
// none. See routes_relay_ce.go for why they are absent rather than refusing.
func enrolDocs() []apidoc.Route { return nil }
