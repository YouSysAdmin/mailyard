// Mailyard, Copyright (c) 2021-2026 YouSysAdmin

package relaynode

import (
	"time"

	"github.com/gofiber/fiber/v2"

	"github.com/yousysadmin/mailyard/internal/core/edition"
	"github.com/yousysadmin/mailyard/internal/core/response"
	"github.com/yousysadmin/mailyard/internal/domain"
	nodemodel "github.com/yousysadmin/mailyard/internal/models/relaynode"
	ssmodel "github.com/yousysadmin/mailyard/internal/models/smtpserver"
)

// A project's own relay nodes.
//
// The same machine and the same binary as a platform node - only two
// things differ. It enrols with an API key carrying relay:enroll
// rather than the operator's shared token, and it is approved by an
// admin of the project rather than a platform admin. Approving
// somebody else's egress machine is not a decision a platform admin
// should be making on their behalf, and it is not one a tenant should
// have to file a ticket for.
//
// Everything downstream is unchanged. A project node is an
// smtp_servers row, so owning one is already the act of taking
// delivery over - the shared pool stops applying, exactly as it does
// for a server typed into the console. And returnPathFor already
// resolves a non-empty ProjectID to the project's own bounce address,
// so a tenant running a node gets their own IP, their own SPF and
// their own return path with no new code at all.

// ListMine returns the calling project's nodes.
func (h *Handler) ListMine(c *fiber.Ctx) error {
	rc := domain.GetRequestContext(c)

	nodes, err := h.Runtime.Store.RelayNode.List(c.UserContext(), rc.Project.ID)
	if err != nil {
		return response.Internal(c, err)
	}

	now := time.Now()
	out := listOutput{
		Nodes:     []nodeView{},
		MXHosts:   []string{},
		Enabled:   h.Runtime.Config.RelayNodes.Enabled,
		Available: edition.RelayNodes,
	}
	for _, n := range nodes {
		v := nodeView{Node: n, Alive: n.Fresh(nodemodel.StaleAfter, now)}
		srv, err := h.Runtime.Store.SMTPServer.Get(c.UserContext(), rc.Project.ID, n.ServerID)
		if err != nil {
			return response.Internal(c, err)
		}

		if srv != nil {
			v.Host, v.Port, v.Status = srv.Host, srv.Port, srv.Status
		}

		// The project's own MX guidance, on the same rule the platform
		// page uses: approved and actually running a mail exchanger. A
		// node that receives is inert until DNS points at it, and the
		// symptom of forgetting - bounces that stop appearing - reads
		// as nothing bouncing.
		if v.Status == ssmodel.StatusEnabled && n.InboundEnabled && v.Host != "" {
			out.MXHosts = append(out.MXHosts, v.Host)
		}

		out.Nodes = append(out.Nodes, v)
	}

	return response.Success(c, out)
}

// ApproveMine lets one of the project's nodes start carrying its mail.
func (h *Handler) ApproveMine(c *fiber.Ctx) error {
	node, srv, resp := h.projectNode(c)
	if resp != nil {
		return resp
	}

	if err := h.Runtime.Store.SMTPServer.SetStatus(c.UserContext(),
		node.ProjectID, srv.ID, ssmodel.StatusEnabled, "", nil); err != nil {
		return response.Internal(c, err)
	}

	h.log().Info("relay node approved by its project",
		"node_id", node.ID, "project_id", node.ProjectID, "host", srv.Host)

	return response.Success(c, StatusResponse{Status: ssmodel.StatusEnabled})
}

// SuspendMine takes a node out of rotation without forgetting it. The
// node keeps its certificate and keeps reporting, so putting it back
// is one click rather than a re-enrolment.
func (h *Handler) SuspendMine(c *fiber.Ctx) error {
	node, srv, resp := h.projectNode(c)
	if resp != nil {
		return resp
	}

	if err := h.Runtime.Store.SMTPServer.SetStatus(c.UserContext(),
		node.ProjectID, srv.ID, ssmodel.StatusDisabled,
		"suspended by a project administrator", nil); err != nil {
		return response.Internal(c, err)
	}

	return response.Success(c, StatusResponse{Status: ssmodel.StatusDisabled})
}

// DeleteMine unenrols a node.
//
// Both rows go. Leaving the server row behind would put a row in the
// project's group with nothing behind it - and since the freshness
// rule only applies to rows a node still claims, that orphan would
// look like an ordinary server and be handed mail forever.
func (h *Handler) DeleteMine(c *fiber.Ctx) error {
	node, srv, resp := h.projectNode(c)
	if resp != nil {
		return resp
	}

	if err := h.Runtime.Store.RelayNode.Delete(c.UserContext(), node.ID); err != nil {
		return response.Internal(c, err)
	}

	h.forgetIssued(c, node.ID)
	if err := h.Runtime.Store.SMTPServer.Delete(c.UserContext(), node.ProjectID, srv.ID); err != nil {
		return response.Internal(c, err)
	}

	h.log().Info("relay node removed by its project",
		"node_id", node.ID, "project_id", node.ProjectID)

	return response.NoContent(c)
}

// projectNode resolves :id within the calling project.
//
// The scope check is the tenancy guarantee: a node id from another
// project answers exactly like one that does not exist, so this
// cannot be used to discover that somebody else's node is there.
//
// Three returns for the reason spelled out on authenticate and
// verifySession - response.* writes the status and returns nil, so a
// single error return would be nil on the refusal path and the caller
// would fall straight through it.
//
// The RESPONSE is what a caller tests - see adminNode, which carries
// the same contract for the platform-admin side.
func (h *Handler) projectNode(c *fiber.Ctx) (*nodemodel.Node, *ssmodel.Server, error) {
	rc := domain.GetRequestContext(c)

	node, err := h.Runtime.Store.RelayNode.Get(c.UserContext(), c.Params("id"))
	if err != nil {
		return nil, nil, response.Internal(c, err)
	}

	if node == nil || node.ProjectID != rc.Project.ID {
		return nil, nil, response.NotFound(c, "relay node not found")
	}

	srv, err := h.Runtime.Store.SMTPServer.Get(c.UserContext(), node.ProjectID, node.ServerID)
	if err != nil {
		return nil, nil, response.Internal(c, err)
	}

	if srv == nil {
		return nil, nil, response.NotFound(c, "relay node has no server row")
	}

	return node, srv, nil
}
