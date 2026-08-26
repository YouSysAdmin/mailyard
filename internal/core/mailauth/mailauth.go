// Mailyard, Copyright (c) 2021-2026 YouSysAdmin

// Package mailauth authenticates received mail: SPF on the connecting
// IP, DKIM on the signatures, and DMARC on top of both.
//
// The MX listener takes connections from anyone on the internet, so
// without this any host could announce MAIL FROM as an address at a
// domain it has nothing to do with, and the message would be stored
// under that project, shown in the console and pushed through an
// inbound.received webhook looking entirely legitimate. Authenticating
// on receipt is what makes the sender of a stored message mean anything.
//
// We record the result rather than enforce it. A receiver that silently
// drops mail does more damage than one that files it with a verdict
// attached, and DMARC exists so the domain owner decides how strict to
// be. We honour their p= only when the operator opts in - see
// inbound.reject_on_dmarc_fail.
package mailauth

import (
	"bytes"
	"context"
	"fmt"
	"net"
	"strings"

	"blitiri.com.ar/go/spf"
	"github.com/emersion/go-msgauth/dmarc"

	"github.com/yousysadmin/mailyard/internal/core/dkim"
)

// Verdict values. These are the RFC 8601 result keywords, so they can
// go straight into an Authentication-Results header.
const (
	ResultPass      = "pass"
	ResultFail      = "fail"
	ResultSoftFail  = "softfail"
	ResultNeutral   = "neutral"
	ResultNone      = "none"
	ResultTempError = "temperror"
	ResultPermError = "permerror"
)

// Result is the full authentication outcome for one message.
type Result struct {
	SPF       string        `json:"spf"`
	SPFDomain string        `json:"spf_domain,omitempty"`
	DKIM      string        `json:"dkim"`
	DKIMSigs  []dkim.Result `json:"dkim_signatures,omitempty"`
	DMARC     string        `json:"dmarc"`

	// DMARCPolicy is the domain owner's published p= value: "none",
	// "quarantine" or "reject". Empty when no record exists.
	DMARCPolicy string `json:"dmarc_policy,omitempty"`

	// Aligned reports whether anything the From domain vouches for
	// actually passed, which is the question DMARC answers and the
	// only one worth acting on.
	Aligned bool `json:"aligned"`
}

// Rejectable reports whether the domain owner asked for this message
// to be refused. Only true for an explicit p=reject with no aligned
// pass - quarantine is a filing instruction, not a refusal, and this
// platform has no spam folder to file into.
func (r Result) Rejectable() bool {
	return r.DMARCPolicy == "reject" && !r.Aligned
}

// Config carries the resolvers, injected so the whole package is
// testable without DNS. Nil fields fall back to the system resolver.
type Config struct {
	LookupTXT func(domain string) ([]string, error)
}

// Verify authenticates one received message.
//
// clientIP is the connecting peer, envelopeFrom the MAIL FROM address,
// heloName what the client announced, and raw the full message. It
// never returns an error: every leg degrades to a result keyword,
// because a DNS hiccup must not stop us accepting mail.
func Verify(ctx context.Context, cfg Config, clientIP, envelopeFrom, heloName string, raw []byte) Result {
	res := Result{SPF: ResultNone, DKIM: ResultNone, DMARC: ResultNone}

	res.SPF, res.SPFDomain = checkSPF(ctx, clientIP, envelopeFrom, heloName)

	sigs, err := dkim.Verify(bytes.NewReader(raw), cfg.LookupTXT)
	switch {
	case err != nil:
		res.DKIM = ResultPermError
	case len(sigs) == 0:
		res.DKIM = ResultNone
	default:
		res.DKIMSigs = sigs
		res.DKIM = ResultFail
		for _, s := range sigs {
			if s.Valid {
				res.DKIM = ResultPass
				break
			}
		}
	}

	res.DMARC, res.DMARCPolicy, res.Aligned = checkDMARC(ctx, cfg, raw, res)

	return res
}

// checkSPF evaluates the sending IP against the envelope sender's
// policy, falling back to the HELO name for a null sender (bounces
// arrive with MAIL FROM:<>, which has no domain to check).
func checkSPF(ctx context.Context, clientIP, envelopeFrom, heloName string) (result, domain string) {
	ip := net.ParseIP(clientIP)
	if ip == nil {
		return ResultNone, ""
	}

	sender := envelopeFrom
	if sender == "" {
		if heloName == "" {
			return ResultNone, ""
		}

		sender = "postmaster@" + heloName
	}

	domain = domainOf(sender)
	if domain == "" {
		return ResultNone, ""
	}

	r, _ := spf.CheckHostWithSender(ip, heloName, sender, spf.WithContext(ctx))
	switch r {
	case spf.Pass:
		return ResultPass, domain
	case spf.Fail:
		return ResultFail, domain
	case spf.SoftFail:
		return ResultSoftFail, domain
	case spf.Neutral:
		return ResultNeutral, domain
	case spf.TempError:
		return ResultTempError, domain
	case spf.PermError:
		return ResultPermError, domain
	default: // spf.None
		return ResultNone, domain
	}
}

