// Mailyard, Copyright (c) 2021-2026 YouSysAdmin

//go:build !enterprise

package env

import "fmt"

// RelayNodeConfig is what ONE NODE knows about itself, and a community
// build has no node to run - so the section is empty here.
//
// Note the singular. relay_nodes is the control plane's view of the
// fleet and stays in both editions, because the console asks about it
// and the same yaml has to move between editions unchanged. relay_node
// is the agent's own blueprint: where its control plane is, what it
// calls itself, where its spool lives, whether it also receives on 25.
//
// Empty rather than absent, because Config names the field and viper
// binds environment variables by walking these structs. A struct with
// no fields binds nothing, so MAILYARD_RELAY_NODE_* does not exist in
// this build - which is the honest answer. Leaving the full section
// here would publish a dozen settings that are read by nothing.
type RelayNodeConfig struct{}

// ValidateNode checks the config of a relay node, and a community build
// has no way to start one - the relay subcommand is not registered.
//
// Unreachable, and present anyway: the alternative is tagging the one
// caller too, and this is the shorter, honest answer if that ever
// changes.
func (c *Config) ValidateNode() error {
	return fmt.Errorf("this build cannot run a relay node")
}
