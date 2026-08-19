// Mailyard, Copyright (c) 2021-2026 YouSysAdmin

package oauthprovider

import (
	"net/url"
	"strings"

	"github.com/gofiber/fiber/v3"
	"github.com/yousysadmin/mailyard/internal/core/ids"

	"github.com/yousysadmin/mailyard/internal/core/env"
	"github.com/yousysadmin/mailyard/internal/core/oidc"
	"github.com/yousysadmin/mailyard/internal/core/response"
	"github.com/yousysadmin/mailyard/internal/core/validation"
	opmodel "github.com/yousysadmin/mailyard/internal/models/oauthprovider"
)

// Handler owns /api/oauth-providers. Platform admin tier - these
// decide who can sign in to the installation.
type Handler struct {
	Runtime *env.Runtime
}

// view is the API shape. The model already hides ClientSecret with
// json:"-", so this only adds the derived fields the console needs.
func view(rt *env.Runtime, p *opmodel.Provider) ProviderView {
	return ProviderView{
		ID:                   p.ID,
		Name:                 p.Name,
		Slug:                 p.Slug,
		Type:                 p.Type,
		ClientID:             p.ClientID,
		HasSecret:            p.HasSecret(),
		Issuer:               p.Issuer,
		AuthURL:              p.AuthURL,
		TokenURL:             p.TokenURL,
		UserInfoURL:          p.UserInfoURL,
		Scopes:               p.EffectiveScopes(),
		Enabled:              p.Enabled,
		Hidden:               p.Hidden,
		AutoRegister:         p.AutoRegister,
		RequireEmailVerified: p.RequireEmailVerified,
		AllowedDomains:       p.AllowedDomains,
		AllowedEmails:        p.AllowedEmails,
		GroupsClaim:          p.GroupsClaim,
		AllowedGroups:        p.AllowedGroups,
		Usable:               p.Usable(),
		CallbackURL:          callbackURL(rt, p.Slug),
		CreatedAt:            p.CreatedAt,
		UpdatedAt:            p.UpdatedAt,
	}
}

// List serves GET /api/v1/admin/oauth-providers.
func (h *Handler) List(c fiber.Ctx) error {
	ps, err := h.Runtime.Store.OAuthProvider.List(c.Context())
	if err != nil {
		return response.Internal(c, err)
	}

	out := make([]ProviderView, 0, len(ps))
	for _, p := range ps {
		out = append(out, view(h.Runtime, p))
	}

	return response.Success(c, ListResponse{Providers: out})
}

// Get serves GET /api/v1/admin/oauth-providers/:id.
func (h *Handler) Get(c fiber.Ctx) error {
	p, err := h.Runtime.Store.OAuthProvider.Get(c.Context(), c.Params("id"))
	if err != nil {
		return response.Internal(c, err)
	}

	if p == nil {
		return response.NotFound(c, "oauth provider not found")
	}

	return response.Success(c, ProviderResponse{Provider: view(h.Runtime, p)})
}

// Create serves POST /api/v1/admin/oauth-providers.
func (h *Handler) Create(c fiber.Ctx) error {
	in, resp, ok := validation.Bind[upsertInput](c)
	if !ok {
		return resp
	}

	p := &opmodel.Provider{
		ID: ids.New(),
		// Defaults chosen so a provider created with only the required
		// fields behaves the way an operator expects: on, visible, and
		// willing to create accounts.
		Enabled:      true,
		AutoRegister: true,
		// Email verification defaults ON, unlike the other two, because
		// the sign-in path links an IdP identity to an EXISTING local
		// account by email address. An IdP that hands out addresses it
		// never verified therefore hands out other people's accounts,
		// and the safe setting is the one an operator has to think
		// about before turning off.
		RequireEmailVerified: true,
	}
	if ok, resp := h.apply(c, p, in); !ok {
		return resp
	}

	if err := h.Runtime.Store.OAuthProvider.Put(c.Context(), p); err != nil {
		return response.Internal(c, err)
	}

	return response.Created(c, ProviderResponse{Provider: view(h.Runtime, p)})
}

