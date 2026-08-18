// Command mailyard is the single binary this project ships: the API
// and console, the SMTP submission and inbound listeners, the delivery
// queue and the scheduled jobs. Which of those a process runs is
// decided by the subcommand - see internal/cli.
package main

import (
	"log/slog"
	"os"

	"github.com/yousysadmin/mailyard/internal/cli"
)

func main() {
	if err := cli.NewRoot().Execute(); err != nil {
		slog.Error("command failed", "err", err)
		os.Exit(1)
	}
}
