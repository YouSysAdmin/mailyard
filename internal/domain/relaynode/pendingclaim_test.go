// Mailyard, Copyright (c) 2021-2026 YouSysAdmin

package relaynode

import (
	"context"
	"encoding/json/v2"
	"io"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/gofiber/fiber/v3"

	"github.com/yousysadmin/mailyard/internal/core/env"
	"github.com/yousysadmin/mailyard/internal/domain/store"
	nodemodel "github.com/yousysadmin/mailyard/internal/models/relaynode"
	ssmodel "github.com/yousysadmin/mailyard/internal/models/smtpserver"
)

const (
	pendingNodeID   = "0195d1a2-7c3e-7f00-8000-0000000000aa"
	pendingServerID = "0195d1a2-7c3e-7f00-8000-0000000000bb"
)

type pendingNodes struct {
	store.RelayNodeStore
	extended bool
}

func (p *pendingNodes) Get(context.Context, string) (*nodemodel.Node, error) {
	return &nodemodel.Node{ID: pendingNodeID, ServerID: pendingServerID, TokenHash: HashToken("tok"),
		Mode: nodemodel.ModePull}, nil
}

func (p *pendingNodes) ExtendAssignments(context.Context, string, []string, time.Time) error {
	p.extended = true

	return nil
}

type pendingShared struct{ store.SharedSMTPStore }

func (pendingShared) Get(context.Context, string) (*ssmodel.Shared, error) {
	return &ssmodel.Shared{Server: ssmodel.Server{ID: pendingServerID, Status: ssmodel.StatusPending}}, nil
}

// A node waiting for approval claims on a loop. Its claim parks and
// answers empty, rather than a refusal every few seconds that the log
// records as a server error, and what it holds is not kept alive.
func TestAPendingNodesClaimParksAndAnswersEmpty(t *testing.T) {
	nodes := &pendingNodes{}
	cfg := &env.Config{}
	cfg.RelayNodes.ClaimWaitMax = 100 * time.Millisecond
	cfg.RelayNodes.ClaimMax = 10

	h := &Handler{Runtime: &env.Runtime{
		Config: cfg,
		Store:  &store.Store{RelayNode: nodes, SharedSMTP: pendingShared{}},
	}}
	app := fiber.New()
	app.Post("/claim", h.Claim)

	body := `{"node_id": "` + pendingNodeID + `", "token": "tok", "wait_seconds": 30, "holding": ["0195d1a2-7c3e-7f00-8000-0000000000ee"]}`
	req := httptest.NewRequest("POST", "/claim", strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")

	start := time.Now()
	res, err := app.Test(req, fiber.TestConfig{Timeout: 5 * time.Second})
	if err != nil {
		t.Fatal(err)
	}

	defer func() { _ = res.Body.Close() }()

	raw, _ := io.ReadAll(res.Body)
	if res.StatusCode != fiber.StatusOK {
		t.Fatalf("pending claim answered %d: %s", res.StatusCode, raw)
	}

	var out claimOutput
	if err := json.Unmarshal(raw, &out); err != nil || len(out.Messages) != 0 {
		t.Errorf("pending claim body %s, %v - want no messages", raw, err)
	}

	if took := time.Since(start); took < 90*time.Millisecond {
		t.Errorf("pending claim answered after %v, want it parked for the wait", took)
	}

	if nodes.extended {
		t.Error("a pending node's holdings were extended")
	}
}
