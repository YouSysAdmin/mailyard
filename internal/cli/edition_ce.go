// Mailyard, Copyright (c) 2021-2026 YouSysAdmin

//go:build !enterprise

package cli

import (
	"github.com/yousysadmin/mailyard/internal/core/env"
	"github.com/yousysadmin/mailyard/internal/database/postgres"
	"github.com/yousysadmin/mailyard/internal/domain/email"
)

// editionQueueWiring attaches what the enterprise build adds to the
// delivery path. The community build adds nothing.
func editionQueueWiring(*env.Runtime, *postgres.Listener, *email.Processor) {}

// editionJobs registers the scheduled jobs only the enterprise build
// has. The community build has none.
func editionJobs(*env.Runtime, bool) {}
