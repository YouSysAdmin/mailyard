// Mailyard, Copyright (c) 2021-2026 YouSysAdmin

package oidc

import (
	"errors"
	"fmt"
	"slices"
	"strings"
)

// Admit checks the allowlist surface against the ID-token claims.
// Returns nil when the user is admitted, or a deny error suitable
// for showing to operators in audit logs (not to the end user, who
// should see a generic "access denied" so we don't leak which leg
// failed).
//
// Order of checks:
//  1. email_verified (if RequireEmailVerified)
//  2. allowed_emails (most specific)
//  3. allowed_domains
//  4. allowed_groups (operator-supplied claim name)
//
// All-empty allowlists pass automatically - the IdP is the gate.
func (p *Provider) Admit(c *Claims) error {
	cfg := p.cfg
	email := strings.ToLower(strings.TrimSpace(c.Email))

	if cfg.RequireEmailVerified && !c.EmailVerified {
		return errors.New("email_verified=false on id_token")
	}

	if len(cfg.AllowedEmails) > 0 {
		if email == "" {
			return errors.New("allowed_emails set but id_token has no email claim")
		}

		if !slices.Contains(cfg.AllowedEmails, email) {
			return fmt.Errorf("email %q not on allowed_emails list", email)
		}

		// explicit-email match short-circuits other checks (operator
		// listed this exact address, so admit regardless of domain/group).
		return nil
	}

	if len(cfg.AllowedDomains) > 0 {
		if email == "" {
			return errors.New("allowed_domains set but id_token has no email claim")
		}

		_, host, ok := strings.CutLast(email, "@")
		if !ok || host == "" {
			return fmt.Errorf("malformed email %q", email)
		}

		if !slices.Contains(cfg.AllowedDomains, host) {
			return fmt.Errorf("domain %q not on allowed_domains list", host)
		}
	}

	if len(cfg.AllowedGroups) > 0 {
		groups := extractGroups(c.Raw, cfg.GroupsClaim)
		if len(groups) == 0 {
			return fmt.Errorf("allowed_groups set but claim %q is empty/missing on id_token", cfg.GroupsClaim)
		}

		// Case-insensitive match: AllowedGroups are pre-lowered by
		// config.Validate() - lower the IdP-supplied claim values here
		// rather than mutating cfg or pre-walking. Most IdPs
		// (Cognito, Keycloak) ship uppercase role names, so this is
		// the path that actually fires in practice.
		matched := false
		for _, g := range groups {
			if slices.Contains(cfg.AllowedGroups, strings.ToLower(strings.TrimSpace(g))) {
				matched = true
				break
			}
		}

		if !matched {
			return fmt.Errorf("none of user's groups %v overlap allowed_groups", groups)
		}
	}

	return nil
}

// extractGroups reads the claim by name and coerces []string from the
// shapes IdPs commonly use. Tolerates []any, []string, single
// string. Returns nil when the claim is missing or unsupported shape.
func extractGroups(raw map[string]any, claim string) []string {
	if claim == "" {
		return nil
	}

	v, ok := raw[claim]
	if !ok || v == nil {
		return nil
	}

	switch t := v.(type) {
	case []string:
		return t
	case string:
		return []string{t}
	case []any:
		out := make([]string, 0, len(t))
		for _, item := range t {
			if s, ok := item.(string); ok {
				out = append(out, s)
			}
		}

		return out
	}

	return nil
}
