// Mailyard, Copyright (c) 2021-2026 YouSysAdmin

package email

import (
	"testing"

	emailmodel "github.com/yousysadmin/mailyard/internal/models/email"
)

// The precedence, which is the part worth pinning: every path records
// a person, so reading CreatedBy first would report every submission
// and every campaign send as somebody sitting in the console.
func TestSentViaPrefersTheMostSpecificOrigin(t *testing.T) {
	cases := []struct {
		name       string
		email      *emailmodel.Email
		campaignID string
		wantKind   string
		wantCamp   string
	}{
		{
			name:     "a submission credential, alongside the person who minted it",
			email:    &emailmodel.Email{CredentialID: "cred-1", CreatedBy: "user-1"},
			wantKind: SentViaSubmission,
		},
		{
			name:     "an api key, alongside the person who created it",
			email:    &emailmodel.Email{APIKeyID: "key-1", CreatedBy: "user-1"},
			wantKind: SentViaAPIKey,
		},
		{
			name:       "a campaign send, which also records its author",
			email:      &emailmodel.Email{CreatedBy: "user-1"},
			campaignID: "camp-1",
			wantKind:   SentViaCampaign,
			wantCamp:   "camp-1",
		},
		{
			name:     "a person in the console, which is what is left",
			email:    &emailmodel.Email{CreatedBy: "user-1"},
			wantKind: SentViaConsole,
		},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			via := sentViaKind(c.email, c.campaignID)
			if via == nil {
				t.Fatalf("no origin for %+v", c.email)
			}

			if via.Kind != c.wantKind {
				t.Errorf("kind is %q, want %q", via.Kind, c.wantKind)
			}

			if via.CampaignID != c.wantCamp {
				t.Errorf("campaign id is %q, want %q", via.CampaignID, c.wantCamp)
			}
		})
	}
}

// A row from before this column existed, or one written by a path
// that records nothing, gets no origin rather than a made-up one.
func TestSentViaIsAbsentWhenTheRowNamesNothing(t *testing.T) {
	if via := sentViaKind(&emailmodel.Email{}, ""); via != nil {
		t.Errorf("an empty row reported %+v", via)
	}
}
