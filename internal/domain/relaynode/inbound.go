// Mailyard, Copyright (c) 2021-2026 YouSysAdmin

package relaynode

import (
	"context"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"errors"
	"net/mail"
	"strings"
	"time"

	"github.com/gofiber/fiber/v3"

	"github.com/yousysadmin/mailyard/internal/core/response"
	"github.com/yousysadmin/mailyard/internal/core/validation"
	"github.com/yousysadmin/mailyard/internal/domain/inbound"
	dmodel "github.com/yousysadmin/mailyard/internal/models/domain"
	ssmodel "github.com/yousysadmin/mailyard/internal/models/smtpserver"
)

// maxInboundRecipients bounds one forwarded message. The local MX
// listener caps a session at 100 and a node forwards one session, so
// anything past this did not come from an SMTP transaction.
const maxInboundRecipients = 100

// inboundDomainsTTL memoizes the accept list.
//
// Every node beats every two minutes and they all get the same
// installation-wide answer, so without this a fleet turns a fixed
// piece of configuration into a table scan per node per beat. A
// minute of staleness on top of the beat interval changes nothing:
// the list already cannot reach a node faster than its next
// heartbeat.
const inboundDomainsTTL = time.Minute

// The three things that can happen to a forwarded message. All three
// are FINAL - the node deletes its spooled copy on any of them.
//
// This is the half of the contract that is easy to get wrong. The
// STATUS CODE says what happened to the request, the status FIELD
// says what happened to the mail, and they are different questions: a
// message refused because nobody verified its recipient domain is a
// perfectly successful request. Answering 4xx there would work by
// accident today, because ControlError.Fatal() also drops on 400 -
// and would silently start losing mail the day a retryable condition
// picked up a 4xx.
const (
	inboundAccepted  = "accepted"
	inboundDuplicate = "duplicate"
	inboundRefused   = "refused"
)

type inboundOutput struct {
	Status string `json:"status"`
	// ID is the stored inbound row, for the node's log. Present on
	// accepted and duplicate.
	ID string `json:"id,omitempty"`
	// Reason is why a refusal was final, so the node can log
	// something a person can act on rather than "refused".
	Reason string `json:"reason,omitempty"`
}

// Inbound receives a message a node's own MX accepted.
//
// A node runs an MX where the platform cannot - behind a national
// firewall, say - and what it receives comes back over the same HTTP
// control channel, which already survives a proxy or a VPN.
//
// It adds NO rules of its own: whether the mail is wanted, whose it
// is, whether it was seen before and whether it is a delivery report
// are all answered by inbound.Service.Ingest, the same as for the
// listener in this process. A second copy is a second place to leave
// one out, and the mail would still land.
//
// What it does add is trust in a CLAIM: client_ip is the address the
// node saw, and SPF is computed from it. That is the one place a
// node's word lands in a field somebody reads as a check, which is why
// approval gates this route.
func (h *Handler) Inbound(c fiber.Ctx) error {
	in, resp, ok := validation.Bind[inboundInput](c)
	if !ok {
		return resp
	}

	node, srv, resp := h.authenticate(c, in.NodeID, in.Token)
	if node == nil {
		return resp
	}

	// Approval gates this direction for a sharper reason than it gates
	// delivery: a node that can forward mail can file a message, and a
	// delivery report, against any verified domain here. One leaked
	// enrolment token would otherwise inject mail that looks like it
	// arrived at our MX.
	//
	// 503 and not 403, which is the difference between holding this
	// mail and losing it - ControlError.Fatal() drops the spooled copy
	// on a 403, and approval is minutes away. max_lifetime bounds the
	// wait if it never comes.
	if srv.Status != ssmodel.StatusEnabled {
		return response.Unavailable(c, "this node is not approved yet")
	}

	if h.Ingest == nil {
		return response.Internal(c, errors.New("relay node inbound: ingest pipeline is not configured"))
	}

	if len(in.EnvelopeTo) > maxInboundRecipients {
		return response.BadRequest(c, "too many recipients in one message")
	}
	raw, derr := base64.StdEncoding.DecodeString(in.RawB64)
	if derr != nil {
		return response.BadRequest(c, "raw_b64 is not valid base64")
	}
	limit := h.Runtime.Config.Inbound.MaxMessageSize
	if limit > 0 && int64(len(raw)) > limit {
		// A hard refusal, not a retry: the message will be exactly as
		// large next time. The node's own listener holds the same
		// ceiling, so reaching this means the two disagree.
		return response.BadRequest(c, "message exceeds inbound.max_message_size")
	}

	rcpts, d, rerr := h.resolveRecipients(c.Context(), in.EnvelopeTo, node.ProjectID)
	if rerr != nil {
		return response.Internal(c, rerr)
	}

	if d == nil {
		// The node's accept list was stale, or it accepted something it
		// should not have. Final either way - no amount of retrying
		// makes a domain verified.
		h.log().Info("relay node: forwarded mail for a domain nobody has verified",
			"node_id", node.ID, "recipients", len(in.EnvelopeTo))

		return response.Success(c, inboundOutput{
			Status: inboundRefused,
			Reason: "no verified domain covers these recipients",
		})
	}

	if len(rcpts) != len(in.EnvelopeTo) {
		// Recipients spanning two verified domains. A node groups a
		// session by domain before it spools, the same way it groups
		// outbound delivery, so this is a node bug rather than a
		// property of the mail - and accepting the subset that matches
		// would deliver half a message to one project and silently drop
		// the rest.
		return response.BadRequest(c, "recipients span more than one verified domain, forward them separately")
	}

	rec, ierr := h.Ingest.Ingest(c.Context(), d, in.EnvelopeFrom, rcpts, raw,
		inbound.Conn{
			IP: in.ClientIP, HELO: in.HELO,
			// Empty for a platform node, which is the unconfined
			// default our own MX has: a provider forwards its bounce
			// copy to a mailbox on the platform's domain, so the
			// receiving project and the sending project routinely
			// differ. A TENANT's node gets its own project, because a
			// machine on somebody's network must not be able to file a
			// bounce against a neighbour's message.
			ReportScope: node.ProjectID,
		})
	switch {
	case ierr == nil:
		h.log().Info("relay node: forwarded mail received",
			"node_id", node.ID, "id", rec.ID, "project_id", rec.ProjectID,
			"domain", d.Domain, "sender", rec.Sender, "bytes", len(raw))
		h.touchInbound(c, node.ID)

		return response.Success(c, inboundOutput{Status: inboundAccepted, ID: rec.ID})

	case errors.Is(ierr, inbound.ErrDuplicate):
		// Already here. The node retrying a batch it could not confirm
		// is the ordinary way this happens, and it is the reason
		// forwarding can be at-least-once at all.
		return response.Success(c, inboundOutput{Status: inboundDuplicate, ID: rec.ID})

	case errors.Is(ierr, inbound.ErrSenderSuppressed):
		return response.Success(c, inboundOutput{
			Status: inboundRefused, Reason: "sender is on the project suppression list",
		})

	case errors.Is(ierr, inbound.ErrDMARCFail):
		return response.Success(c, inboundOutput{
			Status: inboundRefused, Reason: "dmarc: the sender domain publishes p=reject",
		})

	default:
		// Ours, and possibly temporary - a database that is down comes
		// back. 500 keeps the message in the node's spool.
		return response.Internal(c, ierr)
	}
}

