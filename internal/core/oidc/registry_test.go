// Mailyard, Copyright (c) 2021-2026 YouSysAdmin

package oidc

import (
	"encoding/json/v2"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	opmodel "github.com/yousysadmin/mailyard/internal/models/oauthprovider"
)

// discoveryServer serves a minimal OIDC discovery document whose
// issuer carries a TRAILING SLASH, the way JumpCloud publishes its
// own. Everything else is the least a document may carry.
func discoveryServer(t *testing.T) *httptest.Server {
	t.Helper()
	mux := http.NewServeMux()
	srv := httptest.NewServer(mux)
	t.Cleanup(srv.Close)
	mux.HandleFunc("/.well-known/openid-configuration", func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_ = json.MarshalWrite(w, map[string]any{
			"issuer":                 srv.URL + "/",
			"authorization_endpoint": srv.URL + "/auth",
			"token_endpoint":         srv.URL + "/token",
			"jwks_uri":               srv.URL + "/jwks",
		})
	})

	return srv
}

// TestDiscoveryForgivesTheTrailingSlash pins the retry: an issuer
// entered without the slash a provider's document carries must still
// discover, because the document is the authority on its own issuer
// and the spec's exact-string comparison would otherwise refuse a
// mismatch we can resolve ourselves. JumpCloud is the provider that
// made this real.
func TestDiscoveryForgivesTheTrailingSlash(t *testing.T) {
	srv := discoveryServer(t)

	r := NewRegistry("https://mail.example.test")
	p := &opmodel.Provider{
		Slug:     "jumpcloud",
		Type:     opmodel.TypeOIDC,
		ClientID: "cid",
		// No trailing slash, unlike the document.
		Issuer:               srv.URL,
		RequireEmailVerified: true,
		UpdatedAt:            time.Now().UTC(),
	}

	prov, err := r.For(t.Context(), p)
	if err != nil {
		t.Fatalf("discovery refused the slash-only mismatch: %v", err)
	}

	if prov.cfg.Issuer != srv.URL+"/" {
		t.Fatalf("issuer = %q, want the document's own %q", prov.cfg.Issuer, srv.URL+"/")
	}
}
