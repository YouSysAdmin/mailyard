// Mailyard, Copyright (c) 2021-2026 YouSysAdmin

package domains

import (
	"context"
	"strings"

	"github.com/yousysadmin/mailyard/internal/core/dkim"
	dmodel "github.com/yousysadmin/mailyard/internal/models/domain"
)

// Record is one DNS entry the operator has to publish, plus whether
// we can currently see it.
type Record struct {
	// Kind is "ownership", "spf", "dkim" or "dmarc".
	Kind  string `json:"kind"`
	Type  string `json:"type"`
	Host  string `json:"host"`
	Value string `json:"value"`

	// Required marks the records without which the domain does not
	// work at all, as opposed to the ones that only improve how
	// receivers treat it.
	Required bool `json:"required"`
	Verified bool `json:"verified"`

	// Detail explains a failure, or what the record buys when it is
	// missing. Shown in the console next to the record.
	Detail string `json:"detail,omitempty"`
}

// CheckResult is the outcome of one verification pass.
type CheckResult struct {
	Ownership bool
	SPF       bool
	DKIM      bool
	DMARC     bool
}

// CheckAll runs every DNS check for a domain.
//
// The four are independent and none of them gates the others. In
// particular ownership is what governs whether the domain works at
// all - sending and inbound routing both key off it - while SPF,
// DKIM and DMARC only govern how receivers treat the mail. Reporting
// them separately is the point: an operator with a verified domain and
// no DMARC record should see exactly that, not a single opaque
// "unverified".
func CheckAll(ctx context.Context, lookup LookupTXT, d *dmodel.Domain) CheckResult {
	return CheckResult{
		Ownership: CheckOwnership(ctx, lookup, d),
		SPF:       CheckSPF(ctx, lookup, d.Domain),
		DKIM:      CheckDKIM(ctx, lookup, d),
		DMARC:     CheckDMARC(ctx, lookup, d.Domain),
	}
}

// CheckSPF reports whether the apex publishes an SPF record.
//
// Presence only. Whether the policy actually authorizes this
// installation cannot be decided here: the sending IP belongs to
// whichever SMTP server the project configured, which may be a
// third-party relay with its own include, and resolving the full
// mechanism set to answer "would this pass" needs the sending IP,
// which is not known until a message is on the wire. Claiming more
// than presence would be a check that lies.
func CheckSPF(ctx context.Context, lookup LookupTXT, name string) bool {
	records, err := lookup(ctx, name)
	if err != nil {
		return false
	}

	for _, txt := range records {
		if strings.HasPrefix(strings.ToLower(strings.TrimSpace(txt)), "v=spf1") {
			return true
		}
	}

	return false
}

// CheckDKIM reports whether the published key at the domain's
// selector matches the public key we hold.
//
// The key is compared, not merely detected. A record left over from a
// previous provider, or from a key this installation has since
// regenerated, parses perfectly and verifies nothing - reporting that
// as success would be worse than reporting nothing, because the
// operator would stop looking.
func CheckDKIM(ctx context.Context, lookup LookupTXT, d *dmodel.Domain) bool {
	if d.DKIMPublicKey == "" || d.DKIMSelector == "" {
		return false
	}

	records, err := lookup(ctx, dkim.TXTHost(d.DKIMSelector, d.Domain))
	if err != nil {
		return false
	}

	for _, txt := range records {
		if published := dkimPublicKeyOf(txt); published != "" && published == d.DKIMPublicKey {
			return true
		}
	}

	return false
}

// dkimPublicKeyOf pulls the p= tag out of a DKIM record.
//
// Long TXT records arrive split into 255-byte strings that the
// resolver rejoins, and operators paste them with assorted whitespace,
// so the value is stripped of all spacing before comparison - base64
// has none of its own.
func dkimPublicKeyOf(txt string) string {
	for part := range strings.SplitSeq(txt, ";") {
		tag, value, found := strings.Cut(strings.TrimSpace(part), "=")
		if !found || strings.TrimSpace(tag) != "p" {
			continue
		}

		return strings.Map(func(r rune) rune {
			if r == ' ' || r == '\t' || r == '\r' || r == '\n' {
				return -1
			}

			return r
		}, value)
	}

	return ""
}

// CheckDMARC reports whether _dmarc.<domain> publishes a policy.
func CheckDMARC(ctx context.Context, lookup LookupTXT, name string) bool {
	records, err := lookup(ctx, "_dmarc."+name)
	if err != nil {
		return false
	}

	for _, txt := range records {
		if strings.HasPrefix(strings.ToLower(strings.TrimSpace(txt)), "v=dmarc1") {
			return true
		}
	}

	return false
}

// Records lists every DNS entry for a domain with its current state,
// which is what the console renders and what the API returns.
//
// spfInclude comes from sending.spf_include and is the host this
// installation asks receivers to authorize. Empty means the operator
// has not set one, so the SPF row carries no suggested value rather
// than a placeholder somebody would publish verbatim.
//
// Nothing here names a config key. These strings are read by a
// project member in the console, and sending.spf_include is a
// platform setting with no per-domain equivalent - telling a tenant to
// change it is advice they cannot act on, about a control they cannot
// see.
func Records(d *dmodel.Domain, spfInclude string) []Record {
	out := []Record{{
		Kind:     "ownership",
		Type:     "TXT",
		Host:     d.Domain,
		Value:    d.TXTRecordValue(),
		Required: true,
		Verified: d.Verified,
		Detail:   "Proves the domain is yours. Sending and inbound routing both depend on it.",
	}}

	spf := Record{
		Kind:     "spf",
		Type:     "TXT",
		Host:     d.Domain,
		Required: false,
		Verified: d.SPFVerified,
	}
	const spfChecked = " Only presence is checked here, because whether a policy passes depends on the IP a message was sent from."
	if spfInclude == "" {
		spf.Detail = "Authorizes the hosts allowed to send for this domain. The value depends on which SMTP server actually delivers your mail, so take it from that provider - or ask an administrator if this project sends through the platform's own servers." + spfChecked
	} else {
		spf.Value = "v=spf1 include:" + spfInclude + " ~all"
		spf.Detail = "Authorizes this platform to send for the domain. If you deliver through your own SMTP server instead, include that provider's host as well." + spfChecked
	}

	out = append(out, spf)

	dkimRec := Record{
		Kind:     "dkim",
		Type:     "TXT",
		Required: false,
		Verified: d.DKIMVerified,
	}
	if d.DKIMPublicKey == "" {
		dkimRec.Host = dkim.TXTHost(dkim.DefaultSelector, d.Domain)
		dkimRec.Detail = "No signing key yet. Verify ownership and a key is generated automatically."
	} else {
		dkimRec.Host = dkim.TXTHost(d.DKIMSelector, d.Domain)
		dkimRec.Value = dkim.TXTValue(d.DKIMPublicKey)
		dkimRec.Detail = "Lets receivers check the signature on your mail. Signing starts as soon as ownership is verified, so publishing this late costs nothing."
	}

	out = append(out, dkimRec)

	out = append(out, Record{
		Kind:     "dmarc",
		Type:     "TXT",
		Host:     "_dmarc." + d.Domain,
		Value:    "v=DMARC1; p=none; rua=mailto:dmarc@" + d.Domain,
		Required: false,
		Verified: d.DMARCVerified,
		Detail:   "Tells receivers what to do when SPF and DKIM both fail. Start at p=none and tighten once the reports look clean.",
	})

	return out
}
