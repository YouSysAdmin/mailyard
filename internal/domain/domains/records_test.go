// Mailyard, Copyright (c) 2021-2026 YouSysAdmin

package domains

import (
	"regexp"
	"strings"
	"testing"

	dmodel "github.com/yousysadmin/mailyard/internal/models/domain"
)

func sample() *dmodel.Domain {
	return &dmodel.Domain{
		ID: "504a0295-c50b-4e67-82c9-e916c01ecbd0", ProjectID: "e66e7a4d-9e6c-4884-869a-cf9ffcf22181", Domain: "example.com",
		VerificationToken: "tok", Verified: true,
		DKIMSelector: "mailyard", DKIMPublicKey: "MIIBIjAN",
	}
}

// Every string here is read by a project member in the console, and
// platform settings are not theirs to change. Telling them to "set
// sending.spf_include" is advice about a control they cannot see and
// have no per-domain equivalent for.
//
// Matched by shape rather than by a list of key names, so a config key
// added later is caught too.
func TestRecordDetailsNameNoConfigKey(t *testing.T) {
	// section.key_name, the shape every setting in config.go takes.
	configKey := regexp.MustCompile(`\b[a-z]+\.[a-z]+(_[a-z]+)+\b`)
	for _, spfInclude := range []string{"", "spf.mailyard.dev"} {
		for _, rec := range Records(sample(), spfInclude) {
			if hit := configKey.FindString(rec.Detail); hit != "" {
				t.Errorf("the %s detail names the config key %q, which a project member cannot set:\n  %s",
					rec.Kind, hit, rec.Detail)
			}
		}
	}
}

// The row stays either way: its Found / Not found badge is the useful
// part, and dropping it would hide whether a record exists at all.
// Only the suggested value goes away.
func TestSPFRowSurvivesWithoutAnInclude(t *testing.T) {
	d := sample()
	d.SPFVerified = true

	spf := recordOfKind(t, Records(d, ""), "spf")
	if spf.Value != "" {
		t.Errorf("with no include configured the SPF row suggests %q, which is a value somebody would publish", spf.Value)
	}

	if !spf.Verified {
		t.Error("the SPF row lost the state it had found")
	}

	if spf.Detail == "" {
		t.Error("the SPF row explains nothing")
	}

	withInclude := recordOfKind(t, Records(d, "spf.mailyard.dev"), "spf")
	if withInclude.Value != "v=spf1 include:spf.mailyard.dev ~all" {
		t.Errorf("suggested SPF value is %q", withInclude.Value)
	}
}

func recordOfKind(t *testing.T, records []Record, kind string) Record {
	t.Helper()
	for _, r := range records {
		if r.Kind == kind {
			return r
		}
	}

	t.Fatalf("no %q record among %d", kind, len(records))

	return Record{}
}

// A record with nothing to publish must carry no value, because the
// console renders Host and Value only when there is one - see
// DnsRecordList.
func TestARecordWithoutAValueIsEmptyNotPlaceholder(t *testing.T) {
	d := sample()
	d.DKIMPublicKey = ""

	for _, rec := range Records(d, "") {
		if rec.Value == "" {
			continue
		}

		for _, placeholder := range []string{"<", ">", "TODO", "xxx", "example-value"} {
			if strings.Contains(rec.Value, placeholder) {
				t.Errorf("the %s value looks like a placeholder: %q", rec.Kind, rec.Value)
			}
		}
	}

	if dkimRec := recordOfKind(t, Records(d, ""), "dkim"); dkimRec.Value != "" {
		t.Errorf("DKIM suggests %q before a key exists", dkimRec.Value)
	}
}
