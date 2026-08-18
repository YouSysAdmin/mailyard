// Package cli wires the cobra commands the mailyard binary exposes.
// Management is done via the web UI, not here - keep this surface
// deliberately small. The exception is set-password, which exists
// because the console cannot help an operator who is locked out of it.
package cli

import "github.com/spf13/cobra"

// NewRoot builds a Root.
func NewRoot() *cobra.Command {
	root := &cobra.Command{
		Use:           "mailyard",
		Short:         "Self-hosted mailyard service",
		SilenceUsage:  true,
		SilenceErrors: false,
	}
	root.PersistentFlags().String("config", "", "config file path (yaml); defaults to ./mailyard.yaml")
	root.AddCommand(newServeCmd())

	// The split roles and the relay agent, which the community build
	// does not carry - see roles_ce.go.
	root.AddCommand(editionCommands()...)
	root.AddCommand(newVersionCmd())
	root.AddCommand(newSetPasswordCmd())
	root.AddCommand(newTLSCmd())
	root.AddCommand(newExportAPISpecCmd())

	return root
}
