// Mailyard, Copyright (c) 2021-2026 YouSysAdmin

package email

import (
	"context"
	"io"
	"log/slog"
	"strings"
	"testing"

	"github.com/yousysadmin/mailyard/internal/domain/store"
	bmodel "github.com/yousysadmin/mailyard/internal/models/bounce"
	dmodel "github.com/yousysadmin/mailyard/internal/models/domain"
	emailmodel "github.com/yousysadmin/mailyard/internal/models/email"
	projmodel "github.com/yousysadmin/mailyard/internal/models/project"
	ssmodel "github.com/yousysadmin/mailyard/internal/models/smtpserver"
)

// ----------------------------------------------------------------------------
// Fakes: only the methods pickServer reaches
// ----------------------------------------------------------------------------

type fakeProjectServers struct {
	store.SMTPServerStore
	count   int
	inGroup []*ssmodel.Server
}

func (f *fakeProjectServers) Count(context.Context, string) (int, error) { return f.count, nil }
func (f *fakeProjectServers) ListInGroup(context.Context, string, string) ([]*ssmodel.Server, error) {
	return f.inGroup, nil
}

// fakeGroups is a project with exactly the default group. Resolution
// runs through it now, so a test without one would exercise the
// wiring-failure path instead of the behaviour it means to check.
type fakeGroups struct {
	store.SMTPGroupStore
}

func (f *fakeGroups) GetDefault(_ context.Context, projID string) (*ssmodel.Group, error) {
	return &ssmodel.Group{ID: "g-default", ProjectID: projID, Slug: "default", Default: true}, nil
}

type fakeSharedPool struct {
	store.SharedSMTPStore
	servers []*ssmodel.Shared
}

func (f *fakeSharedPool) ListEnabled(context.Context) ([]*ssmodel.Shared, error) {
	return f.servers, nil
}

type fakeDomains struct {
	store.DomainStore
	verified *dmodel.Domain
}

func (f *fakeDomains) GetVerifiedByName(_ context.Context, name string) (*dmodel.Domain, error) {
	if f.verified != nil && f.verified.Domain == name {
		return f.verified, nil
	}

	return nil, nil
}

// GetVerifiedCovering answers for the name and anything under it,
// mirroring the store. Spelled out rather than delegating, so a test
// that expects a subdomain to be covered is asserting behaviour rather
// than agreeing with the implementation about itself.
func (f *fakeDomains) GetVerifiedCovering(_ context.Context, name string) (*dmodel.Domain, error) {
	if f.verified == nil {
		return nil, nil
	}

	if name == f.verified.Domain || strings.HasSuffix(name, "."+f.verified.Domain) {
		return f.verified, nil
	}

	return nil, nil
}

func sharedServer(name string, mutate func(*ssmodel.Shared)) *ssmodel.Shared {
	s := &ssmodel.Shared{
		Server: ssmodel.Server{
			ID: name, Name: name, Host: name + ".example.net", Port: 587,
			Status: ssmodel.StatusEnabled, AllowedEmails: []string{},
		},
		AllowedDomains: []string{},
		SecurityMode:   ssmodel.SecurityPermissive,
	}
	if mutate != nil {
		mutate(s)
	}

	return s
}

func processorWith(owned int, ownServers []*ssmodel.Server, pool []*ssmodel.Shared, verified *dmodel.Domain) *Processor {
	return &Processor{
		Store: &store.Store{
			SMTPServer: &fakeProjectServers{count: owned, inGroup: ownServers},
			SMTPGroup:  &fakeGroups{},
			SharedSMTP: &fakeSharedPool{servers: pool},
			Domain:     &fakeDomains{verified: verified},
		},
		Log: slog.New(slog.NewTextHandler(io.Discard, nil)),
	}
}

func job() *emailmodel.Email {
	return &emailmodel.Email{ID: "9e2f6f11-cdd3-4058-86f2-29f3ad60b06a", ProjectID: "proj-a", Sender: "hi@example.com"}
}

func TestSharedPoolIsUsedWhenTheProjectOwnsNothing(t *testing.T) {
	p := processorWith(0, nil, []*ssmodel.Shared{sharedServer("pool-1", nil)}, nil)
	srv, err := p.pickServer(t.Context(), job())
	if err != nil {
		t.Fatal(err)
	}

	if srv == nil || srv.Name != "pool-1" {
		t.Fatalf("got %v, want the shared server", srv)
	}

	if srv.ProjectID != "" {
		t.Errorf("shared server came back with project_id %q, want empty - it belongs to the platform", srv.ProjectID)
	}
}

