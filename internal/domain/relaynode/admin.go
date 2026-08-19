// Mailyard, Copyright (c) 2021-2026 YouSysAdmin

package relaynode

import (
	"strings"
	"time"

	"github.com/gofiber/fiber/v3"

	"github.com/yousysadmin/mailyard/internal/core/edition"
	"github.com/yousysadmin/mailyard/internal/core/response"
	nodemodel "github.com/yousysadmin/mailyard/internal/models/relaynode"
	ssmodel "github.com/yousysadmin/mailyard/internal/models/smtpserver"
)

// The Handler, its logger and serverFor live in handler.go, which both
// halves of this domain share. The wire types are in types.go.

// List returns every enrolled node. Platform admin.
func (h *Handler) List(c fiber.Ctx) error {
	nodes, err := h.Runtime.Store.RelayNode.ListAll(c.Context())
	if err != nil {
		return response.Internal(c, err)
	}

	now := time.Now()
	out := listOutput{
		Nodes:       []nodeView{},
		MXHosts:     []string{},
		AutoApprove: h.autoApprove(),
		Enabled:     h.Runtime.Config.RelayNodes.Enabled,
		Available:   edition.RelayNodes,
	}
	var ips []string

	for _, n := range nodes {
		v := nodeView{Node: n, Alive: n.Fresh(nodemodel.StaleAfter, now)}

		// serverFor, not the shared store directly. A project's node
		// lives in smtp_servers, and reading only the shared table
		// showed tenant nodes with no host and no status at all.
		srv, err := h.serverFor(c.Context(), n)
		if err != nil {
			return response.Internal(c, err)
		}

		if srv != nil {
			v.Host, v.Port, v.Status = srv.Host, srv.Port, srv.Status
		}

		// Only an approved node's address matters for SPF. Listing a
		// pending one would have the operator authorize a machine that
		// is not sending, and quietly leave it authorized if it never
		// gets approved.
		if v.Status == ssmodel.StatusEnabled && n.PublicIP != "" {
			ips = append(ips, "ip4:"+n.PublicIP)
		}

		// Same rule for the MX list, and for the same reason: pointing
		// a domain's mail at a node nobody approved sends it to a
		// machine that answers every recipient with a refusal.
		//
		// Platform nodes only. A tenant's node accepts only its own
		// project's domains, so listing it here would have the
		// operator publish an MX on their own bounce domain pointing
		// at a machine that refuses every one of their recipients.
		// The project sees its own on its own page.
		if n.Platform() && v.Status == ssmodel.StatusEnabled && n.InboundEnabled && v.Host != "" {
			out.MXHosts = append(out.MXHosts, v.Host)
		}

		out.Nodes = append(out.Nodes, v)
	}

	if len(ips) > 0 {
		out.SPFInclude = strings.Join(ips, " ")
	}

	return response.Success(c, out)
}

// Approve lets a node start carrying mail.
//
// The decision an operator makes by hand unless
// relay_nodes_auto_approve says otherwise. It is a decision worth
// making: a node in the pool receives the content of real messages.
func (h *Handler) Approve(c fiber.Ctx) error {
	node, srv, resp := h.adminNode(c)
	if resp != nil {
		return resp
	}

	if !node.Platform() {
		// A tenant's node is theirs to admit. Letting a platform
		// admin do it would put somebody else's machine into somebody
		// else's mail flow on our say-so - and to write would have
		// gone to the wrong table anyway, changing nothing while
		// looking like it worked.
		return response.Forbidden(c,
			"this node belongs to a project and is approved by an administrator of that project")
	}

	if err := h.Runtime.Store.SharedSMTP.SetStatus(c.Context(),
		srv.ID, ssmodel.StatusEnabled, "", nil); err != nil {
		return response.Internal(c, err)
	}

	h.log().Info("relay node approved", "node_id", node.ID, "host", srv.Host)

	return response.Success(c, StatusResponse{Status: ssmodel.StatusEnabled})
}

// Suspend takes a node out of the pool without forgetting it.
//
// Distinct from delete: the node keeps its certificate and keeps
// heartbeating, so putting it back is one click rather than a
// re-enrolment. Anything already spooled on it still delivers - we
// stop handing it new work, we do not reach into its queue.
func (h *Handler) Suspend(c fiber.Ctx) error {
	node, srv, resp := h.adminNode(c)
	if resp != nil {
		return resp
	}

	// Suspending and removing do cross the tenancy line, on purpose:
	// an operator has to be able to stop a machine that is sending
	// badly from their installation, whoever enrolled it.
	if err := h.setStatus(c, node, srv.ID,
		ssmodel.StatusDisabled, "suspended by an administrator"); err != nil {
		return response.Internal(c, err)
	}

	h.log().Info("relay node suspended", "node_id", node.ID, "host", srv.Host)

	return response.Success(c, StatusResponse{Status: ssmodel.StatusDisabled})
}

