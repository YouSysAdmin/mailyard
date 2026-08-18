// Mailyard, Copyright (c) 2021-2026 YouSysAdmin

//go:build !enterprise

package cli

import "github.com/spf13/cobra"

// The community binary has one way to run a node: serve, which is the
// API and the delivery worker in one process.
//
// Not a command that exists and refuses. A subcommand in --help is a
// promise, and the honest thing for an edition that does not have it is
// not to list it - the same reason the enrolment routes are absent
// rather than answering 404.
//
// Several serve nodes still run against one database and share the
// queue. What is absent is running the halves apart, and the relay
// agent that would carry mail from somewhere else.
func editionCommands() []*cobra.Command { return nil }
