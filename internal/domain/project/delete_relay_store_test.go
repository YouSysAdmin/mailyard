// Mailyard, Copyright (c) 2021-2026 YouSysAdmin

package project

import (
	"context"
	"net/http/httptest"
	"testing"

	"github.com/gofiber/fiber/v3"

	"github.com/yousysadmin/mailyard/internal/core/env"
	"github.com/yousysadmin/mailyard/internal/domain"
	"github.com/yousysadmin/mailyard/internal/domain/store"
	nodemodel "github.com/yousysadmin/mailyard/internal/models/relaynode"
	usermodel "github.com/yousysadmin/mailyard/internal/models/user"
)

type noKeys struct{}

func (noKeys) StorageKeysForProject(context.Context, string) ([]string, error) { return nil, nil }

type keylessEmail struct {
	store.EmailStore
	noKeys
}

func (k keylessEmail) StorageKeysForProject(ctx context.Context, p string) ([]string, error) {
	return k.noKeys.StorageKeysForProject(ctx, p)
}

type keylessInbound struct {
	store.InboundStore
	noKeys
}

func (k keylessInbound) StorageKeysForProject(ctx context.Context, p string) ([]string, error) {
	return k.noKeys.StorageKeysForProject(ctx, p)
}

type keylessTemplate struct {
	store.TemplateStore
	noKeys
}

func (k keylessTemplate) StorageKeysForProject(ctx context.Context, p string) ([]string, error) {
	return k.noKeys.StorageKeysForProject(ctx, p)
}

type projectNodes struct {
	store.RelayNodeStore
	nodes map[string][]*nodemodel.Node
}

func (p projectNodes) List(_ context.Context, projID string) ([]*nodemodel.Node, error) {
	return p.nodes[projID], nil
}

type forgottenCerts struct {
	store.CertificateStore
	deleted []string
}

func (f *forgottenCerts) Delete(_ context.Context, scope, name string) error {
	f.deleted = append(f.deleted, scope+"/"+name)

	return nil
}

// Deleting a project drops the certificate record of each relay node it
// enrolled. The nodes cascade away with the project, and a record left
// behind would expire into a daily alert nobody can clear.
func TestDeletingAProjectForgetsItsRelayNodeCertificates(t *testing.T) {
	st, proj, _ := roleFixture(t)
	owner := addRoleMember(t, st, proj, true)

	certs := &forgottenCerts{}
	h := &Handler{Runtime: &env.Runtime{
		Config: &env.Config{},
		Store: &store.Store{
			Project:     st,
			Email:       keylessEmail{},
			Inbound:     keylessInbound{},
			Template:    keylessTemplate{},
			RelayNode:   projectNodes{nodes: map[string][]*nodemodel.Node{proj: {{ID: "node-a"}, {ID: "node-b"}}}},
			Certificate: certs,
		},
	}}

	app := fiber.New()
	app.Use(func(c fiber.Ctx) error {
		c.Locals(domain.ContextKey, &domain.RequestContext{User: &usermodel.User{ID: owner}})

		return c.Next()
	})
	app.Delete("/projects/:id", h.Delete)

	res, err := app.Test(httptest.NewRequest(fiber.MethodDelete, "/projects/"+proj, nil))
	if err != nil {
		t.Fatalf("delete: %v", err)
	}

	_ = res.Body.Close()
	if res.StatusCode != fiber.StatusNoContent {
		t.Fatalf("delete project: %d, want 204", res.StatusCode)
	}

	if got := len(certs.deleted); got != 2 || certs.deleted[0] != "relay-node/node-a" || certs.deleted[1] != "relay-node/node-b" {
		t.Errorf("certificate records dropped: %v, want both nodes", certs.deleted)
	}
}