// Delete unrolls a node.
//
// Both rows go. Leaving the delivery row would put a server back in
// the pool that nothing is behind - the freshness rule only applies
// to rows a node still claims, so an orphan would look like an
// ordinary manually configured server and be handed mail.
func (h *Handler) Delete(c fiber.Ctx) error {
	node, srv, resp := h.adminNode(c)
	if resp != nil {
		return resp
	}

	if err := h.Runtime.Store.RelayNode.Delete(c.Context(), node.ID); err != nil {
		return response.Internal(c, err)
	}

	h.forgetIssued(c, node.ID)
	if node.Platform() {
		if err := h.Runtime.Store.SharedSMTP.Delete(c.Context(), srv.ID); err != nil {
			return response.Internal(c, err)
		}
	} else if err := h.Runtime.Store.SMTPServer.Delete(c.Context(),
		node.ProjectID, srv.ID); err != nil {
		return response.Internal(c, err)
	}

	h.log().Info("relay node removed", "node_id", node.ID, "host", srv.Host)

	return response.NoContent(c)
}

// ResetAuthority destroys the relay authority and un-enrols the fleet.
//
// The emergency lever, so an operator who suspects the key can end it
// without a psql prompt.
//
// It un-enrols the nodes as well, which is not an extra. Destroying the
// authority on its own leaves every node holding a certificate signed by
// a key that no longer exists: still heartbeating, still listed as
// alive, and impossible to deliver through.
//
// A node does not come back on its own. It holds a stored identity and
// never re-enrols, so the operator has to hand it the token again or
// clear its spool.
func (h *Handler) ResetAuthority(c fiber.Ctx) error {
	if h.CA == nil {
		// Two ways to have no authority, and they are not the same
		// thing to be told. A switch is the operator's to turn on, an
		// edition is not - and pointing them at relay_nodes.enabled on
		// a build that refuses to boot with it set wastes their evening.
		if !edition.RelayNodes {
			return response.BadRequest(c, "relay nodes are part of the enterprise edition")
		}

		return response.BadRequest(c, "relay nodes are not enabled on this installation")
	}

	nodes, err := h.Runtime.Store.RelayNode.ListAll(c.Context())
	if err != nil {
		return response.Internal(c, err)
	}

	for _, n := range nodes {
		// The delivery row first. Left behind it would look like an
		// ordinary server somebody typed in - the freshness rule only
		// covers rows a node still claims - and be handed mail forever.
		if n.Platform() {
			if derr := h.Runtime.Store.SharedSMTP.Delete(c.Context(), n.ServerID); derr != nil {
				return response.Internal(c, derr)
			}
		} else if derr := h.Runtime.Store.SMTPServer.Delete(c.Context(),
			n.ProjectID, n.ServerID); derr != nil {
			return response.Internal(c, derr)
		}

		if derr := h.Runtime.Store.RelayNode.Delete(c.Context(), n.ID); derr != nil {
			return response.Internal(c, derr)
		}
	}

	issued, err := h.CA.Reset(c.Context())
	if err != nil {
		return response.Internal(c, err)
	}

	h.log().Warn("relay authority destroyed",
		"nodes_unenrolled", len(nodes), "issued_certificates_removed", issued)

	return response.Success(c, ResetAuthorityResponse{
		NodesUnenrolled: len(nodes),
		Message: "The relay authority and every node identity are gone. A new authority is " +
			"created the moment something enrols. Each node has to enrol AGAIN, which " +
			"means giving it its enrolment token - a node holding a stored identity does " +
			"not re-enrol by itself. Other API nodes keep the old authority cached until " +
			"they restart.",
	})
}

// forgetIssued drops the record of the certificate this node was
// given. Never fails to delete: the node is going either way, and a
// leftover bookkeeping row is not worth refusing that over.
func (h *Handler) forgetIssued(c fiber.Ctx, nodeID string) {
	if h.CA == nil {
		return
	}

	if err := h.CA.ForgetNode(c.Context(), nodeID); err != nil {
		h.log().Warn("relay node: could not forget the issued certificate",
			"node_id", nodeID, "err", err)
	}
}

// setStatus writes to whichever table holds this node's delivery row.
// Which one follows from the node's project scope and from nothing
// else - the same rule serverFor uses to read it.
func (h *Handler) setStatus(c fiber.Ctx, node *nodemodel.Node, serverID, status, reason string) error {
	if node.Platform() {
		return h.Runtime.Store.SharedSMTP.SetStatus(c.Context(), serverID, status, reason, nil)
	}

	return h.Runtime.Store.SMTPServer.SetStatus(c.Context(),
		node.ProjectID, serverID, status, reason, nil)
}

// adminNode resolves :id into a node and its delivery row.
//
// Three returns for the same reason as authenticate: response.*
// writes the status and returns nil, so a lone error would be nil on
// the refusal path.
//
// The RESPONSE is what a caller tests. Non-nil means the refusal is
// already written; nil means BOTH pointers are set. Testing a pointer
// instead happens to work today - no path returns one without the
// other - but that is a property of the current paths, not a promise,
// and every caller dereferences the server.
func (h *Handler) adminNode(c fiber.Ctx) (*nodemodel.Node, *ssmodel.Shared, error) {
	node, err := h.Runtime.Store.RelayNode.Get(c.Context(), c.Params("id"))
	if err != nil {
		return nil, nil, response.Internal(c, err)
	}

	if node == nil {
		return nil, nil, response.NotFound(c, "relay node not found")
	}

	srv, err := h.serverFor(c.Context(), node)
	if err != nil {
		return nil, nil, response.Internal(c, err)
	}

	if srv == nil {
		return nil, nil, response.NotFound(c, "relay node has no server row")
	}

	return node, srv, nil
}
