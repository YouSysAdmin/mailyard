// Mailyard, Copyright (c) 2021-2026 YouSysAdmin

package project

import (
	"github.com/yousysadmin/mailyard/internal/core/env"
	"github.com/yousysadmin/mailyard/internal/domain"
	settingmodel "github.com/yousysadmin/mailyard/internal/models/setting"
)

// MayCreate answers whether this account may create a project.
//
// One function because three places ask and they must not disagree:
// the middleware that refuses the request, the list endpoint that
// tells the console whether to offer the button, and the console
// itself, which renders from that answer rather than reaching for the
// setting. A console that decided this for itself would be a second
// copy of the rule, and the failure it produces is a button that is
// offered and then answers 403.
//
// A platform admin is never subject to the setting. Somebody has to
// be able to make the first project on an installation where nobody
// else may, and that somebody is the account bootstrap created.
//
// Auth disabled is every caller, the same exemption requireAdmin
// makes: an installation running with authentication switched off has
// no accounts to distinguish.
//
// A gate that cannot answer refuses. rt.Config is always set on a
// running node, so reaching either of these means something is half
// built - and the safe answer for the thing that mints tenants is no.
func MayCreate(rt *env.Runtime, rc *domain.RequestContext) bool {
	if rt == nil || rt.Config == nil {
		return false
	}

	if rt.Config.Auth.Disabled {
		return true
	}

	// The RequestContext rather than the user, because that is what
	// both callers hold - and because Create refuses a caller with no
	// USER anyway, an admin API key included, so there is no
	// IsPlatformAdmin branch to make here.
	if rc == nil || rc.User == nil {
		return false
	}

	if rc.User.IsAdmin() {
		return true
	}

	return rt.Settings.Bool(settingmodel.KeyUserProjectCreation)
}
