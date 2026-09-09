// Mailyard, Copyright (c) 2021-2026 YouSysAdmin

package cli

import "github.com/spf13/cobra"

// nodeCommands are the subcommands that start one KIND of node: an
// api-only node, a worker-only node, and the relay agent.
//
// Roles are SUBCOMMANDS and not config: the point of splitting roles is
// to put more machines behind one queue, so every node ships the SAME
// config and the role is a word in argv.
//
// Several `serve` nodes against one database already claim disjoint
// batches through FOR UPDATE SKIP LOCKED and wake each other over
// LISTEN/NOTIFY. What api and worker add is running the two halves on
// separate machines, not the ability to run more than one.
func nodeCommands() []*cobra.Command {
	return []*cobra.Command{newAPICmd(), newWorkerCmd(), newRelayCmd()}
}

func newAPICmd() *cobra.Command {
	return withInit(&cobra.Command{
		Use:   "api",
		Short: "Start the API server only, without the delivery worker",
		Long: "Start an ingress-only node - HTTP API, console and SMTP listeners.\n" +
			"Accepted mail is queued in the database and delivered by whatever\n" +
			"worker nodes are running. At least one worker must exist or nothing\n" +
			"is ever sent.",
		RunE: func(cmd *cobra.Command, _ []string) error {
			return runServe(cmd, role{api: true})
		},
	})
}

func newWorkerCmd() *cobra.Command {
	return withInit(&cobra.Command{
		Use:     "worker",
		Aliases: []string{"sender"},
		Short:   "Start the delivery worker only, without the API",
		Long: "Start a delivery-only node - queue, campaigns and maintenance jobs.\n" +
			"It still binds server.addr, but serves only /healthz, /readyz and\n" +
			"/metrics, so probes and scraping work without exposing the console.\n" +
			"Add as many as the sending volume needs: claiming is a locking\n" +
			"statement, so the nodes take disjoint batches.",
		RunE: func(cmd *cobra.Command, _ []string) error {
			return runServe(cmd, role{worker: true})
		},
	})
}
