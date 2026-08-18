// Mailyard, Copyright (c) 2021-2026 YouSysAdmin

package relaynode

import (
	"context"
	"log/slog"
	"sync"
	"time"

	"github.com/yousysadmin/mailyard/internal/core/env"
	"github.com/yousysadmin/mailyard/internal/domain/inbound"
	nodemodel "github.com/yousysadmin/mailyard/internal/models/relaynode"
	settingmodel "github.com/yousysadmin/mailyard/internal/models/setting"
	ssmodel "github.com/yousysadmin/mailyard/internal/models/smtpserver"
)

// Handler owns both halves of this domain: the node-facing enrolment
// endpoints and the console-facing administration of what enrolled.
//
// The enrolment half is public by necessity - a node has no session
// and no API key, and the whole point is that it starts knowing only a
// URL and a token. What authenticates it afterwards is its
// certificate.
//
// The struct and the three methods below sit here rather than beside
// either half because both halves need them, and an installation may
// carry only one of the two.
type Handler struct {
	Runtime *env.Runtime

	// CA signs and forgets node identities. NIL is an ordinary state,
	// not a broken one: an installation that cannot enrol nodes has no
	// authority, and every method that needs one says so rather than
	// crashing. Minted on first use even where it exists - a key that
	// exists is a key that can leak.
	CA CertAuthority

	// Ingest is the pipeline a node's forwarded mail goes through -
	// the same one the local MX listener uses, built by
	// inbound.NewService. Nil disables the inbound endpoint rather
	// than crashing it.
	Ingest *inbound.Service

	// The accept list handed to nodes, memoized per project. See
	// inboundDomains for why the key matters.
	//
	// Only the enterprise half fills these, and a struct cannot be split
	// by build tag, so in the community build they are carried and never
	// read.
	domainsMu   sync.Mutex                  //nolint:unused // read by the enterprise half
	domainCache map[string]domainCacheEntry //nolint:unused // read by the enterprise half
}

// domainCacheEntry is one project's memoized accept list. Declared
// beside the field rather than beside the code that fills it, because
// the field is on the shared struct and the filling is not.
//
//nolint:unused // filled by the enterprise half
type domainCacheEntry struct {
	names []string
	etag  string
	at    time.Time
}

// CertAuthority is the relay authority as its callers use it.
//
// An interface rather than the concrete *Authority because the
// authority is what a node needs and what an installation without
// nodes must not carry: the implementation names a private CA, mints
// keys and signs certificates, and none of that belongs in a build
// that cannot enrol anything. Every caller here already handles nil.
type CertAuthority interface {
	// SignNode signs a node's CSR and returns the leaf and the bundle
	// to verify our own workers against.
	SignNode(ctx context.Context, nodeID, csrPEM, cn string, hosts []string) (certPEM, caPEM string, err error)

	// ClientName is the SAN a node must require of connecting workers.
	ClientName() string

	// Reset destroys the authority and returns how many issued node
	// certificates went with it.
	Reset(ctx context.Context) (int, error)

	// ForgetNode drops the record of what one node was issued.
	ForgetNode(ctx context.Context, nodeID string) error
}

func (h *Handler) log() *slog.Logger { return h.Runtime.Log }

// serverFor loads the delivery row behind a node. Which table that is
// follows from the node's project scope and from nothing else - a
// second field saying so could disagree with it.
func (h *Handler) serverFor(ctx context.Context, node *nodemodel.Node) (*ssmodel.Shared, error) {
	if node.Platform() {
		return h.Runtime.Store.SharedSMTP.Get(ctx, node.ServerID)
	}

	srv, err := h.Runtime.Store.SMTPServer.Get(ctx, node.ProjectID, node.ServerID)
	if err != nil || srv == nil {
		return nil, err
	}

	// Wrapped so callers see one shape. The shared fields are
	// meaningless on a project server and are left at their zero
	// values rather than invented.
	return &ssmodel.Shared{Server: *srv}, nil
}

func (h *Handler) autoApprove() bool {
	if h.Runtime.Settings == nil {
		return false
	}

	return h.Runtime.Settings.Bool(settingmodel.KeyRelayNodesAutoApprove)
}
