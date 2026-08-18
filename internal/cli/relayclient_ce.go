// Mailyard, Copyright (c) 2021-2026 YouSysAdmin

//go:build !enterprise

package cli

import (
	"github.com/yousysadmin/mailyard/internal/core/env"
	"github.com/yousysadmin/mailyard/internal/domain/email"
	"github.com/yousysadmin/mailyard/internal/domain/store"
)

// A community worker never dials a relay node, so it carries no
// identity to present to one.
//
// Nil rather than an identity that refuses: this is the value every
// installation with relay_nodes.enabled off already gets, and
// email.Processor has always taken it. The delivery path is unchanged
// here - what is absent is the certificate authority that would sign
// the worker's client certificate, and there is nothing on the other
// end of the connection either.
func relayClientSource(_ *env.Config, _ *store.Store) email.RelayClientSource {
	return nil
}