// Owning a server means owning delivery. A project whose only server
// is disabled must fail loudly rather than have its mail quietly
// leave through platform credentials, from a different IP and under a
// different SPF record than the operator set up.
func TestSharedPoolIsSkippedWhenTheProjectOwnsAnyServer(t *testing.T) {
	p := processorWith(1, nil, []*ssmodel.Shared{sharedServer("pool-1", nil)}, nil)
	srv, err := p.pickServer(t.Context(), job())
	if err != nil {
		t.Fatal(err)
	}

	if srv != nil {
		t.Fatalf("got %v, want nil - the project owns a server, so the pool is not its business", srv)
	}
}

func TestProjectServerWinsOverThePool(t *testing.T) {
	own := &ssmodel.Server{ID: "own", Name: "own", ProjectID: "proj-a", Status: ssmodel.StatusEnabled}
	p := processorWith(1, []*ssmodel.Server{own}, []*ssmodel.Shared{sharedServer("pool-1", nil)}, nil)
	srv, err := p.pickServer(t.Context(), job())
	if err != nil {
		t.Fatal(err)
	}

	if srv == nil || srv.Name != "own" {
		t.Fatalf("got %v, want the project's own server", srv)
	}
}

func TestSharedPoolHonoursAllowedDomains(t *testing.T) {
	pool := []*ssmodel.Shared{
		sharedServer("other-only", func(s *ssmodel.Shared) {
			s.AllowedDomains = []string{"elsewhere.test"}
		}),
		sharedServer("ours", func(s *ssmodel.Shared) {
			s.AllowedDomains = []string{"example.com"}
		}),
	}
	p := processorWith(0, nil, pool, nil)
	srv, err := p.pickServer(t.Context(), job())
	if err != nil {
		t.Fatal(err)
	}

	if srv == nil || srv.Name != "ours" {
		t.Fatalf("got %v, want the server whose allowed_domains admits example.com", srv)
	}
}

func TestSharedPoolHonoursAllowedEmails(t *testing.T) {
	pool := []*ssmodel.Shared{
		sharedServer("no", func(s *ssmodel.Shared) {
			s.AllowedEmails = []string{"someone@example.com"}
		}),
		sharedServer("yes", func(s *ssmodel.Shared) {
			s.AllowedEmails = []string{"*@example.com"}
		}),
	}
	p := processorWith(0, nil, pool, nil)
	srv, err := p.pickServer(t.Context(), job())
	if err != nil {
		t.Fatal(err)
	}

	if srv == nil || srv.Name != "yes" {
		t.Fatalf("got %v, want the server whose allowed_emails admits the sender", srv)
	}
}

// Strict mode is the rule that stops one tenant relaying as another's
// domain through platform credentials. Domain names are globally
// unique, so a verified row belonging to a different project must not
// count.
func TestStrictSharedServerNeedsTheSendingProjectsOwnVerifiedDomain(t *testing.T) {
	strict := []*ssmodel.Shared{
		sharedServer("strict", func(s *ssmodel.Shared) { s.SecurityMode = ssmodel.SecurityStrict }),
	}

	cases := []struct {
		name     string
		verified *dmodel.Domain
		want     bool
	}{
		{"unverified domain", nil, false},
		{"verified to another project", &dmodel.Domain{Domain: "example.com", ProjectID: "proj-b"}, false},
		{"verified to this project", &dmodel.Domain{Domain: "example.com", ProjectID: "proj-a"}, true},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			p := processorWith(0, nil, strict, c.verified)
			srv, err := p.pickServer(t.Context(), job())
			if err != nil {
				t.Fatal(err)
			}

			if got := srv != nil; got != c.want {
				t.Fatalf("picked=%v, want %v", got, c.want)
			}
		})
	}
}

// A permissive server does not consult domain verification at all,
// which is the whole difference between the two modes.
func TestPermissiveSharedServerNeedsNoVerifiedDomain(t *testing.T) {
	p := processorWith(0, nil, []*ssmodel.Shared{sharedServer("open", nil)}, nil)
	srv, err := p.pickServer(t.Context(), job())
	if err != nil {
		t.Fatal(err)
	}

	if srv == nil {
		t.Fatal("a permissive shared server was skipped for an unverified domain")
	}
}

