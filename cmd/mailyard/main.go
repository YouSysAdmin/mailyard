// Command mailyard is the single binary this project ships: the API
// and console, the SMTP submission and inbound listeners, the delivery
// queue and the scheduled jobs. Which of those a process runs is
// decided by the subcommand - see internal/cli.
package main

import (
	"fmt"
	"os"
	"time"

	"github.com/yousysadmin/mailyard/internal/cli"
	"github.com/yousysadmin/mailyard/pkg"
)

func main() {
	// Every time the process renders, in responses and logs, is UTC
	// whatever the host zone is.
	time.Local = time.UTC

	err := cli.NewRoot().Execute()
	if err == nil {
		return
	}

	fmt.Fprintf(os.Stderr, "%s: %v\n", pkg.AppName, err)
	os.Exit(cli.ExitCode(err))
}
