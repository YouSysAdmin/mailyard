// Mailyard, Copyright (c) 2021-2026 YouSysAdmin

package tests

import (
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"

	"github.com/yousysadmin/mailyard/internal/core/alertmail"
	coreaudit "github.com/yousysadmin/mailyard/internal/core/audit"
	amodel "github.com/yousysadmin/mailyard/internal/models/audit"
)

// Every event alertmail says it mails has to be an event something can
// actually produce.
//
// The failure this prevents is the quietest kind there is. The alert list
// is keyed by strings that audit.RouteType DERIVES from the router, so a
// plausible-looking key - "apikeys.created", "project.member.added",
// "admin.api-keys" - matches nothing, sends nothing, and reads in review
// as a feature that is present. Nobody notices until the day they needed
// the mail.
//
// Two sources, checked separately because they are produced differently:
//
//   - project and platform tiers come from the ROUTER. RouteType is run
//     over every mutating route registered in routes.go, and the result
//     is the set of types that can exist.
//   - the account tier is recorded EXPLICITLY by a handler, so those keys
//     are constants from internal/models/audit and the compiler has
//     already checked them - what this adds is that the constant is
//     actually USED by a recorder somewhere. It was not, twice over:
//     auth.2fa.enabled and auth.2fa.disabled existed as constants and
//     nothing wrote either, so turning off a second factor left no trace
//     in the security log at all.
func TestEveryAlertNamesARealEvent(t *testing.T) {
	produced := routeEventTypes(t)
	recorded := securityEventTypes(t)

	for typ, tier := range alertmail.Types() {
		if tier == alertmail.TierAccount {
			if !recorded[typ] {
				t.Errorf("alertmail mails %q, but no handler under internal/domain records it - "+
					"the alert would never be sent", typ)
			}

			continue
		}

		// A project event is normally derived from a route, but one that
		// no request produces - the dispatcher disabling a webhook - is
		// recorded explicitly like the security events, and counts.
		if !produced[typ] && !recorded[typ] {
			t.Errorf("alertmail mails %q, but no route in routes.go produces that event type - "+
				"RouteType derives these from the router, so a plausible-looking string here "+
				"is an alert nobody ever gets", typ)
		}
	}
}

// routeEventTypes runs RouteType over every mutating route the router
// registers, which is exactly the set auditWrites can produce.
//
// It reuses routesUnder, the same routes.go parser the OpenAPI and
// permission guards read - one parser, so a route registered in a way
// this one cannot see fails those too rather than silently narrowing
// this check.
func routeEventTypes(t *testing.T) map[string]bool {
	t.Helper()
	const id = "b4f9bd4a-f184-4e93-a40c-14e7f7181242"
	param := regexp.MustCompile(`:[A-Za-z]+`)

	out := map[string]bool{}
	n := 0
	for r := range routesUnder(t, "", nil) {
		method, path, ok := strings.Cut(r, " ")
		if !ok {
			continue
		}

		switch method {
		case "POST", "PUT", "PATCH", "DELETE":
		default:
			continue
		}

		// A registered path carries :params. The audit trail sees them
		// RESOLVED, and RouteType decides what a segment is by whether it
		// looks like an identifier - so they have to be substituted here or
		// every /:id route reports the wrong type. Which is what the first
		// cut of this test did: it looked for {id}, matched nothing, and
		// blamed the alert list.
		out[coreaudit.RouteType(method, param.ReplaceAllString(path, id))] = true
		n++
	}

	if n < 100 {
		t.Fatalf("only found %d mutating routes - the routes.go parse is broken, and this "+
			"test would pass vacuously", n)
	}

	return out
}

// securityEventTypes collects the audit type constants a handler
// actually records.
//
// Every domain, not just auth: an administrator resetting somebody's
// second factor or passkeys is recorded by domain/user, and narrowing
// this to one directory reported those as unproduced. Which the first cut
// did.
func securityEventTypes(t *testing.T) map[string]bool {
	t.Helper()
	root := filepath.Join(repoRoot(t), "internal", "domain")
	var src strings.Builder
	err := filepath.WalkDir(root, func(path string, d os.DirEntry, err error) error {
		if err != nil {
			return err
		}

		if d.IsDir() || !strings.HasSuffix(path, ".go") || strings.HasSuffix(path, "_test.go") {
			return nil
		}

		b, err := os.ReadFile(path)
		if err != nil {
			return err
		}

		src.Write(b)

		return nil
	})
	if err != nil {
		t.Fatalf("walk %s: %v", root, err)
	}

	body := src.String()

	// The constant names, resolved to their values. Matching the values
	// in the source would find nothing: the handlers reference the
	// constants, which is the point of them existing.
	byValue := map[string]string{
		amodel.TypePasswordChanged:  "TypePasswordChanged",
		amodel.TypePasswordResetOK:  "TypePasswordResetOK",
		amodel.TypeTOTPEnabled:      "TypeTOTPEnabled",
		amodel.TypeTOTPDisabled:     "TypeTOTPDisabled",
		amodel.TypeTOTPReset:        "TypeTOTPReset",
		amodel.TypeTOTPRecoveryUsed: "TypeTOTPRecoveryUsed",
		amodel.TypePasskeyAdded:     "TypePasskeyAdded",
		amodel.TypePasskeyRemoved:   "TypePasskeyRemoved",
		amodel.TypePasskeyReset:     "TypePasskeyReset",
		amodel.TypeWebhookDisabled:  "TypeWebhookDisabled",
	}
	out := map[string]bool{}
	for value, name := range byValue {
		if strings.Contains(body, name) {
			out[value] = true
		}
	}

	return out
}