func TestNoSharedStoreIsNotAFailure(t *testing.T) {
	p := &Processor{
		Store: &store.Store{SMTPServer: &fakeProjectServers{count: 0}},
		Log:   slog.New(slog.NewTextHandler(io.Discard, nil)),
	}
	srv, err := p.pickServer(t.Context(), job())
	if err != nil {
		t.Fatalf("a runtime without a shared pool must not error: %v", err)
	}

	if srv != nil {
		t.Fatalf("got %v, want nil", srv)
	}
}

// ----------------------------------------------------------------------------
// Group routing
// ----------------------------------------------------------------------------

// fakeRoutedGroups answers by id as well as by default, so a test can
// exercise a named group and a deleted one.
type fakeRoutedGroups struct {
	store.SMTPGroupStore
	byID map[string]*ssmodel.Group
}

func (f *fakeRoutedGroups) Get(_ context.Context, projID, id string) (*ssmodel.Group, error) {
	return f.byID[id], nil
}
func (f *fakeRoutedGroups) GetDefault(_ context.Context, projID string) (*ssmodel.Group, error) {
	return &ssmodel.Group{ID: "g-default", ProjectID: projID, Slug: "default", Default: true}, nil
}

// fakeGroupServers returns different servers per group id.
type fakeGroupServers struct {
	store.SMTPServerStore
	count   int
	byGroup map[string][]*ssmodel.Server
	byID    map[string]*ssmodel.Server
}

func (f *fakeGroupServers) Count(context.Context, string) (int, error) { return f.count, nil }
func (f *fakeGroupServers) ListInGroup(_ context.Context, _, groupID string) ([]*ssmodel.Server, error) {
	return f.byGroup[groupID], nil
}
func (f *fakeGroupServers) Get(_ context.Context, _, id string) (*ssmodel.Server, error) {
	return f.byID[id], nil
}

func srv(id string, mutate func(*ssmodel.Server)) *ssmodel.Server {
	s := &ssmodel.Server{ID: id, Name: id, ProjectID: "proj-a", Status: ssmodel.StatusEnabled}
	if mutate != nil {
		mutate(s)
	}

	return s
}

func routedStore(byGroup map[string][]*ssmodel.Server, byID map[string]*ssmodel.Server,
	groups map[string]*ssmodel.Group) *store.Store {
	count := 0
	for _, list := range byGroup {
		count += len(list)
	}

	return &store.Store{
		SMTPServer: &fakeGroupServers{count: count, byGroup: byGroup, byID: byID},
		SMTPGroup:  &fakeRoutedGroups{byID: groups},
		SharedSMTP: &fakeSharedPool{servers: []*ssmodel.Shared{sharedServer("pool", nil)}},
		Domain:     &fakeDomains{},
	}
}

func TestNamedGroupIsUsedOverTheDefault(t *testing.T) {
	st := routedStore(
		map[string][]*ssmodel.Server{
			"g-default": {srv("in-default", nil)},
			"g-bulk":    {srv("in-bulk", nil)},
		},
		nil,
		map[string]*ssmodel.Group{"g-bulk": {ID: "g-bulk", ProjectID: "proj-a", Slug: "bulk"}},
	)
	got, err := ResolveServer(t.Context(), st, "proj-a", "hi@example.com", Route{GroupID: "g-bulk"})
	if err != nil {
		t.Fatal(err)
	}

	if got == nil || got.Name != "in-bulk" {
		t.Fatalf("got %v, want the named group's server", got)
	}
}

// Deleting a group moves its servers into the default one, so a
// message queued against the old group must follow them rather than
// fail for naming something that no longer exists.
func TestQueuedRouteToADeletedGroupFallsBackToTheDefault(t *testing.T) {
	st := routedStore(
		map[string][]*ssmodel.Server{"g-default": {srv("in-default", nil)}},
		nil,
		map[string]*ssmodel.Group{}, // g-gone is not there any more
	)
	got, err := ResolveServer(t.Context(), st, "proj-a", "hi@example.com", Route{GroupID: "g-gone"})
	if err != nil {
		t.Fatal(err)
	}

	if got == nil || got.Name != "in-default" {
		t.Fatalf("got %v, want the default group's server", got)
	}
}