// Update serves PATCH /api/v1/admin/oauth-providers/:id.
func (h *Handler) Update(c fiber.Ctx) error {
	p, err := h.Runtime.Store.OAuthProvider.Get(c.Context(), c.Params("id"))
	if err != nil {
		return response.Internal(c, err)
	}

	if p == nil {
		return response.NotFound(c, "oauth provider not found")
	}

	in, resp, ok := validation.Bind[upsertInput](c)
	if !ok {
		return resp
	}

	if ok, resp := h.apply(c, p, in); !ok {
		return resp
	}

	if err := h.Runtime.Store.OAuthProvider.Put(c.Context(), p); err != nil {
		return response.Internal(c, err)
	}

	// The cached discovery result is keyed on the row's updated_at, so
	// a config change invalidates it without an explicit purge. Drop
	// it anyway: an operator who just fixed a typo should not wait for
	// a TTL to see whether it worked.
	h.Runtime.OAuth.Forget(p.Slug)

	return response.Success(c, ProviderResponse{Provider: view(h.Runtime, p)})
}

// Delete serves DELETE /api/v1/admin/oauth-providers/:id.
func (h *Handler) Delete(c fiber.Ctx) error {
	p, err := h.Runtime.Store.OAuthProvider.Get(c.Context(), c.Params("id"))
	if err != nil {
		return response.Internal(c, err)
	}

	if p == nil {
		return response.NotFound(c, "oauth provider not found")
	}

	if err := h.Runtime.Store.OAuthProvider.Delete(c.Context(), p.ID); err != nil {
		return response.Internal(c, err)
	}

	h.Runtime.OAuth.Forget(p.Slug)

	return response.Success(c, DeletedResponse{Deleted: true})
}

// Test runs discovery against the stored configuration and reports
// what came back, so an operator can find a bad issuer or a typo'd
// client id before handing the login page to users.
func (h *Handler) Test(c fiber.Ctx) error {
	p, err := h.Runtime.Store.OAuthProvider.Get(c.Context(), c.Params("id"))
	if err != nil {
		return response.Internal(c, err)
	}

	if p == nil {
		return response.NotFound(c, "oauth provider not found")
	}

	res := h.Runtime.OAuth.Test(c.Context(), p)

	return response.Success(c, TestResponse{Test: res})
}

