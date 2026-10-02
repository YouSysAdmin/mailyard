// Mailyard, Copyright (c) 2021-2026 YouSysAdmin

package setting

import (
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/yousysadmin/mailyard/internal/core/ids"
	"github.com/yousysadmin/mailyard/internal/domain/email"
	"github.com/yousysadmin/mailyard/internal/domain/store"
	dmodel "github.com/yousysadmin/mailyard/internal/models/domain"
	projmodel "github.com/yousysadmin/mailyard/internal/models/project"
	ssmodel "github.com/yousysadmin/mailyard/internal/models/smtpserver"
)

type oneProject struct {
	store.ProjectStore
	p *projmodel.Project
}

func (f oneProject) Get(_ context.Context, id string) (*projmodel.Project, error) {
	if f.p != nil && f.p.ID == id {
		return f.p, nil
	}

	return nil, nil
}

type oneDomain struct {
	store.DomainStore
	d *dmodel.Domain
}

func (f oneDomain) GetVerifiedCovering(_ context.Context, name string) (*dmodel.Domain, error) {
	if f.d != nil && (name == f.d.Domain || strings.HasSuffix(name, "."+f.d.Domain)) {
		return f.d, nil
	}

	return nil, nil
}

type noOwnServers struct{ store.SMTPServerStore }

func (noOwnServers) Count(context.Context, string) (int, error) { return 0, nil }
func (noOwnServers) ListInGroup(context.Context, string, string) ([]*ssmodel.Server, error) {
	return nil, nil
}

type defaultGroup struct{ store.SMTPGroupStore }

func (defaultGroup) GetDefault(_ context.Context, projID string) (*ssmodel.Group, error) {
	return &ssmodel.Group{ID: "g", ProjectID: projID, Slug: "default", Default: true}, nil
}

type pool struct {
	store.SharedSMTPStore
	servers []*ssmodel.Shared
}

func (f pool) ListEnabled(context.Context) ([]*ssmodel.Shared, error) { return f.servers, nil }

// The setting is refused for every reason a send through it would
// fail, at the moment the operator can act on it: not an id, not a
// project, a From on a domain the project has not verified, or no
// server to carry it. Empty is the shared pool and always passes.
func TestPlatformMailProjectIsCheckedAgainstTheAddress(t *testing.T) {
	projID := ids.New()
	shared := &ssmodel.Shared{}
	shared.ID, shared.Name, shared.Host, shared.Port = "pool-1", "pool-1", "pool-1.example.net", 587
	shared.Status, shared.SecurityMode = ssmodel.StatusEnabled, ssmodel.SecurityPermissive
	shared.AllowedEmails, shared.AllowedDomains = []string{}, []string{}

	st := func(servers ...*ssmodel.Shared) *store.Store {
		return &store.Store{
			Project:    oneProject{p: &projmodel.Project{ID: projID, Name: "Platform"}},
			Domain:     oneDomain{d: &dmodel.Domain{Domain: "example.com", ProjectID: projID, Verified: true}},
			SMTPServer: noOwnServers{},
			SMTPGroup:  defaultGroup{},
			SharedSMTP: pool{servers: servers},
		}
	}

	ctx := t.Context()
	if err := validatePlatformMailProject(ctx, st(), "", "x@other.test"); err != nil {
		t.Errorf("empty project: %v, want nil", err)
	}

	for name, id := range map[string]string{"garbage": "banana", "missing": ids.New()} {
		err := validatePlatformMailProject(ctx, st(shared), id, "no-reply@example.com")
		if !errors.Is(err, errBadSetting) {
			t.Errorf("%s project: %v, want errBadSetting", name, err)
		}
	}

	err := validatePlatformMailProject(ctx, st(shared), projID, "no-reply@other.test")
	if _, ok := errors.AsType[*email.RequestError](err); !ok {
		t.Errorf("unverified domain: %v, want the sender refusal", err)
	}

	if err := validatePlatformMailProject(ctx, st(), projID, "no-reply@example.com"); !errors.Is(err, errBadSetting) {
		t.Errorf("no server: %v, want errBadSetting", err)
	}

	if err := validatePlatformMailProject(ctx, st(shared), projID, "no-reply@example.com"); err != nil {
		t.Errorf("verified domain and a pool server: %v, want nil", err)
	}

	// With no From yet, only the project itself is judged.
	if err := validatePlatformMailProject(ctx, st(), projID, ""); err != nil {
		t.Errorf("project without a From: %v, want nil", err)
	}
}