// checkDMARC looks up the From domain's policy and decides alignment.
//
// Alignment, not merely "did something pass", is the whole point: SPF
// passing for bounce.mailyard-service.example says nothing about a
// message claiming to be From your bank. DMARC asks whether the domain
// in the From header - the one a human reads - is the domain that
// vouched for the message.
func checkDMARC(ctx context.Context, cfg Config, raw []byte, res Result) (result, policy string, aligned bool) {
	fromDomain := domainOf(headerAddress(raw, "From"))
	if fromDomain == "" {
		return ResultPermError, "", false
	}

	rec, err := lookupDMARC(ctx, cfg, fromDomain)
	if err != nil || rec == nil {
		// No policy published. Nothing to align against and nothing the
		// owner has asked for, so this is "none", not a failure.
		return ResultNone, "", false
	}

	policy = string(rec.Policy)

	// Relaxed alignment (the default) admits a subdomain, strict
	// demands an exact match.
	spfAligned := res.SPF == ResultPass &&
		alignedWith(res.SPFDomain, fromDomain, rec.SPFAlignment == dmarc.AlignmentStrict)
	dkimAligned := false
	for _, s := range res.DKIMSigs {
		if s.Valid && alignedWith(s.Domain, fromDomain, rec.DKIMAlignment == dmarc.AlignmentStrict) {
			dkimAligned = true
			break
		}
	}

	if spfAligned || dkimAligned {
		return ResultPass, policy, true
	}

	return ResultFail, policy, false
}

func lookupDMARC(ctx context.Context, cfg Config, domain string) (*dmarc.Record, error) {
	if cfg.LookupTXT == nil {
		return dmarc.LookupWithOptions(domain, &dmarc.LookupOptions{})
	}

	return dmarc.LookupWithOptions(domain, &dmarc.LookupOptions{
		LookupTXT: func(name string) ([]string, error) {
			_ = ctx

			return cfg.LookupTXT(name)
		},
	})
}

// alignedWith reports whether authDomain vouches for fromDomain.
func alignedWith(authDomain, fromDomain string, strict bool) bool {
	a := strings.ToLower(strings.TrimSuffix(authDomain, "."))
	f := strings.ToLower(strings.TrimSuffix(fromDomain, "."))
	if a == "" || f == "" {
		return false
	}

	if a == f {
		return true
	}

	if strict {
		return false
	}

	// Relaxed: an organizational-domain match. Comparing suffixes is an
	// approximation of the public-suffix walk the RFC describes, and it
	// is deliberately the conservative direction - it can refuse an
	// alignment a full PSL check would allow, never the reverse.
	return strings.HasSuffix(a, "."+f) || strings.HasSuffix(f, "."+a)
}

// headerAddress pulls one address-bearing header out of raw. Only the
// header block is scanned, and only the first occurrence is used: a
// message with two From headers is malformed, and picking a later one
// is how header-injection tricks get through.
func headerAddress(raw []byte, name string) string {
	prefix := strings.ToLower(name) + ":"
	headers, _, _ := bytes.Cut(raw, []byte("\r\n\r\n"))
	var value string
	var found bool
	for line := range strings.SplitSeq(string(headers), "\r\n") {
		if found {
			// Folded continuation lines start with whitespace.
			if strings.HasPrefix(line, " ") || strings.HasPrefix(line, "\t") {
				value += " " + strings.TrimSpace(line)
				continue
			}

			break
		}

		if strings.HasPrefix(strings.ToLower(line), prefix) {
			value = strings.TrimSpace(line[len(prefix):])
			found = true
		}
	}

	return value
}

// domainOf is the lowercase host part of an address, tolerating a
// display name around it.
func domainOf(addr string) string {
	addr = strings.TrimSpace(addr)
	if open := strings.LastIndex(addr, "<"); open >= 0 {
		if cls := strings.Index(addr[open:], ">"); cls > 0 {
			addr = addr[open+1 : open+cls]
		}
	}

	_, host, ok := strings.CutLast(addr, "@")
	if !ok || host == "" {
		return ""
	}

	return strings.ToLower(strings.Trim(host, " \t>"))
}

// AuthenticationResults renders the RFC 8601 header recording what we
// found, so anything downstream (a rule engine, a human reading the
// stored message) sees the same verdict we did.
func AuthenticationResults(hostname string, r Result) string {
	if hostname == "" {
		hostname = "mailyard"
	}

	parts := []string{hostname}
	if r.SPFDomain != "" {
		parts = append(parts, fmt.Sprintf("spf=%s smtp.mailfrom=%s", r.SPF, r.SPFDomain))
	} else {
		parts = append(parts, "spf="+r.SPF)
	}

	if len(r.DKIMSigs) > 0 {
		parts = append(parts, fmt.Sprintf("dkim=%s header.d=%s", r.DKIM, r.DKIMSigs[0].Domain))
	} else {
		parts = append(parts, "dkim="+r.DKIM)
	}

	parts = append(parts, "dmarc="+r.DMARC)

	return strings.Join(parts, "; ")
}
