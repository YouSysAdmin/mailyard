// Package cli wires the cobra commands the mailyard binary exposes.
// Management is done via the web UI, not here - keep this surface
// deliberately small. The exception is set-password, which exists
// because the console cannot help an operator who is locked out of it.
package cli

import (
	"github.com/spf13/cobra"

	"github.com/yousysadmin/mailyard/pkg"
)

// NewRoot builds a Root.
//
// Errors are silenced here and printed once by main. With cobra printing
// them too, every failure reaches the operator twice, in two formats.
func NewRoot() *cobra.Command {
	// Listed in the order written, so serve, the default, leads its group.
	cobra.EnableCommandSorting = false

	root := &cobra.Command{
		Use:           pkg.AppName,
		Short:         "Self-hosted mailyard service",
		Version:       pkg.Version,
		SilenceUsage:  true,
		SilenceErrors: true,

		// Runnable so that an unknown subcommand reaches Args and comes
		// back as a typed usage error. A root without Run reports it
		// from inside cobra's command lookup, where it cannot be typed.
		Args: noArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			return cmd.Help()
		},
	}
	root.SetFlagErrorFunc(flagError)
	root.PersistentFlags().String("config", "", "config file path (yaml); defaults to ./mailyard.yaml")

	// help and completion stay ungrouped and land under cobra's own
	// "Additional Commands".
	root.AddGroup(
		&cobra.Group{ID: groupNode, Title: "Run a node:"},
		&cobra.Group{ID: groupRecover, Title: "Recover an installation:"},
		&cobra.Group{ID: groupInspect, Title: "Inspect this build:"},
	)
	add(root, groupNode, newServeCmd())
	add(root, groupNode, nodeCommands()...)
	add(root, groupRecover, newSetPasswordCmd(), newTLSCmd())
	add(root, groupInspect, newVersionCmd(), newExportAPISpecCmd())

	return root
}

// Help groups. cobra panics on a GroupID that is not registered on the
// parent, so NewRoot adds these before any command.
const (
	groupNode    = "node"
	groupRecover = "recover"
	groupInspect = "inspect"
)

func add(parent *cobra.Command, group string, cmds ...*cobra.Command) {
	for _, c := range cmds {
		c.GroupID = group
	}

	parent.AddCommand(cmds...)
}
