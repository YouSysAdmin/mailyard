// Mailyard, Copyright (c) 2021-2026 YouSysAdmin

//go:build !enterprise

package env

import "fmt"

// A community build refuses to start with relay_nodes.enabled set.
//
// FATAL, not a warning, and on the same grounds as a refused port bind
// or a schema older than the binary: the operator has said out loud
// that a fleet of machines should be able to enrol and carry their
// mail, and this build has no enrolment endpoint, no certificate
// authority and no agent. Starting anyway means every node they
// provision fails at a 404 they have to go and find, while the
// installation reports itself healthy.
//
// The key stays in the config in both editions on purpose - the same
// yaml has to move between them without editing - so this is the one
// place that says the two disagree.
func (c *Config) validateRelayEdition() error {
	if !c.RelayNodes.Enabled {
		return nil
	}

	return fmt.Errorf("relay_nodes.enabled is set, but relay nodes are an enterprise edition " +
		"feature and this is the community build. Unset it to start")
}
