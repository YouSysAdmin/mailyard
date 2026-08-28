// Mailyard, Copyright (c) 2021-2026 YouSysAdmin

package email

import (
	"context"
	"io"
	"log/slog"
	"testing"

	"github.com/yousysadmin/mailyard/internal/domain/store"
	emailmodel "github.com/yousysadmin/mailyard/internal/models/email"
	projmodel "github.com/yousysadmin/mailyard/internal/models/project"
	ssmodel "github.com/yousysadmin/mailyard/internal/models/smtpserver"
)

type returnPathProjects struct {
	store.ProjectStore
	proj *projmodel.Project
}

func (f *returnPathProjects) Get(context.Context, string) (*projmodel.Project, error) {
	return f.proj, nil
}

func returnPathProcessor(platform string, proj *projmodel.Project) *Processor {
	return &Processor{
		Store:         &store.Store{Project: &returnPathProjects{proj: proj}},
		Log:           slog.New(slog.NewTextHandler(io.Discard, nil)),
		BounceAddress: platform,
	}
}

// The server decides, not the project and not the installation.
//
// A receiver checks the return path's SPF against the IP that
// connected, so the only domain that can carry it is one that
// authorizes that IP. The shared pool sends from platform IPs, a
// project's own server from the project's.
func TestReturnPathFollowsTheServer(t *testing.T) {
	tenant := &projmodel.Project{ID: "e66e7a4d-9e6c-4884-869a-cf9ffcf22181", BounceAddress: "bounces@bounce.user.com"}

	cases := []struct {
		name     string
		platform string
		proj     *projmodel.Project
		srv      *ssmodel.Server
		want     string
	}{
		{
			// No ProjectID is how a shared row is told apart - see
			// resolveShared.
			name:     "shared pool takes the platform address",
			platform: "bounces@mail.example.com",
			proj:     tenant,
			srv:      &ssmodel.Server{ID: "e225ddc5-d236-4b3d-8892-08db6fdc9696"},
			want:     "bounces@mail.example.com",
		},
		{
			name:     "the project's own server takes the project address",
			platform: "bounces@mail.example.com",
			proj:     tenant,
			srv:      &ssmodel.Server{ID: "e225ddc5-d236-4b3d-8892-08db6fdc9696", ProjectID: "e66e7a4d-9e6c-4884-869a-cf9ffcf22181"},
			want:     "bounces@bounce.user.com",
		},
		{
			// The aligned default, and it has to stay the default. As
			// the ADDRESS, not as an empty string: a pull node takes an
			// empty envelope literally and sends MAIL FROM:<>.
			name:     "neither configured leaves MAIL FROM as the From address",
			platform: "",
			proj:     &projmodel.Project{ID: "e66e7a4d-9e6c-4884-869a-cf9ffcf22181"},
			srv:      &ssmodel.Server{ID: "e225ddc5-d236-4b3d-8892-08db6fdc9696", ProjectID: "e66e7a4d-9e6c-4884-869a-cf9ffcf22181"},
			want:     "noreply@user.com",
		},
		{
			// The platform's own relay with nothing configured is the
			// case found live: every message it carried left as a
			// null sender and no bounce could name it.
			name:     "a platform server with no address takes the From address",
			platform: "",
			proj:     tenant,
			srv:      &ssmodel.Server{ID: "e225ddc5-d236-4b3d-8892-08db6fdc9696"},
			want:     "noreply@user.com",
		},
		{
			// The platform address must not leak onto a tenant's relay:
			// our SPF does not authorize their IP, so it would fail.
			name:     "a tenant server with no address does not borrow the platform one",
			platform: "bounces@mail.example.com",
			proj:     &projmodel.Project{ID: "e66e7a4d-9e6c-4884-869a-cf9ffcf22181"},
			srv:      &ssmodel.Server{ID: "e225ddc5-d236-4b3d-8892-08db6fdc9696", ProjectID: "e66e7a4d-9e6c-4884-869a-cf9ffcf22181"},
			want:     "noreply@user.com",
		},
		{
			// And the reverse: the shared pool must not borrow the
			// project's, because the project's SPF does not authorize
			// platform IPs either.
			name:     "the shared pool does not borrow the project address",
			platform: "",
			proj:     tenant,
			srv:      &ssmodel.Server{ID: "e225ddc5-d236-4b3d-8892-08db6fdc9696"},
			want:     "noreply@user.com",
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			p := returnPathProcessor(tc.platform, tc.proj)
			e := &emailmodel.Email{ProjectID: "e66e7a4d-9e6c-4884-869a-cf9ffcf22181", Sender: "Noreply <noreply@user.com>"}
			got := p.returnPathFor(t.Context(), e, tc.srv)
			if got != tc.want {
				t.Errorf("return path is %q, want %q", got, tc.want)
			}
		})
	}
}

// Failover can cross from one kind of server to the other, and the
// envelope has to follow. Carrying the platform return path onto a
// tenant IP is a guaranteed SPF failure, which is the same shape of
// bug the per-candidate skip_dkim handling exists to prevent.
func TestFailoverRecomputesTheReturnPathPerServer(t *testing.T) {
	script := &scriptedSend{byHost: map[string]error{"shared": transient()}}
	shared := srv("shared", func(s *ssmodel.Server) { s.Host = "shared"; s.ProjectID = "" })
	own := srv("own", func(s *ssmodel.Server) { s.Host = "own"; s.ProjectID = "proj-a" })

	p := failoverProcessor(t, []*ssmodel.Server{shared, own}, script)
	p.BounceAddress = "bounces@mail.example.com"
	p.Store.Project = &returnPathProjects{proj: &projmodel.Project{
		ID: "proj-a", BounceAddress: "bounces@bounce.user.com",
	}}

	p.Process(t.Context(), delivery())

	if len(script.envelopes) != 2 {
		t.Fatalf("tried %d servers, want 2: %v", len(script.envelopes), script.envelopes)
	}

	if script.envelopes[0] != "bounces@mail.example.com" {
		t.Errorf("the shared server got envelope %q, want the platform address", script.envelopes[0])
	}

	if script.envelopes[1] != "bounces@bounce.user.com" {
		t.Errorf("the project server got envelope %q, want the project address - carrying the platform one here fails SPF",
			script.envelopes[1])
	}
}
