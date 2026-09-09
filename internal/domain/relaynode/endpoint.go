// Mailyard, Copyright (c) 2021-2026 YouSysAdmin

package relaynode

import (
	"context"
	"crypto/rand"
	"crypto/subtle"
	"encoding/hex"
	"fmt"
	"net"
	"strings"
	"time"

	"github.com/gofiber/fiber/v3"

	"github.com/yousysadmin/mailyard/internal/core/clientip"
	"github.com/yousysadmin/mailyard/internal/core/ids"
	"github.com/yousysadmin/mailyard/internal/core/response"
	"github.com/yousysadmin/mailyard/internal/core/smtpclient"
	"github.com/yousysadmin/mailyard/internal/core/validation"
	akmodel "github.com/yousysadmin/mailyard/internal/models/apikey"
	perm "github.com/yousysadmin/mailyard/internal/models/permission"
	nodemodel "github.com/yousysadmin/mailyard/internal/models/relaynode"
	ssmodel "github.com/yousysadmin/mailyard/internal/models/smtpserver"
)

// tokenBytes sizes a node control token. 32 bytes of randomness, hex
// encoded, which is the same shape as the enrolment token an operator
// generates with openssl.
const tokenBytes = 32

type registerOutput struct {
	NodeID string `json:"node_id"`
	// Token is shown ONCE. Everything after enrolment presents this.
	Token string `json:"token"`
	// Certificate is the signed leaf, CA is the bundle to verify
	// inbound workers against.
	Certificate string `json:"certificate"`
	CA          string `json:"ca"`
	// ClientName is the SAN a node must require of inbound peers.
	// Without narrowing to it, any certificate this authority signed -
	// including another NODE's - would be admitted.
	ClientName string `json:"client_name"`
	// Status is enabled or pending. A node that is pending should keep
	// heartbeating and say so in its own logs, because from the
	// outside it simply never receives mail.
	Status string `json:"status"`
	// StaleAfterSeconds tells the node how often it must report. It
	// comes from us so the window cannot drift between the two sides.
	StaleAfterSeconds int `json:"stale_after_seconds"`
}

