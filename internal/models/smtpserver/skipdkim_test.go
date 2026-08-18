// Mailyard, Copyright (c) 2021-2026 YouSysAdmin

package smtpserver

import (
	"testing"

	"github.com/yousysadmin/mailyard/internal/core/transport"
)

// The row cannot bring a broken signature back.
//
// A SES row created before the provider column existed carries
// skip_dkim = false, and a PATCH can clear the flag on any row. Either
// would have put our signature on a message SES then rewrites and
// re-signs, which arrives broken - and a broken signature is IGNORED
// rather than punished, so the only symptom is mail that stopped being
// authenticated by us.
//
// Computed from the provider, so neither is possible.
func TestASigningProviderIsNeverSignedForLocally(t *testing.T) {
	for name, tc := range map[string]struct {
		srv  Server
		want bool
	}{
		"ses with the flag off": {
			Server{Provider: transport.ProviderSES, SkipDKIM: false}, true,
		},
		"ses with the flag on": {
			Server{Provider: transport.ProviderSES, SkipDKIM: true}, true,
		},

		// An ordinary dial does not touch the signed headers, so there the
		// flag is a real choice and stays one.
		"smtp with the flag off": {Server{SkipDKIM: false}, false},
		"smtp with the flag on":  {Server{SkipDKIM: true}, true},
	} {
		if got := tc.srv.SkipsDKIM(); got != tc.want {
			t.Errorf("%s: SkipsDKIM() = %v, want %v", name, got, tc.want)
		}
	}
}
