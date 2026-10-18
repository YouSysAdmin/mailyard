// Mailyard, Copyright (c) 2021-2026 YouSysAdmin

package server

import (
	"context"
	"net"
	"net/http"
	"testing"

	"github.com/gofiber/fiber/v3"

	"github.com/yousysadmin/mailyard/internal/core/env"
	"github.com/yousysadmin/mailyard/internal/domain/relaynode"
	"github.com/yousysadmin/mailyard/internal/domain/store"
	nodemodel "github.com/yousysadmin/mailyard/internal/models/relaynode"
	ssmodel "github.com/yousysadmin/mailyard/internal/models/smtpserver"
	"github.com/yousysadmin/mailyard/internal/relayagent"
)

// relayNodes answers one node.
type relayNodes struct {
	store.RelayNodeStore
	node *nodemodel.Node
}

func (s relayNodes) Get(_ context.Context, id string) (*nodemodel.Node, error) {
	if s.node.ID == id {
		return s.node, nil
	}

	return nil, nil
}

// smtpServers answers one project server.
type smtpServers struct {
	store.SMTPServerStore
	srv *ssmodel.Server
}

func (s smtpServers) Get(_ context.Context, projID, id string) (*ssmodel.Server, error) {
	if s.srv.ProjectID == projID && s.srv.ID == id {
		return s.srv, nil
	}

	return nil, nil
}

// A node's forward is a whole received message, base64 in JSON. The
// credential it repeats in relaynode.ControlHeader is what earns the
// server's ceiling, so a message the node accepted up to
// inbound.max_message_size reaches the handler. Anything that does not
// verify keeps apiBodyLimit.
func TestAVerifiedRelayNodeForwardsAMessageAboveTheAPICeiling(t *testing.T) {
	const (
		nodeID = "0195d1a2-7c3e-7f00-8000-00000000000a"
		token  = "node-control-token"
	)

	srv := &ssmodel.Server{ID: "s1", ProjectID: "p1", Status: ssmodel.StatusEnabled}
	rt := &env.Runtime{
		Config: &env.Config{
			Auth:       env.AuthConfig{JWTSecret: "0123456789abcdef0123456789abcdef"},
			RelayNodes: env.RelayNodesConfig{Enabled: true},
			Inbound:    env.InboundConfig{MaxMessageSize: 26214400},
		},
		Store: &store.Store{
			RelayNode: relayNodes{node: &nodemodel.Node{
				ID: nodeID, ProjectID: "p1", ServerID: "s1", TokenHash: relaynode.HashToken(token),
			}},
			SMTPServer: smtpServers{srv: srv},
		},
	}

	app := fiber.New(fiber.Config{BodyLimit: bodyLimitFor(rt.Config), ErrorHandler: errorHandler})
	app.Server().HeaderReceived = perRequestLimits(credentialVerifier(rt))
	app.Post(relayInboundPath, func(c fiber.Ctx) error {
		return c.JSON(fiber.Map{"status": "accepted"})
	})

	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("listen: %v", err)
	}

	go func() { _ = app.Listener(ln, fiber.ListenConfig{DisableStartupMessage: true}) }()
	t.Cleanup(func() { _ = app.Shutdown() })

	control := &relayagent.Control{BaseURL: "http://" + ln.Addr().String(), HTTP: &http.Client{}}
	msg := relayagent.InboundMessage{
		EnvelopeFrom: "s@far.test", EnvelopeTo: []string{"alice@a.example"},
		ClientIP: "198.51.100.7", HELO: "mx.far.test",
		Raw: make([]byte, 20<<20),
	}

	res, err := control.Inbound(t.Context(), nodeID, token, msg)
	if err != nil {
		t.Fatalf("a 20 MiB forward from an approved node: %v", err)
	}

	if res.Status != "accepted" {
		t.Errorf("status = %q, want accepted", res.Status)
	}

	const nineMiB = 9 << 20
	for _, tc := range []struct {
		name   string
		header []string
	}{
		{"no node credential", nil},
		{"a wrong token", []string{nodemodel.ControlHeader + ": " + nodeID + ":wrong"}},
		{"an unknown node", []string{nodemodel.ControlHeader + ": 0195d1a2-7c3e-7f00-8000-00000000000b:" + token}},
		{"no separator", []string{nodemodel.ControlHeader + ": " + token}},
	} {
		if got := post(t, ln.Addr(), relayInboundPath, nineMiB, tc.header...); got != fiber.StatusRequestEntityTooLarge {
			t.Errorf("%s with %d bytes answered %d, want 413", tc.name, nineMiB, got)
		}
	}

	// A node not yet approved is handed nothing larger either.
	srv.Status = ssmodel.StatusPending
	header := nodemodel.ControlHeader + ": " + nodeID + ":" + token
	if got := post(t, ln.Addr(), relayInboundPath, nineMiB, header); got != fiber.StatusRequestEntityTooLarge {
		t.Errorf("an unapproved node with %d bytes answered %d, want 413", nineMiB, got)
	}
}