// touchInbound records that mail came through this node.
//
// Never fails the request. The message is already stored - refusing
// here would make the node forward it again, and the second copy
// would be a duplicate the platform discards, so the only thing lost
// would be a timestamp on a console page.
func (h *Handler) touchInbound(c fiber.Ctx, nodeID string) {
	if err := h.Runtime.Store.RelayNode.TouchInbound(c.Context(), nodeID, time.Now()); err != nil {
		h.log().Warn("relay node: could not record the inbound timestamp", "node_id", nodeID, "err", err)
	}
}

// resolveRecipients normalizes the envelope and finds the one verified
// domain covering it.
//
// Returns the recipients it could place and that domain. A caller
// comparing the two lengths learns whether any recipient belonged
// somewhere else, which is the only shape of partial answer worth
// having here.
//
// owner is the node's project, empty for a platform node. Resolution
// stays GLOBAL - domain names are globally unique - and ownership is
// asserted after. Querying only the project's domains instead would
// make a neighbour's verified name simply "not found", losing the
// difference between a stale accept list and a node reaching for
// another tenant's mail.
func (h *Handler) resolveRecipients(ctx context.Context, in []string, owner string) ([]string, *dmodel.Domain, error) {
	var d *dmodel.Domain
	out := make([]string, 0, len(in))
	for _, rcpt := range in {
		addr := strings.ToLower(strings.TrimSpace(rcpt))
		if parsed, err := mail.ParseAddress(addr); err == nil {
			addr = strings.ToLower(parsed.Address)
		}
		found, err := h.Ingest.ResolveDomain(ctx, addr)
		if err != nil {
			return nil, nil, err
		}

		if found == nil {
			continue
		}

		if owner != "" && found.ProjectID != owner {
			// A tenant's node offering mail for a domain its project
			// does not own. Treated exactly like an unverified name -
			// same refusal, so this cannot be used to learn which
			// domains a neighbour has verified.
			h.log().Warn("relay node: forwarded mail for a domain the node's project does not own",
				"recipient", addr)
			continue
		}

		if d == nil {
			d = found
		}

		if found.ID != d.ID {
			continue
		}
		out = append(out, addr)
	}

	return out, d, nil
}

// inboundDomains is the accept list a node caches, with a fingerprint
// so it only travels when it changed.
//
// The etag is what the node keys on, NOT the presence of the list in
// the response: an installation with no verified domains at all has
// an empty list and a perfectly good etag, and a node that read
// absence as "unchanged" would keep accepting mail for domains that
// were removed.
// projID is the node's project, empty for a platform node - and the
// memo is keyed on it, because handing one project's node the list
// another project's node warmed would be the whole tenancy hole in
// one line.
func (h *Handler) inboundDomains(ctx context.Context, projID string) ([]string, string, error) {
	h.domainsMu.Lock()
	defer h.domainsMu.Unlock()

	if e, ok := h.domainCache[projID]; ok && time.Since(e.at) < inboundDomainsTTL {
		return e.names, e.etag, nil
	}
	var (
		names []string
		err   error
	)
	if projID == "" {
		names, err = h.Runtime.Store.Domain.VerifiedNames(ctx)
	} else {
		names, err = h.Runtime.Store.Domain.VerifiedNamesIn(ctx, projID)
	}

	if err != nil {
		return nil, "", err
	}
	sum := sha256.Sum256([]byte(strings.Join(names, "\n")))
	e := domainCacheEntry{names: names, etag: hex.EncodeToString(sum[:16]), at: time.Now()}
	if h.domainCache == nil {
		h.domainCache = map[string]domainCacheEntry{}
	}
	h.domainCache[projID] = e

	return e.names, e.etag, nil
}