// Register enrols a node.
//
// Two things are established here and they are not the same. The
// TOKEN says the caller was told our shared secret, which is all a
// shared secret can ever say. The CERTIFICATE, signed from here on, is
// what identifies this node individually from now on - so the token
// is used exactly once per node and never again.
func (h *Handler) Register(c fiber.Ctx) error {
	in, resp, ok := validation.Bind[registerInput](c)
	if !ok {
		return resp
	}

	// Two ways in, and they enrol two different kinds of node.
	//
	// The platform token joins the shared pool: one secret the
	// operator holds, in config. A project API key carrying
	// relay:enroll joins that project instead, and reuses everything
	// an API key already is - project bound, revocable, audited,
	// counted by the plan. Inventing a second per-project secret
	// would duplicate all of it.
	projID, resp, ok := h.enrolmentScope(c, in.Token)
	if !ok {
		return resp
	}

	host, herr := validHost(in.Hostname)
	if herr != nil {
		return response.BadRequest(c, herr.Error())
	}

	// ONE node per hostname, across every project and the pool. The
	// certificate minted below is for THIS host, and a worker checks a
	// node's certificate against the host it dialled - so a tenant
	// enrolling under a platform node's name held a certificate the
	// platform's own workers would have accepted, and needed only a
	// route between them to receive the shared pool's mail.
	taken, terr := h.hostTaken(c.Context(), host)
	if terr != nil {
		return response.Internal(c, terr)
	}

	if taken {
		return response.Conflict(c, "a relay node is already enrolled as "+host)
	}
	port := in.Port
	if port == 0 {
		port = defaultNodePort
	}

	nodeID := ids.New()
	name := in.Name
	if name == "" {
		name = host
	}

	certPEM, caPEM, err := h.CA.SignNode(c.Context(), nodeID, in.CSR, host, []string{host})
	if err != nil {
		// A bad request is the caller's fault, anything else is ours.
		// Both are refusals, but only one is worth a 500.
		if strings.Contains(err.Error(), "certificate request") {
			return response.BadRequest(c, "certificate request is not usable: "+err.Error())
		}

		return response.Internal(c, err)
	}

	token, err := newToken()
	if err != nil {
		return response.Internal(c, err)
	}

	status := ssmodel.StatusPending
	if projID == "" && in.ServerGroup != "" {
		// The shared pool has no groups. Saying so beats accepting a
		// value that would be dropped, which is how an operator ends
		// up believing a node is somewhere it is not.
		return response.BadRequest(c,
			"server_group applies to a project node - the shared pool has no groups")
	}

	if projID == "" && h.autoApprove() {
		// Auto-approval is the PLATFORM operator's choice about their
		// own pool. It must not decide anything for a tenant, whose
		// node an admin of that project approves.
		status = ssmodel.StatusEnabled
	}

	// The delivery row first, then the identity. In that order a
	// failure between the two leaves a server nobody enrolled -
	// inert, visible, deletable - rather than a node pointing at a
	// server that does not exist.
	//
	// Implicit TLS with a client certificate, never starttls and never
	// none: this hop crosses the public internet and there is no
	// reason to offer a cleartext phase on a port whose only client
	// is us.
	base := ssmodel.Server{
		ID:        ids.New(),
		ProjectID: projID,
		Name:      name,
		Host:      host,
		Port:      port,
		// The scope the node asked for, on the row from the start.
		// Empty carries any, and the console edits it afterward like
		// any other server - this is the seed, not a lock.
		AllowedDomains: in.Domains,
		Encryption:     smtpclient.EncryptionSSL,
		Status:         status,
	}
	if projID == "" {
		if err := h.Runtime.Store.SharedSMTP.Put(c.Context(), &ssmodel.Shared{
			Server: base, SecurityMode: ssmodel.SecurityPermissive,
		}); err != nil {
			return response.Internal(c, err)
		}
	} else {
		// A project server always sits in exactly one group, so the
		// node goes into the project's default one - the same place a
		// server typed into the console lands.
		group, gerr := h.groupFor(c, projID, in.ServerGroup)
		if gerr != nil {
			return response.Internal(c, gerr)
		}

		if group == nil {
			// A named group that does not exist. Refused HERE, at
			// enrolment, which is the only moment it can still be a
			// clear message - silently landing in the default would
			// mean a node quietly carrying traffic its operator
			// routed somewhere else.
			return response.BadRequest(c,
				"server group "+in.ServerGroup+" does not exist in this project")
		}
		base.GroupID = group.ID
		if err := h.Runtime.Store.SMTPServer.Put(c.Context(), &base); err != nil {
			return response.Internal(c, err)
		}
	}
	srv := &base

	now := time.Now().UTC()
	node := &nodemodel.Node{
		ID:         nodeID,
		ProjectID:  projID,
		ServerID:   srv.ID,
		TokenHash:  HashToken(token),
		Name:       name,
		Version:    in.Version,
		PublicIP:   clientip.From(c),
		LastSeenAt: &now,
		CreatedAt:  now,
		Mode:       in.Mode,
	}
	if err := h.Runtime.Store.RelayNode.Put(c.Context(), node); err != nil {
		return response.Internal(c, err)
	}

	h.log().Info("relay node enrolled",
		"node_id", nodeID, "project_id", projID, "host", host,
		"public_ip", clientip.From(c), "status", status)

	return response.Created(c, registerOutput{
		NodeID:            nodeID,
		Token:             token,
		Certificate:       certPEM,
		CA:                caPEM,
		ClientName:        h.CA.ClientName(),
		Status:            status,
		StaleAfterSeconds: int(nodemodel.StaleAfter.Seconds()),
	})
}

// defaultNodePort is where a node listens for workers. Not 587: that
// is submission, this is a private transport between our own
// processes, and colliding with it on a box that also relays would be
// gratuitous.
const defaultNodePort = 2587

type heartbeatOutput struct {
	// Status lets a node report in its own logs that it is enrolled
	// but not yet approved, which is otherwise indistinguishable from
	// simply having no traffic.
	Status            string `json:"status"`
	StaleAfterSeconds int    `json:"stale_after_seconds"`
	// RenewAfter is when the node should present a fresh CSR.
	RenewAfter string `json:"renew_after,omitempty"`

	// InboundDomainsETag fingerprints the accept list. A node stores
	// it and sends it back, and keys its cache on THIS rather than on
	// whether the list below arrived - see inboundDomains for why the
	// difference matters when the list is empty.
	InboundDomainsETag string `json:"inbound_domains_etag,omitempty"`
	// InboundDomains is present only when the node's etag differs.
	// Omitted when it matches, which is nearly every beat.
	InboundDomains []string `json:"inbound_domains,omitempty"`
}

