// Mailyard, Copyright (c) 2021-2026 YouSysAdmin

// Package tracking mints and verifies the signed public /t/ URLs
// (open pixel, click redirects, hosted unsubscribe, view in browser)
// and rewrites campaign HTML to use them. Everything is keyed on an
// HMAC secret so the predictable id-shaped paths cannot be forged to
// inflate metrics or unsubscribe strangers.
package tracking

import (
	"crypto/hmac"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"errors"
	"fmt"
	"regexp"
	"strconv"
	"strings"
	"time"
)

// WebViewTTL bounds how long a hosted "view in browser" link stays
// valid. A web-view link exposes full message content, so unlike
// unsubscribe tokens it carries an expiry.
const WebViewTTL = 90 * 24 * time.Hour

// Signer builds and verifies tracking URLs. Zero-value baseURL or key
// disables tracking (Enabled reports it) - campaigns then send
// without pixels, rewrites, or hosted links.
type Signer struct {
	baseURL string
	key     []byte
}

// NewSigner builds a Signer.
func NewSigner(baseURL, secret string) *Signer {
	s := &Signer{baseURL: strings.TrimRight(baseURL, "/")}
	if secret != "" {
		s.key = []byte(secret)
	}

	return s
}

// Enabled reports whether tracking URLs can be built at all: they are
// absolute, so a public base URL and a signing key are both required.
func (s *Signer) Enabled() bool { return s.baseURL != "" && len(s.key) > 0 }

// OpenURL is the signed open-pixel address for a campaign message.
func (s *Signer) OpenURL(messageID string) string {
	return fmt.Sprintf("%s/tracking/open/%s.gif?sig=%s", s.baseURL, messageID, s.sign("open:"+messageID))
}

// ClickURL is the signed redirect address for one tracked link in one
// message.
func (s *Signer) ClickURL(messageID, hash string) string {
	return fmt.Sprintf("%s/tracking/click/%s/%s?sig=%s", s.baseURL, messageID, hash, s.sign("click:"+messageID+":"+hash))
}

// UnsubscribeURL is the hosted unsubscribe page for a campaign
// message. No expiry: an unsubscribe link in a delivered email must
// keep working.
func (s *Signer) UnsubscribeURL(messageID string) string {
	return fmt.Sprintf("%s/tracking/unsubscribe/%s", s.baseURL, s.token("unsub:"+messageID))
}

// ListUnsubscribeURL is the hosted one-click unsubscribe page for a
// transactional send scoped to an opt-out list. The token binds the
// list AND the recipient, so a leaked link can only unsubscribe the
// address it was minted for. No expiry, for the same reason as
// UnsubscribeURL.
func (s *Signer) ListUnsubscribeURL(listID, email string) string {
	return fmt.Sprintf("%s/tracking/unsubscribe/%s", s.baseURL, s.token("lunsub:"+listID+":"+email))
}

// VerifyListUnsubscribeToken returns the list id and recipient the
// token encodes.
func (s *Signer) VerifyListUnsubscribeToken(tok string) (listID, email string, err error) {
	payload, err := s.open(tok)
	if err != nil {
		return "", "", err
	}

	rest, found := strings.CutPrefix(payload, "lunsub:")
	if !found {
		return "", "", errors.New("wrong token kind")
	}

	// The address may itself contain a colon in exotic cases, so split
	// on the first separator only.
	listID, email, found = strings.Cut(rest, ":")
	if !found || listID == "" || email == "" {
		return "", "", errors.New("malformed token")
	}

	return listID, email, nil
}

// WebViewURL is the hosted "view in browser" page for an email.
func (s *Signer) WebViewURL(emailID string) string {
	exp := time.Now().Add(WebViewTTL).Unix()

	return fmt.Sprintf("%s/tracking/view/%s", s.baseURL, s.token(fmt.Sprintf("view:%s:%d", emailID, exp)))
}

// VerifyOpen reports whether sig is this signer's signature over
// messageID. The pixel is unauthenticated, so this is the only thing
// standing between a stranger and a forged open.
func (s *Signer) VerifyOpen(messageID, sig string) bool {
	return hmac.Equal([]byte(sig), []byte(s.sign("open:"+messageID)))
}

// VerifyClick reports whether sig covers messageID and the link hash
// together - the hash is in the signature so a redirect cannot be
// repointed.
func (s *Signer) VerifyClick(messageID, hash, sig string) bool {
	return hmac.Equal([]byte(sig), []byte(s.sign("click:"+messageID+":"+hash)))
}

// VerifyUnsubscribeToken returns the campaign message id the token
// encodes.
func (s *Signer) VerifyUnsubscribeToken(tok string) (string, error) {
	payload, err := s.open(tok)
	if err != nil {
		return "", err
	}

	id, found := strings.CutPrefix(payload, "unsub:")
	if !found || id == "" {
		return "", errors.New("wrong token kind")
	}

	return id, nil
}

// VerifyWebViewToken returns the email id the token encodes, checking
// the embedded expiry.
func (s *Signer) VerifyWebViewToken(tok string) (string, error) {
	payload, err := s.open(tok)
	if err != nil {
		return "", err
	}

	rest, found := strings.CutPrefix(payload, "view:")
	if !found {
		return "", errors.New("wrong token kind")
	}

	payload, expiry, ok := strings.CutLast(rest, ":")
	if !ok || payload == "" {
		return "", errors.New("invalid token payload")
	}

	exp, err := strconv.ParseInt(expiry, 10, 64)
	if err != nil {
		return "", errors.New("invalid token expiry")
	}

	if time.Now().Unix() > exp {
		return "", errors.New("link expired")
	}

	return payload, nil
}

