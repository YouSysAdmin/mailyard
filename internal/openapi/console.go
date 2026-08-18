// Mailyard, Copyright (c) 2021-2026 YouSysAdmin

package openapi

import (
	"fmt"

	"gopkg.in/yaml.v3"

	"github.com/yousysadmin/mailyard/internal/domain/analytics"
	"github.com/yousysadmin/mailyard/internal/domain/apikey"
	"github.com/yousysadmin/mailyard/internal/domain/audit"
	"github.com/yousysadmin/mailyard/internal/domain/auth"
	"github.com/yousysadmin/mailyard/internal/domain/bounce"
	"github.com/yousysadmin/mailyard/internal/domain/campaign"
	"github.com/yousysadmin/mailyard/internal/domain/contact"
	"github.com/yousysadmin/mailyard/internal/domain/data"
	"github.com/yousysadmin/mailyard/internal/domain/domains"
	"github.com/yousysadmin/mailyard/internal/domain/email"
	"github.com/yousysadmin/mailyard/internal/domain/eventstream"
	"github.com/yousysadmin/mailyard/internal/domain/health"
	"github.com/yousysadmin/mailyard/internal/domain/inbound"
	"github.com/yousysadmin/mailyard/internal/domain/language"
	"github.com/yousysadmin/mailyard/internal/domain/notification"
	"github.com/yousysadmin/mailyard/internal/domain/oauthprovider"
	"github.com/yousysadmin/mailyard/internal/domain/plan"
	"github.com/yousysadmin/mailyard/internal/domain/project"
	"github.com/yousysadmin/mailyard/internal/domain/relaynode"
	"github.com/yousysadmin/mailyard/internal/domain/sandbox"
	"github.com/yousysadmin/mailyard/internal/domain/sender"
	"github.com/yousysadmin/mailyard/internal/domain/setting"
	"github.com/yousysadmin/mailyard/internal/domain/smtpcredential"
	"github.com/yousysadmin/mailyard/internal/domain/smtpserver"
	"github.com/yousysadmin/mailyard/internal/domain/stylesheet"
	"github.com/yousysadmin/mailyard/internal/domain/subscriber"
	"github.com/yousysadmin/mailyard/internal/domain/subscriberlist"
	"github.com/yousysadmin/mailyard/internal/domain/suppression"
	"github.com/yousysadmin/mailyard/internal/domain/template"
	"github.com/yousysadmin/mailyard/internal/domain/unsubscribelist"
	"github.com/yousysadmin/mailyard/internal/domain/user"
	"github.com/yousysadmin/mailyard/internal/domain/webhook"

	"github.com/yousysadmin/mailyard/internal/core/apidoc"
	"github.com/yousysadmin/mailyard/pkg"
)

// The description is assembled rather than written out, because one of
// the two groups it introduces exists only in the enterprise build -
// and a document that describes a prefix this binary does not serve
// sends a reader looking for routes that are not there.
var consoleDescription = `What is NOT on the product API, and could not be.

Under ` + "`/app/api`" + `: the browser ceremonies. Signing in, passkeys, the
second factor, the OIDC round-trip, session management, the caller's
own security log. An API key is not accepted here and would have
nothing to do with any of it - these describe how a PERSON proves who
they are, and they move whenever the sign-in page does.
` + enrolDescription + `
Everything else an installation can do is on the product API
(` + "`/api/v1`" + `), which the console also calls with its session. Describe
that one unless you specifically need what is above.`

// ConsoleRoutes is every documented endpoint the console talks to,
// with the FULL path including its prefix.
//
// Full paths because the console now spans two of them. The browser
// ceremonies - signing in, passkeys, the event stream - moved to
// /app/api, beside the console they belong to, while the product
// surface is on its way to /api/v1. A document that named one base for
// both would be describing routes that are not there, which is the
// exact failure this generator exists to make impossible.
//
// Assembled from each domain's generated ConsoleDocs, which is what
// TestEveryConsoleRouteIsDocumented compares against routes.go.
func ConsoleRoutes() []apidoc.Route { return consoleDocs() }

// allConsoleDocs is every generated entry, still carrying the path
// relative to its mount. surfaces.go decides where each one lives.
func allConsoleDocs() []apidoc.Route {
	var routes []apidoc.Route
	add := func(sets ...[]apidoc.Route) {
		for _, set := range sets {
			routes = append(routes, set...)
		}
	}

	add(
		analytics.ConsoleDocs(),
		apikey.ConsoleDocs(),
		audit.ConsoleDocs(),
		auth.ConsoleDocs(),
		bounce.ConsoleDocs(),
		campaign.ConsoleDocs(),
		contact.ConsoleDocs(),
		domains.ConsoleDocs(),
		email.ConsoleDocs(),
		eventstream.ConsoleDocs(),
		data.ConsoleDocs(),
		health.ConsoleDocs(),
		inbound.ConsoleDocs(),
		language.ConsoleDocs(),
		notification.ConsoleDocs(),
		oauthprovider.ConsoleDocs(),
		plan.ConsoleDocs(),
		project.ConsoleDocs(),
		relaynode.ConsoleDocs(),
		sandbox.ConsoleDocs(),
		sender.ConsoleDocs(),
		setting.ConsoleDocs(),
		smtpcredential.ConsoleDocs(),
		smtpserver.ConsoleDocs(),
		stylesheet.ConsoleDocs(),
		subscriber.ConsoleDocs(),
		subscriberlist.ConsoleDocs(),
		suppression.ConsoleDocs(),
		template.ConsoleDocs(),
		unsubscribelist.ConsoleDocs(),
		user.ConsoleDocs(),
		webhook.ConsoleDocs(),
	)

	return routes
}

// ConsoleSpec builds the console document as YAML.
//
// Served from the origin root rather than a base path, because the
// routes span two prefixes and carry their own. Point it at the host
// and every path in it is complete.
func ConsoleSpec() ([]byte, error) {
	doc, err := apidoc.Build(apidoc.Info{
		Title:       "Mailyard console and enrolment API",
		Description: consoleDescription,
		Version:     pkg.Version,
		ServerURL:   "/",
	}, ConsoleRoutes())
	if err != nil {
		return nil, fmt.Errorf("building the console openapi document: %w", err)
	}

	return yaml.Marshal(doc)
}
