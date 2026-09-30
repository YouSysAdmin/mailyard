// Mailyard, Copyright (c) 2021-2026 YouSysAdmin

package campaign

import (
	"strings"
	"testing"

	"github.com/yousysadmin/mailyard/internal/core/tracking"
	"github.com/yousysadmin/mailyard/internal/domain/email"
	cmodel "github.com/yousysadmin/mailyard/internal/models/campaign"
)

// A campaign that turned its unsubscribe off ships neither List-Unsubscribe
// header and no unsubscribe link, while the web view and the open pixel
// stay - the option removes the opt-out, not the tracking.
func TestAnUnsubscribeDisabledCampaignCarriesNoOptOut(t *testing.T) {
	signer := tracking.NewSigner("https://mail.example.test", "test-secret-test-secret-test-secret")
	vars := tracking.WithSystemVars(nil)
	body := `<p><a href="` + vars[tracking.VarUnsubscribe].(string) + `">stop</a> ` +
		`<a href="` + vars[tracking.VarWebView].(string) + `">view</a></p>`

	for _, disabled := range []bool{false, true} {
		r := &Runner{Tracking: signer}
		c := &cmodel.Campaign{ID: "c", ProjectID: "p", UnsubscribeDisabled: disabled}
		m := &cmodel.Message{ID: "m"}
		req := &email.SendRequest{HTML: body, Text: body}
		r.applyTracking(t.Context(), c, m, req)

		if tracking.HasSystemSentinels(req.HTML) || tracking.HasSystemSentinels(req.Text) {
			t.Errorf("disabled=%v: a sentinel shipped:\n%s", disabled, req.HTML)
		}

		hasUnsub := strings.Contains(req.HTML, "/tracking/unsubscribe/")
		if hasUnsub == disabled {
			t.Errorf("disabled=%v: unsubscribe link present=%v", disabled, hasUnsub)
		}

		if !strings.Contains(req.HTML, "/tracking/view/") {
			t.Errorf("disabled=%v: the web view link is gone", disabled)
		}

		if (req.ListUnsubscribeURL != "") == disabled || req.ListUnsubscribePost == disabled {
			t.Errorf("disabled=%v: headers url=%q post=%v", disabled, req.ListUnsubscribeURL, req.ListUnsubscribePost)
		}
	}
}