// sign returns a short URL-safe MAC over payload.
func (s *Signer) sign(payload string) string {
	mac := hmac.New(sha256.New, s.key)
	mac.Write([]byte(payload))

	return base64.RawURLEncoding.EncodeToString(mac.Sum(nil)[:16])
}

// token wraps a payload as base64(payload).sig - self-describing and
// verifiable without a database lookup.
func (s *Signer) token(payload string) string {
	return base64.RawURLEncoding.EncodeToString([]byte(payload)) + "." + s.sign(payload)
}

// open verifies a token and returns its payload.
func (s *Signer) open(tok string) (string, error) {
	head, sig, found := strings.Cut(tok, ".")
	if !found {
		return "", errors.New("invalid token format")
	}

	payload, err := base64.RawURLEncoding.DecodeString(head)
	if err != nil {
		return "", errors.New("invalid token encoding")
	}

	if !hmac.Equal([]byte(sig), []byte(s.sign(string(payload)))) {
		return "", errors.New("invalid token signature")
	}

	return string(payload), nil
}

// Link is one rewritten link discovered by ProcessHTML. The caller
// persists it so the click handler can resolve hash back to URL.
type Link struct {
	URL  string
	Hash string
}

// HashLink derives the stable link hash within a grouping.
//
// scope is the campaign id for campaign mail and the project id for
// everything else - the same URL in two different campaigns is two
// tallies, and the same URL across a project's transactional mail is
// one. Hashing the scope in is what keeps those apart.
func HashLink(scope, url string) string {
	sum := sha256.Sum256([]byte(scope + "|" + url))

	return hex.EncodeToString(sum[:8])
}

// linkRe matches http(s) hrefs case-insensitively.
var linkRe = regexp.MustCompile(`(?i)href\s*=\s*["'](https?://[^"']+)["']`)

// TrackOpts says what to rewrite and under which identity.
//
// EmailID is what the URLs carry - the one id campaign and
// transactional mail both have, so the handlers resolve one thing.
// LinkScope groups the click tallies (campaign id, or project id
// outside a campaign). Opens and Clicks are independent because they
// are independently useful: a pixel is the part some senders will not
// put in transactional mail, while a wrapped link is the part others
// object to.
type TrackOpts struct {
	EmailID   string
	LinkScope string
	Opens     bool
	Clicks    bool
}

// ProcessHTML rewrites external links to their signed click redirect
// and injects the open pixel before the closing body tag, according
// to opts. Links already pointing at this installation are left
// alone. Returns the rewritten HTML plus the discovered links.
func (s *Signer) ProcessHTML(html string, opts TrackOpts) (string, []Link) {
	if html == "" || !s.Enabled() || opts.EmailID == "" {
		return html, nil
	}

	if !opts.Opens && !opts.Clicks {
		return html, nil
	}

	trackingPrefix := s.baseURL + "/tracking/"
	seen := map[string]bool{}
	var links []Link

	if opts.Clicks {
		html = linkRe.ReplaceAllStringFunc(html, func(match string) string {
			sub := linkRe.FindStringSubmatch(match)
			if len(sub) < 2 {
				return match
			}

			original := sub[1]
			if strings.HasPrefix(original, trackingPrefix) {
				return match
			}

			hash := HashLink(opts.LinkScope, original)
			if !seen[hash] {
				seen[hash] = true
				links = append(links, Link{URL: original, Hash: hash})
			}

			return strings.Replace(match, original, s.ClickURL(opts.EmailID, hash), 1)
		})
	}

	if opts.Opens {
		pixel := fmt.Sprintf(`<img src="%s" width="1" height="1" alt="" style="display:none" />`, s.OpenURL(opts.EmailID))
		if strings.Contains(html, "</body>") {
			html = strings.Replace(html, "</body>", pixel+"</body>", 1)
		} else {
			html += pixel
		}
	}

	return html, links
}

// botUASubstrings marks user agents that fetch a pixel without a human
// having opened the message.
//
// The line is prefetch against display. A security appliance
// (Proofpoint, Barracuda, Mimecast) fetches every URL on arrival
// before anyone reads it, so counting that is a fabricated open and it
// belongs here. An image proxy (GoogleImageProxy and friends) fetches
// BECAUSE the recipient opened the message - listing those discarded
// most real opens and left rates near zero with nothing to explain it,
// so they are deliberately absent.
//
// Gmail caches the image, so repeat opens by the same recipient may
// never reach us: first-open data is sound, per-open counts
// under-report. That is pixel tracking, not this list.
var botUASubstrings = []string{
	"bot", "crawler", "spider", "scanner",
	"barracuda", "mimecast", "proofpoint",
	"symantec", "trendmicro", "fortinet",
	"sophos", "messagelabs", "ironport",
	"safelinks",
	"http-client", "go-http-client", "curl/", "wget/", "python-requests",
	"headlesschrome", "phantomjs", "puppeteer", "playwright",
}

// IsBotUA reports whether the user agent belongs to something that
// fetched the pixel on its own account rather than because a person
// opened the message. An empty UA counts as one: every real mail
// client sends something.
func IsBotUA(ua string) bool {
	return BotReason(ua) != ""
}

// BotReason is IsBotUA with the matching token, so a caller can log
// why an open was discarded. Empty means the agent looks human.
//
// Every rejection on the tracking path answers with the same transparent
// GIF, so without a reason recorded, a discarded open, a bad signature
// and an unknown message id are indistinguishable - and an operator
// watching their open rate sit at zero has no way to tell which is
// happening.
func BotReason(ua string) string {
	if ua == "" {
		return "empty user agent"
	}

	low := strings.ToLower(ua)
	for _, needle := range botUASubstrings {
		if strings.Contains(low, needle) {
			return needle
		}
	}

	return ""
}