// apply folds validated input onto the record.
//
// Two return values on purpose, matching verifySession in the server
// package: the response.* helpers write the status and return nil, so
// a single error return would make a rejected request indistinguishable
// from an accepted one and the caller would save it anyway.
func (h *Handler) apply(c fiber.Ctx, p *opmodel.Provider, in upsertInput) (bool, error) {
	p.Name = in.Name

	p.Type = strings.ToLower(in.Type)
	if p.Type == "" {
		p.Type = opmodel.TypeOIDC
	}

	if _, ok := opmodel.ValidTypes[p.Type]; !ok {
		return false, response.BadRequest(c, "unknown provider type "+p.Type)
	}

	slug := opmodel.NormalizeSlug(in.Slug)
	if slug == "" {
		slug = opmodel.NormalizeSlug(in.Name)
	}
	if slug == "" {
		return false, response.BadRequest(c, "slug is empty after normalization, use letters or digits")
	}

	taken, err := h.Runtime.Store.OAuthProvider.SlugTaken(c.Context(), slug, p.ID)
	if err != nil {
		return false, response.Internal(c, err)
	}

	if taken {
		return false, response.BadRequest(c, "another provider already uses the slug "+slug)
	}

	p.Slug = slug

	p.ClientID = in.ClientID
	if in.Secret != nil {
		p.ClientSecret = strings.TrimSpace(*in.Secret)
	}

	// Stored EXACTLY as typed, trailing slash included. The issuer is
	// not a base URL to normalize - it is an identifier the discovery
	// document must match STRING for STRING, and providers disagree
	// about the slash: JumpCloud's canonical issuer is
	// "https://oauth.id.jumpcloud.com/" and stripping it here made
	// that provider impossible to configure at all - the operator
	// typed the slash, this line deleted it, and discovery then
	// refused the mismatch it had just created.
	p.Issuer = in.Issuer
	p.AuthURL = in.AuthURL
	p.TokenURL = in.TokenURL
	p.UserInfoURL = in.UserInfoURL

	if in.Enabled != nil {
		p.Enabled = *in.Enabled
	}

	if in.Hidden != nil {
		p.Hidden = *in.Hidden
	}

	if in.AutoRegister != nil {
		p.AutoRegister = *in.AutoRegister
	}

	if in.RequireEmailVerified != nil {
		p.RequireEmailVerified = *in.RequireEmailVerified
	}

	// absent leaves the list alone, empty clears it, and the difference
	// is load-bearing on a PATCH.
	//
	// Assign these unconditionally and a partial body wipes every
	// admission control the provider has. This is a PATCH, so a
	// one-field body is the natural call from a script or an SDK, and
	// `{"name": "Okta"}` sent to fix a label would empty
	// allowed_domains. With auto_register still true, every account at
	// the IdP could then sign in and mint a local user.
	//
	// JSON tells the two apart: an absent array decodes to nil, an
	// explicit [] to an empty non-nil slice. The console always sends all
	// four, since splitList of an empty field yields [], so it can still
	// clear them.
	//
	// Scopes gets the same treatment for the same reason - losing them
	// breaks sign-in at the IdP rather than at our end.
	if in.Scopes != nil {
		p.Scopes = cleanList(in.Scopes, false)
	}

	// Allowlists are lowercased at write time so the admission check
	// can compare directly rather than lowering on every sign-in.
	if in.AllowedDomains != nil {
		p.AllowedDomains = cleanList(in.AllowedDomains, true)
	}

	if in.AllowedEmails != nil {
		p.AllowedEmails = cleanList(in.AllowedEmails, true)
	}

	if in.AllowedGroups != nil {
		p.AllowedGroups = cleanList(in.AllowedGroups, true)
	}

	p.GroupsClaim = in.GroupsClaim

	// An oidc provider with neither an issuer nor explicit endpoints
	// can never complete a sign-in. Reject at write time rather than
	// storing something that only fails when a user clicks it.
	if p.Type == opmodel.TypeOIDC && p.Issuer == "" && (p.AuthURL == "" || p.TokenURL == "") {
		return false, response.BadRequest(c,
			"set an issuer for discovery, or both auth_url and token_url")
	}

	if msg := checkIssuerHost(p.Issuer, h.Runtime.Config.Server.PublicURL); msg != "" {
		return false, response.BadRequest(c, msg)
	}

	return true, nil
}

// checkIssuerHost catches the recurring mistake of pointing the
// issuer at this application instead of at the IdP. Discovery would
// then fetch /.well-known/openid-configuration from ourselves, 404,
// and surface as an opaque dial error at the first sign-in attempt
// rather than here where the operator can see the offending field.
func checkIssuerHost(issuer, publicURL string) string {
	if issuer == "" || publicURL == "" {
		return ""
	}

	iu, err := url.Parse(issuer)
	if err != nil || iu.Host == "" {
		return ""
	}

	pu, err := url.Parse(publicURL)
	if err != nil || pu.Host == "" {
		return ""
	}

	if strings.EqualFold(iu.Host, pu.Host) {
		return "the issuer points at this installation (" + iu.Host +
			"). It must be the identity provider's own URL, not Mailyard's."
	}

	return ""
}

// cleanList trims, optionally lowercases, and drops empties so the
// stored JSON array has no blank members.
func cleanList(in []string, lower bool) []string {
	out := make([]string, 0, len(in))
	for _, s := range in {
		s = strings.TrimSpace(s)
		if lower {
			s = strings.ToLower(s)
		}

		if s != "" {
			out = append(out, s)
		}
	}

	return out
}

// callbackURL is the redirect URI to register at the IdP. Built from
// server.public_url because the IdP has to reach us by our external
// name, which the request host may not be behind a proxy.
func callbackURL(rt *env.Runtime, slug string) string {
	base := ""
	if rt != nil {
		base = strings.TrimRight(rt.Config.Server.PublicURL, "/")
	}

	return base + oidc.CallbackPath(slug)
}
