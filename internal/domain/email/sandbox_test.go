// Mailyard, Copyright (c) 2021-2026 YouSysAdmin

package email

import (
	"strings"
	"testing"

	"github.com/yousysadmin/mailyard/internal/domain"
	akmodel "github.com/yousysadmin/mailyard/internal/models/apikey"
)

func ctxWithKey(sandbox bool) *domain.RequestContext {
	return &domain.RequestContext{APIKey: &akmodel.Key{ID: "eea04bb8-feb6-49e8-8e3d-69758376292a", Sandbox: sandbox}}
}

// The one-way switch, which is the whole safety property of the
// feature. A sandbox credential exists so an application CANNOT send
// real mail, and a request body that could undo that would put the
// decision back where it does not belong.
func TestSandboxIsOneWay(t *testing.T) {
	cases := []struct {
		name        string
		rc          *domain.RequestContext
		body        *bool
		wantCapture bool
		wantRefusal bool
	}{
		{
			name: "a sandbox key captures with no field at all",
			rc:   ctxWithKey(true), body: nil, wantCapture: true,
		},
		{
			name: "a sandbox key still captures when the body agrees",
			rc:   ctxWithKey(true), body: new(true), wantCapture: true,
		},
		{
			name: "a sandbox key asking to send for real is REFUSED",
			rc:   ctxWithKey(true), body: new(false), wantRefusal: true,
		},
		{
			name: "an ordinary key sends by default",
			rc:   ctxWithKey(false), body: nil,
		},
		{
			name: "an ordinary key may opt in for one message",
			rc:   ctxWithKey(false), body: new(true), wantCapture: true,
		},
		{
			name: "an ordinary key saying false is simply a send",
			rc:   ctxWithKey(false), body: new(false),
		},
		{
			name: "a session request carries no key and sends",
			rc:   &domain.RequestContext{}, body: nil,
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			want, refusal := sandboxIntent(tc.rc, &sendInput{Sandbox: tc.body})
			if tc.wantRefusal {
				if refusal == "" {
					t.Fatal("an opt-out from a sandbox key was allowed through")
				}

				if want {
					t.Error("a refused request also reported that it wanted capture")
				}

				return
			}

			if refusal != "" {
				t.Fatalf("unexpected refusal: %s", refusal)
			}

			if want != tc.wantCapture {
				t.Errorf("capture = %v, want %v", want, tc.wantCapture)
			}
		})
	}
}

// The refusal has to be an error a person can act on. "Forbidden"
// leaves a developer staring at a key that used to work.
func TestTheRefusalSaysWhatToDo(t *testing.T) {
	_, refusal := sandboxIntent(ctxWithKey(true), &sendInput{Sandbox: new(false)})
	if refusal == "" {
		t.Fatal("no refusal")
	}

	for _, want := range []string{"sandbox-only", "without the sandbox flag"} {
		if !strings.Contains(refusal, want) {
			t.Errorf("refusal %q does not mention %q", refusal, want)
		}
	}
}
