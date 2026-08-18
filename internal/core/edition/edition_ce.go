// Mailyard, Copyright (c) 2021-2026 YouSysAdmin

//go:build !enterprise

package edition

const (
	// Name is what this build calls itself, on the wire and in the
	// version banner.
	Name = "community"

	// RelayNodes reports whether this build can enrol and drive relay
	// nodes. False here means the enrolment routes are not registered,
	// there is no certificate authority to sign a node, and there is no
	// agent to run on one - not that a switch is off.
	RelayNodes = false

	// SplitRoles reports whether the api and worker subcommands exist.
	// A community node runs both halves in one process, which is what
	// serve has always done.
	SplitRoles = false
)