// Heartbeat records that a node is alive.
func (h *Handler) Heartbeat(c fiber.Ctx) error {
	in, resp, ok := validation.Bind[heartbeatInput](c)
	if !ok {
		return resp
	}

	node, srv, resp := h.authenticate(c, in.NodeID, in.Token)
	if node == nil {
		return resp
	}

	if err := h.Runtime.Store.RelayNode.Heartbeat(c.Context(), node.ID, clientip.From(c), time.Now(),
		nodemodel.Beat{
			Version:        in.Version,
			InboundEnabled: in.InboundEnabled,
			InboundQueued:  in.InboundQueued,
			Mode:           in.Mode,
		}); err != nil {
		return response.Internal(c, err)
	}

	out := heartbeatOutput{
		Status:            srv.Status,
		StaleAfterSeconds: int(nodemodel.StaleAfter.Seconds()),
	}
	// The accept list rides the beat rather than having an endpoint of
	// its own. A node has to make this call anyway, on a timer already
	// sized well inside the staleness window, so a separate poll would
	// be a second schedule to reason about for a list that changes
	// when somebody verifies a domain.
	//
	// Refusing the beat over this would be wrong: liveness is what
	// keeps the node in the pool, and losing delivery because an
	// unrelated list could not be read is a much larger failure than
	// serving a stale one.
	//
	// Approved nodes only. The list names the domains mail is received
	// for, so a node that is merely enrolled - which one leaked token
	// is enough to become - must not be handed it.
	//
	// A platform node gets the installation's, a project node gets its
	// own project's. That split is what makes the list safe to hand a
	// tenant's machine at all.
	if srv.Status == ssmodel.StatusEnabled {
		names, etag, derr := h.inboundDomains(c.Context(), node.ProjectID)
		switch {
		case derr != nil:
			h.log().Error("relay node: could not read the inbound accept list", "err", derr)
		case etag != in.InboundDomainsETag:
			out.InboundDomainsETag, out.InboundDomains = etag, names
		default:
			out.InboundDomainsETag = etag
		}
	}

	return response.Success(c, out)
}

type renewOutput struct {
	Certificate string `json:"certificate"`
	CA          string `json:"ca"`
	ClientName  string `json:"client_name"`
}

// Renew reissues a node certificate.
//
// Renewal exists because a peer pair that expires takes every
// handshake down at once, and a ninety-day leaf makes that a routine
// the node has already performed rather than an event nobody has
// rehearsed. The node is re-signed for the SAME host it enrolled with:
// a rename is an enrolment, not a renewal.
func (h *Handler) Renew(c fiber.Ctx) error {
	in, resp, ok := validation.Bind[renewInput](c)
	if !ok {
		return resp
	}

	node, srv, resp := h.authenticate(c, in.NodeID, in.Token)
	if node == nil {
		return resp
	}

	certPEM, caPEM, err := h.CA.SignNode(c.Context(), node.ID, in.CSR, srv.Host, []string{srv.Host})
	if err != nil {
		if strings.Contains(err.Error(), "certificate request") {
			return response.BadRequest(c, "certificate request is not usable: "+err.Error())
		}

		return response.Internal(c, err)
	}
	h.log().Info("relay node certificate renewed", "node_id", node.ID, "host", srv.Host)

	return response.Success(c, renewOutput{
		Certificate: certPEM,
		CA:          caPEM,
		ClientName:  h.CA.ClientName(),
	})
}

// authenticate resolves a node from its id and token.
//
// Returns (nil, nil, resp) on refusal. Three values rather than an
// error, for the reason spelled out on verifySession and passkeySelf:
// the response helpers write the status and return nil, so a single
// error return would be nil on the refusal path and the caller would
// fall straight through it.
func (h *Handler) authenticate(c fiber.Ctx, nodeID, token string) (*nodemodel.Node, *ssmodel.Shared, error) {
	node, err := h.Runtime.Store.RelayNode.Get(c.Context(), nodeID)
	if err != nil {
		return nil, nil, response.Internal(c, err)
	}

	// Identical refusal for an unknown node and a wrong token, and a
	// constant-time comparison for the token, so this cannot be used
	// to learn which node ids exist.
	if node == nil {
		_ = subtle.ConstantTimeCompare([]byte(HashToken(token)), []byte(HashToken(token)))

		return nil, nil, response.Unauthorized(c, "node id or token is not valid")
	}

	if subtle.ConstantTimeCompare([]byte(HashToken(token)), []byte(node.TokenHash)) != 1 {
		h.log().Warn("relay node: bad control token", "node_id", nodeID, "client_ip", clientip.From(c))

		return nil, nil, response.Unauthorized(c, "node id or token is not valid")
	}

	srv, err := h.serverFor(c.Context(), node)
	if err != nil {
		return nil, nil, response.Internal(c, err)
	}

	if srv == nil {
		// The identity outlived its delivery row. Not the node's
		// fault and not something it can fix by retrying, so say so
		// rather than letting it heartbeat into nothing forever.
		return nil, nil, response.NotFound(c, "this node is no longer registered")
	}

	return node, srv, nil
}

