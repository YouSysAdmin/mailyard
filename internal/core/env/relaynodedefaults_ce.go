// Mailyard, Copyright (c) 2021-2026 YouSysAdmin

//go:build !enterprise

package env

import "github.com/spf13/viper"

// No relay_node section in this build, so nothing to default. See
// relaynode_ce.go.
func relayNodeDefaults(_ *viper.Viper) {}
