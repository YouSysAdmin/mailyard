package cli

import (
	"fmt"

	"github.com/spf13/cobra"

	"github.com/yousysadmin/mailyard/pkg"
)

func newVersionCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "version",
		Short: "Print the binary version",
		Run: func(cmd *cobra.Command, args []string) {
			fmt.Printf("%s %s\n", pkg.AppName, pkg.Version)
		},
	}
}