// groupFor resolves the group a project node joins.
//
// EnsureDefault for the empty case, not GetDefault: this is a write
// path, and a project that has never had a server has no group yet.
// Its own doc comment says so.
//
// A named group is looked up and NOT created. The slug came off a
// config file on somebody else's machine, so a typo must be an error
// rather than a new group nobody meant to make.
func (h *Handler) groupFor(c fiber.Ctx, projID, slug string) (*ssmodel.Group, error) {
	if slug == "" {
		return h.Runtime.Store.SMTPGroup.EnsureDefault(c.Context(), projID)
	}

	return h.Runtime.Store.SMTPGroup.GetBySlug(c.Context(), projID, slug)
}

// enrolmentScope decides which project a registration belongs to.
//
// Empty project id means the shared pool. A non-empty one means the
// caller presented an API key of that project carrying relay:enroll.
// The refusal is identical for a wrong platform token and a wrong
// key, so this cannot be used to learn which one was tried.
//
// Three returns, and the ok flag is load-bearing: response.* writes
// the status and returns NIL, so a caller branching on the error
// alone would sail past a refusal - and a key with no relay:enroll
// scope would enrol into the SHARED POOL. verifySession and
// passkeySelf carry the same contract.
func (h *Handler) enrolmentScope(c fiber.Ctx, token string) (string, error, bool) {
	if h.tokenAccepted(token) {
		return "", nil, true
	}

	if strings.HasPrefix(token, akmodel.Prefix) {
		if projID, ok := h.projectForKey(c, token); ok {
			return projID, nil, true
		}
	}
	h.log().Warn("relay node: enrolment refused", "client_ip", clientip.From(c))

	return "", response.Unauthorized(c, "enrolment token is not valid"), false
}

func (h *Handler) projectForKey(c fiber.Ctx, token string) (string, bool) {
	k, err := h.Runtime.Store.APIKey.GetByPrefix(c.Context(), akmodel.TokenPrefix(token))
	if err != nil {
		h.log().Error("relay node: key lookup failed", "err", err)

		return "", false
	}
	now := time.Now().UTC()
	// relay:write is the resource the relay:enroll scope became. It
	// stayed apart from smtp rather than folding into it for the
	// reason the scope's own comment gave: a machine holding this
	// receives the CONTENT of the project's outbound mail.
	if k == nil || !akmodel.HashEquals(token, k.KeyHash) || !k.IsValid(now) ||
		!k.AllowsIP(clientip.From(c)) || !perm.ForKey(k.Permissions, k.Sandbox).Has(perm.ResourceRelay, perm.ActionWrite) {
		return "", false
	}

	return k.ProjectID, true
}

// tokenAccepted compares the presented enrolment token in constant
// time. An empty configured token accepts nothing - Config.Validate
// requires one when the feature is on, and treating empty as open
// would turn a config mistake into an open door.
func (h *Handler) tokenAccepted(presented string) bool {
	want := h.Runtime.Config.RelayNodes.AutoRegisterToken
	if want == "" || presented == "" {
		return false
	}

	return subtle.ConstantTimeCompare([]byte(presented), []byte(want)) == 1
}

func newToken() (string, error) {
	buf := make([]byte, tokenBytes)
	if _, err := rand.Read(buf); err != nil {
		return "", fmt.Errorf("generate node token: %w", err)
	}

	return "rnt_" + hex.EncodeToString(buf), nil
}

// validHost checks the name a node wants to be reached at.
//
// It becomes a certificate SAN and a host workers dial, so the
// characters that would end up in either are rejected here rather
// than discovered at handshake time.
// hostTaken reports whether any enrolled node, in any project or the
// pool, already answers to host. The fleet is small enough to read
// whole: an installation runs tens of nodes, not thousands.
func (h *Handler) hostTaken(ctx context.Context, host string) (bool, error) {
	nodes, err := h.Runtime.Store.RelayNode.ListAll(ctx)
	if err != nil {
		return false, err
	}

	for _, n := range nodes {
		srv, err := h.serverFor(ctx, n)
		if err != nil {
			return false, err
		}

		if srv != nil && strings.EqualFold(srv.Host, host) {
			return true, nil
		}
	}

	return false, nil
}

func validHost(in string) (string, error) {
	host := strings.TrimSuffix(strings.TrimSpace(strings.ToLower(in)), ".")
	if host == "" {
		return "", fmt.Errorf("hostname is required")
	}

	if len(host) > 253 {
		return "", fmt.Errorf("hostname is too long")
	}

	if ip := net.ParseIP(host); ip != nil {
		return host, nil
	}

	if strings.ContainsAny(host, " \t\r\n/\\:@,") {
		return "", fmt.Errorf("hostname contains characters that are not valid in a name")
	}

	if !strings.Contains(host, ".") {
		return "", fmt.Errorf("hostname must be a fully qualified name or an address")
	}

	return host, nil
}