// A pin is a pin. Falling back would send from a server the caller
// did not choose, which for mail means a different IP and a different
// SPF record.
func TestAPinnedServerNeverFallsBack(t *testing.T) {
	st := routedStore(
		map[string][]*ssmodel.Server{"g-default": {srv("in-default", nil)}},
		map[string]*ssmodel.Server{
			"disabled": srv("disabled", func(s *ssmodel.Server) { s.Status = ssmodel.StatusDisabled }),
		},
		map[string]*ssmodel.Group{},
	)
	got, err := ResolveServer(t.Context(), st, "proj-a", "hi@example.com", Route{ServerID: "disabled"})
	if err != nil {
		t.Fatal(err)
	}

	if got != nil {
		t.Fatalf("got %v, want nil - the pinned server is disabled", got)
	}
}

// Candidates come back in pick order, and only the usable ones, so
// failover can just walk the slice.
func TestCandidatesAreFilteredAndOrdered(t *testing.T) {
	st := routedStore(
		map[string][]*ssmodel.Server{"g-default": {
			srv("first", nil),
			srv("disabled", func(s *ssmodel.Server) { s.Status = ssmodel.StatusDisabled }),
			srv("wrong-sender", func(s *ssmodel.Server) { s.AllowedEmails = []string{"*@elsewhere.test"} }),
			srv("second", nil),
		}},
		nil, map[string]*ssmodel.Group{},
	)
	got, err := ResolveCandidates(t.Context(), st, "proj-a", "hi@example.com", Route{})
	if err != nil {
		t.Fatal(err)
	}

	var names []string
	for _, s := range got {
		names = append(names, s.Name)
	}

	if len(names) != 2 || names[0] != "first" || names[1] != "second" {
		t.Fatalf("got %v, want [first second] - the store's order, minus what cannot carry it", names)
	}
}

// fakeProjects and fakeBounces cover the two stores Process touches
// outside resolution: the bounce address lookup and, on a permanent
// rejection, the bounce record.
type fakeProjects struct{ store.ProjectStore }

func (f *fakeProjects) Get(context.Context, string) (*projmodel.Project, error) {
	return &projmodel.Project{ID: "proj-a"}, nil
}

type fakeBounces struct{ store.BounceStore }

func (f *fakeBounces) Put(context.Context, *bmodel.Bounce) error { return nil }

// A pool row reserved for platform mail is not a delivery candidate.
//
// The flag exists so an operator can point invitations and password
// resets at credentials no tenant touches. If resolveShared still
// picked it up, that separation would be a label rather than a rule -
// and the failure is silent, since the mail goes out either way and
// only the reputation it goes out on differs.
func TestAReservedPoolServerCarriesNoTenantMail(t *testing.T) {
	reserved := sharedServer("platform", func(s *ssmodel.Shared) { s.PlatformOnly = true })
	open := sharedServer("tenants", nil)

	// Alone in the pool, it is as if the pool were empty. pickServer
	// answers (nil, nil) for that - no candidate is not an error here,
	// the caller turns it into one.
	p := processorWith(0, nil, []*ssmodel.Shared{reserved}, nil)
	srv, err := p.pickServer(t.Context(), job())
	if err != nil {
		t.Fatalf("pickServer: %v", err)
	}

	if srv != nil {
		t.Errorf("a project was routed through %q, a platform-only server", srv.Name)
	}

	// Beside an ordinary row, only the ordinary one is offered - and
	// the reserved one is first, so this is not passing by ordering.
	got, err := ResolveCandidates(t.Context(),
		&store.Store{
			SMTPServer: &fakeProjectServers{count: 0},
			SMTPGroup:  &fakeGroups{},
			SharedSMTP: &fakeSharedPool{servers: []*ssmodel.Shared{reserved, open}},
			Domain:     &fakeDomains{},
		},
		"proj-a", "hi@example.com", Route{})
	if err != nil {
		t.Fatalf("ResolveCandidates: %v", err)
	}

	if len(got) != 1 || got[0].Name != "tenants" {
		names := make([]string, len(got))
		for i, s := range got {
			names[i] = s.Name
		}

		t.Errorf("candidates = %v, want just [tenants]", names)
	}
}
