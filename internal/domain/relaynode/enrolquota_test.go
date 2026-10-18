// Mailyard, Copyright (c) 2021-2026 YouSysAdmin

package relaynode

import (
	"context"
	"log/slog"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/gofiber/fiber/v3"

	"github.com/yousysadmin/mailyard/internal/core/env"
	"github.com/yousysadmin/mailyard/internal/domain/store"
	akmodel "github.com/yousysadmin/mailyard/internal/models/apikey"
	pmodel "github.com/yousysadmin/mailyard/internal/models/plan"
	projmodel "github.com/yousysadmin/mailyard/internal/models/project"
	nodemodel "github.com/yousysadmin/mailyard/internal/models/relaynode"
	ssmodel "github.com/yousysadmin/mailyard/internal/models/smtpserver"
)

type quotaProjects struct{ store.ProjectStore }

func (quotaProjects) Get(_ context.Context, id string) (*projmodel.Project, error) {
	return &projmodel.Project{ID: id}, nil
}

type quotaPlans struct{ store.PlanStore }

func (quotaPlans) GetDefault(context.Context) (*pmodel.Plan, error) {
	return &pmodel.Plan{Name: "tiny", MaxSMTPServers: 1}, nil
}

// quotaServers holds one server, which fills a plan of one.
type quotaServers struct {
	store.SMTPServerStore
	puts int
}

func (s *quotaServers) Count(context.Context, string) (int, error) { return 1 + s.puts, nil }

func (s *quotaServers) Put(context.Context, *ssmodel.Server) error {
	s.puts++

	return nil
}

type quotaGroups struct{ store.SMTPGroupStore }

func (quotaGroups) EnsureDefault(_ context.Context, projID string) (*ssmodel.Group, error) {
	return &ssmodel.Group{ID: "g1", ProjectID: projID}, nil
}

type quotaNodes struct {
	store.RelayNodeStore
	puts int
}

func (*quotaNodes) ListAll(context.Context) ([]*nodemodel.Node, error) { return nil, nil }

func (n *quotaNodes) Put(context.Context, *nodemodel.Node) error {
	n.puts++

	return nil
}

type signingCA struct{ CertAuthority }

func (signingCA) SignNode(context.Context, string, string, string, []string) (string, string, error) {
	return "cert", "ca", nil
}

func (signingCA) ClientName() string { return "mailyard-worker" }

// A project node is a project server, so enrolling one is refused at
// the plan's server cap and writes nothing.
func TestAProjectEnrolmentRespectsTheServerCap(t *testing.T) {
	const raw = "myk_enrolquotaenrolquotaenrolquotaenrolq"

	key := &akmodel.Key{ID: "k1", ProjectID: "p1", KeyHash: akmodel.Hash(raw), Permissions: []string{"*"}}
	servers, nodes := &quotaServers{}, &quotaNodes{}
	cfg := &env.Config{}
	cfg.Sending.AllowPrivateSMTPTargets = true
	h := &Handler{CA: signingCA{}, Runtime: &env.Runtime{
		Config: cfg,
		Log:    slog.New(slog.DiscardHandler),
		Store: &store.Store{
			APIKey:     oneKey{key: key},
			Project:    quotaProjects{},
			Plan:       quotaPlans{},
			SMTPServer: servers,
			SMTPGroup:  quotaGroups{},
			RelayNode:  nodes,
		},
	}}

	app := fiber.New()
	app.Post("/", h.Register)

	req := httptest.NewRequest("POST", "/",
		strings.NewReader(`{"token":"`+raw+`","csr":"csr","hostname":"203.0.113.7"}`))
	req.Header.Set("Content-Type", "application/json")

	resp, err := app.Test(req)
	if err != nil {
		t.Fatal(err)
	}

	_ = resp.Body.Close()

	if resp.StatusCode != fiber.StatusTooManyRequests {
		t.Fatalf("an enrolment at the cap answered %d, want 429", resp.StatusCode)
	}

	if servers.puts != 0 || nodes.puts != 0 {
		t.Fatalf("a refused enrolment wrote %d servers and %d nodes", servers.puts, nodes.puts)
	}
}
