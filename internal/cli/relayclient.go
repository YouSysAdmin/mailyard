// Mailyard, Copyright (c) 2021-2026 YouSysAdmin

package cli

import (
	"github.com/yousysadmin/mailyard/internal/core/env"
	"github.com/yousysadmin/mailyard/internal/domain/email"
	"github.com/yousysadmin/mailyard/internal/domain/relaynode"
	"github.com/yousysadmin/mailyard/internal/domain/store"
)

// relayClientSource is the identity a delivery worker presents when the
// server it picked turns out to be a relay node.
//
// A function rather than a block in runServe because it is one of the
// two places the delivery path reaches for the relay authority. NIL is
// the supported answer - it is what every installation with
// relay_nodes.enabled off gets, and email.Processor takes it.
//
// Built on every role because the processor is, and it costs nothing
// until a node is actually dialled: the certificate is loaded lazily on
// first use.
func relayClientSource(cfg *env.Config, st *store.Store) email.RelayClientSource {
	if !cfg.RelayNodes.Enabled {
		return nil
	}

	return &relaynode.WorkerIdentity{
		Authority: &relaynode.Authority{
			Store:      st.Certificate,
			CommonName: cfg.RelayNodes.CACommonName,
		},
	}
}
