// Mailyard, Copyright (c) 2021-2026 YouSysAdmin

// Package trackingpage serves the public /t/ surface: the open pixel,
// click redirects, the hosted unsubscribe page, and the hosted "view
// in browser" page. No session or API key auth - every route is
// authorized by its HMAC-signed URL (core/tracking mints them).
package trackingpage

import (
	"fmt"
	"html"
	"log/slog"
	"net/url"
	"strings"
	"time"

	"github.com/gofiber/fiber/v3"

	"github.com/yousysadmin/mailyard/internal/core/clientip"
	"github.com/yousysadmin/mailyard/internal/core/env"
	"github.com/yousysadmin/mailyard/internal/core/ids"
	"github.com/yousysadmin/mailyard/internal/core/tracking"

	cmodel "github.com/yousysadmin/mailyard/internal/models/campaign"
	supmodel "github.com/yousysadmin/mailyard/internal/models/suppression"
)

// transparentGIF is the classic 1x1 transparent pixel.
var transparentGIF = []byte{
	0x47, 0x49, 0x46, 0x38, 0x39, 0x61, 0x01, 0x00, 0x01, 0x00, 0x80, 0x00,
	0x00, 0x00, 0x00, 0x00, 0xff, 0xff, 0xff, 0x21, 0xf9, 0x04, 0x01, 0x00,
	0x00, 0x00, 0x00, 0x2c, 0x00, 0x00, 0x00, 0x00, 0x01, 0x00, 0x01, 0x00,
	0x00, 0x02, 0x02, 0x44, 0x01, 0x00, 0x3b,
}

// Handler owns the /t/ routes.
type Handler struct {
	Runtime *env.Runtime
}

// trackedEventsPerEmail caps the tracking_events rows one message can
// produce.
//
// These endpoints are unauthenticated on purpose, since the signed URL is
// the authority, and that signature never expires. The URL sits in the
// recipient's mailbox and can be replayed for as long as the mailbox
// exists. Without a ceiling, each replay writes another row, and
// tracking_events retention is opt in, so on a default install one
// leaked pixel URL grows the table forever.
//
// A ceiling rather than a rate limiter, because what is unbounded is the
// write, not the request. A per-IP limit would also be the wrong tool:
// Gmail fetches images through its own proxies, so any bound tight
// enough to stop a replay loop throttles genuine opens first. That is
// why the /tracking group has no limiter on it.
//
// At the ceiling the handler stops WRITING altogether - no counter
// increment, no campaign lookup, no event row - and answers the pixel or
// the redirect off the one read it has already done. Fifty answers
// everything the counters and the timeline are good for: a message with
// ten thousand recorded opens tells you nothing the fiftieth did not,
// and a replay loop past that point has to cost a read, not four writes.
const trackedEventsPerEmail = 50

func (h *Handler) signer() *tracking.Signer { return h.Runtime.Tracking }

// Open serves the pixel and records the open.
//
// The response is always the same transparent GIF, whatever happens:
// this endpoint answers a mail client rendering an image, and there is
// nothing useful to tell it. That makes the reasons an open is not
// recorded invisible from outside, so each one is logged. Chasing an
// open rate stuck at zero should be a matter of reading the log, not
// of guessing between four indistinguishable outcomes.
func (h *Handler) Open(c fiber.Ctx) error {
	emailID := strings.TrimSuffix(c.Params("file"), ".gif")
	ua := c.Get("User-Agent")
	pixel := func() error {
		c.Set(fiber.HeaderContentType, "image/gif")
		c.Set(fiber.HeaderCacheControl, "no-store, max-age=0")

		return c.Send(transparentGIF)
	}

	if !h.signer().Enabled() {
		slog.Warn("tracking: open ignored, tracking is not configured (set server.public_url and auth.jwt_secret)",
			"email_id", emailID)

		return pixel()
	}

	if !h.signer().VerifyOpen(emailID, c.Query("sig")) {
		slog.Warn("tracking: open ignored, bad signature",
			"email_id", emailID, "client_ip", clientip.From(c))

		return pixel()
	}

	if reason := tracking.BotReason(ua); reason != "" {
		slog.Debug("tracking: open ignored, automated fetch",
			"email_id", emailID, "matched", reason, "user_agent", ua)

		return pixel()
	}

	now := time.Now().UTC()
	ctx := c.Context()

	// The id in the URL is an EMAIL id, for campaign and transactional
	// mail alike - one identifier, so this handler resolves one thing.
	e, err := h.Runtime.Store.Email.GetAny(ctx, emailID)
	if err != nil {
		slog.Error("tracking: open lookup", "email_id", emailID, "err", err)

		return pixel()
	}

	if e == nil {
		// A correctly signed URL pointing at nothing. Worth saying out
		// loud: no amount of opening it will ever register, and the
		// usual cause is a retention sweep having removed the row.
		slog.Warn("tracking: open ignored, no email with this id", "email_id", emailID)

		return pixel()
	}

	if e.OpenCount >= trackedEventsPerEmail {
		return pixel()
	}

	// e.CreatedAt, not a derived value: the row was read three lines up,
	// so the partition is known exactly. See the store method.
	if _, _, err := h.Runtime.Store.Email.MarkOpened(ctx, emailID, e.CreatedAt, now); err != nil {
		slog.Error("tracking: mark opened", "email_id", emailID, "err", err)
	}

	// Campaign mail also keeps its per-message state, so campaign
	// reporting is unchanged by the move to email ids.
	campaignMessageID := ""
	if m, merr := h.Runtime.Store.Campaign.GetMessageByEmail(ctx, emailID); merr == nil && m != nil {
		campaignMessageID = m.ID
		if _, err := h.Runtime.Store.Campaign.MarkOpened(ctx, m.ID, now); err != nil {
			slog.Error("tracking: mark campaign message opened", "message_id", m.ID, "err", err)
		}
	}

	if err := h.Runtime.Store.Campaign.InsertTrackingEvent(ctx, &cmodel.TrackingEvent{
		EmailID: emailID, CampaignMessageID: campaignMessageID,
		EventType: cmodel.EventOpen, IP: clientip.From(c), UserAgent: ua,
	}); err != nil {
		slog.Error("tracking: record open", "email_id", emailID, "err", err)
	}

	return pixel()
}

// Click records the click and redirects to the original URL.
func (h *Handler) Click(c fiber.Ctx) error {
	emailID := c.Params("id")
	hash := c.Params("hash")
	ua := c.Get("User-Agent")
	if !h.signer().VerifyClick(emailID, hash, c.Query("sig")) {
		return c.Status(fiber.StatusNotFound).SendString("not found")
	}

	ctx := c.Context()
	e, err := h.Runtime.Store.Email.GetAny(ctx, emailID)
	if err != nil || e == nil {
		return c.Status(fiber.StatusNotFound).SendString("not found")
	}

	// Links are grouped by campaign for campaign mail and by project
	// for everything else, so the lookup needs to know which this is.
	campaignMessageID, scope := "", ""
	if m, merr := h.Runtime.Store.Campaign.GetMessageByEmail(ctx, emailID); merr == nil && m != nil {
		campaignMessageID, scope = m.ID, m.CampaignID
	}

	link, err := h.Runtime.Store.Campaign.GetTrackedLink(ctx, e.ProjectID, scope, hash)
	if err != nil || link == nil {
		return c.Status(fiber.StatusNotFound).SendString("not found")
	}

	// See trackedEventsPerEmail: past the ceiling this is a redirect and
	// nothing else.
	if e.ClickCount >= trackedEventsPerEmail {
		return redirectToTracked(c, link.OriginalURL)
	}

	// The REDIRECT is unconditional, the RECORDING is not.
	//
	// Opens were defended against automated fetches and clicks were not,
	// though the same appliances cause both: a security gateway
	// (Proofpoint, Barracuda, Safe Links) fetches every URL in a message
	// on arrival, and every wrapped link in our own mail is one of them.
	// So a message nobody had seen scored a click on every link in it -
	// and because MarkClicked backfills opened_at, an open too. Bot
	// filtering the pixel while leaving this open meant click rates were
	// systematically higher than open rates for the same audience.
	//
	// A bot still gets the redirect. Answering it with anything else
	// makes a scanner report our links as broken, and a person behind
	// that gateway is about to follow the same link for real - at which
	// point their own user agent records it.
	if reason := tracking.BotReason(ua); reason != "" {
		slog.Debug("tracking: click not recorded, automated fetch",
			"email_id", emailID, "matched", reason, "user_agent", ua)

		return redirectToTracked(c, link.OriginalURL)
	}

	now := time.Now().UTC()
	// e.CreatedAt for the reason on the pixel above: the row is already in
	// hand, so the UPDATE names its partition instead of visiting all of them.
	if _, err := h.Runtime.Store.Email.MarkClicked(ctx, emailID, e.CreatedAt, now); err != nil {
		slog.Error("tracking: mark clicked", "email_id", emailID, "err", err)
	}

	if campaignMessageID != "" {
		if err := h.Runtime.Store.Campaign.MarkClicked(ctx, campaignMessageID, now); err != nil {
			slog.Error("tracking: mark campaign message clicked",
				"message_id", campaignMessageID, "err", err)
		}
	}

	if err := h.Runtime.Store.Campaign.IncrementLinkClicks(ctx, link.ID); err != nil {
		slog.Error("tracking: count click", "link_id", link.ID, "err", err)
	}

	if err := h.Runtime.Store.Campaign.InsertTrackingEvent(ctx, &cmodel.TrackingEvent{
		EmailID: emailID, CampaignMessageID: campaignMessageID,
		EventType: cmodel.EventClick, TrackedLinkID: link.ID,
		IP: clientip.From(c), UserAgent: ua,
	}); err != nil {
		slog.Error("tracking: record click", "email_id", emailID, "err", err)
	}

	return redirectToTracked(c, link.OriginalURL)
}

// UnsubscribePage shows the confirmation page (a GET must not mutate:
// scanners prefetch links).
func (h *Handler) UnsubscribePage(c fiber.Ctx) error {
	token := c.Params("token")

	// Two token kinds land here: a campaign message (unsubscribes the
	// subscriber from that campaign's list) and a transactional
	// opt-out scope (suppresses the address for that list only).
	scope := "this list"
	if listID, _, err := h.signer().VerifyListUnsubscribeToken(token); err == nil {
		if l, lerr := h.Runtime.Store.UnsubscribeList.GetAny(c.Context(), listID); lerr == nil && l != nil {
			scope = l.Display()
		}
	} else if _, err := h.signer().VerifyUnsubscribeToken(token); err != nil {
		return pageResponse(c, fiber.StatusNotFound, "Link invalid",
			"This unsubscribe link is invalid or incomplete.")
	}

	body := pageBody(fmt.Sprintf(`<p>Click the button below to stop receiving %s emails.</p>
<form method="post" action="/tracking/unsubscribe/%s"><button type="submit">Unsubscribe</button></form>`,
		html.EscapeString(scope), html.EscapeString(token)))

	return pageResponse(c, fiber.StatusOK, "Unsubscribe", body)
}

// listUnsubscribe handles the transactional opt-out kind: write a
// suppression scoped to the list, leaving every other kind of mail to
// that address alone.
func (h *Handler) listUnsubscribe(c fiber.Ctx, listID, email string) error {
	ctx := c.Context()
	l, err := h.Runtime.Store.UnsubscribeList.GetAny(ctx, listID)
	if err != nil || l == nil {
		return pageResponse(c, fiber.StatusNotFound, "Link invalid",
			"This unsubscribe link no longer resolves.")
	}

	sup := &supmodel.Suppression{
		ID:                ids.New(),
		ProjectID:         l.ProjectID,
		Email:             strings.ToLower(email),
		Kind:              supmodel.KindListUnsubscribe,
		Reason:            "one-click unsubscribe from " + l.Name,
		UnsubscribeListID: l.ID,
	}
	if err := h.Runtime.Store.Suppression.Upsert(ctx, sup); err != nil {
		slog.Error("tracking: list unsubscribe", "list_id", listID, "err", err)

		return pageResponse(c, fiber.StatusInternalServerError, "Something went wrong",
			"The unsubscribe could not be processed. Please try again later.")
	}

	slog.Info("tracking: list unsubscribed", "list_id", l.ID, "project_id", l.ProjectID)

	return pageResponse(c, fiber.StatusOK, "Unsubscribed",
		pageBody("You will no longer receive "+html.EscapeString(l.Display())+" emails. Other messages are unaffected."))
}

// UnsubscribeConfirm performs the opt-out. RFC 8058 one-click POSTs
// land here directly.
func (h *Handler) UnsubscribeConfirm(c fiber.Ctx) error {
	token := c.Params("token")
	if listID, email, lerr := h.signer().VerifyListUnsubscribeToken(token); lerr == nil {
		return h.listUnsubscribe(c, listID, email)
	}

	messageID, err := h.signer().VerifyUnsubscribeToken(token)
	if err != nil {
		return pageResponse(c, fiber.StatusNotFound, "Link invalid",
			"This unsubscribe link is invalid or incomplete.")
	}

	ctx := c.Context()
	m, err := h.Runtime.Store.Campaign.GetMessageAny(ctx, messageID)
	if err != nil || m == nil {
		return pageResponse(c, fiber.StatusNotFound, "Link invalid",
			"This unsubscribe link no longer resolves.")
	}

	cam, err := h.Runtime.Store.Campaign.GetAny(ctx, m.CampaignID)
	if err != nil || cam == nil {
		return pageResponse(c, fiber.StatusNotFound, "Link invalid",
			"This unsubscribe link no longer resolves.")
	}

	if err := h.Runtime.Store.SubscriberList.Unsubscribe(ctx,
		cam.ProjectID, cam.ListID, m.SubscriberID, "unsubscribe link"); err != nil {
		slog.Error("tracking: unsubscribe", "message_id", messageID, "err", err)

		return pageResponse(c, fiber.StatusInternalServerError, "Something went wrong",
			"The unsubscribe could not be processed. Please try again later.")
	}

	// The unsubscribe token names a campaign message - unsubscribing
	// is from a list, which only campaign mail belongs to - so this
	// one keeps its campaign identity and carries the email id along
	// for the timeline.
	if err := h.Runtime.Store.Campaign.InsertTrackingEvent(ctx, &cmodel.TrackingEvent{
		EmailID: m.EmailID, CampaignMessageID: messageID,
		EventType: cmodel.EventUnsubscribe,
		IP:        clientip.From(c), UserAgent: c.Get("User-Agent"),
	}); err != nil {
		slog.Error("tracking: record unsubscribe", "message_id", messageID, "err", err)
	}

	slog.Info("tracking: unsubscribed", "message_id", messageID,
		"campaign_id", cam.ID, "list_id", cam.ListID)

	return pageResponse(c, fiber.StatusOK, "Unsubscribed",
		"You will no longer receive emails from this list.")
}

// webViewCSP replaces the site policy on the one response that returns
// tenant-authored HTML as a top-level document on the console's own
// origin.
//
// Without it that HTML runs as trusted first-party script. The site
// policy allows script-src 'unsafe-inline' for the Vue bundle, and the
// session cookie rides along on same-origin requests whether or not it
// is HttpOnly - so a project editor could put a script in an email,
// get an admin to open its "view in browser" link, and have it call
// /api/users as that admin. Editor to platform admin in one step.
//
// sandbox without allow-same-origin is the load-bearing part: it drops
// the document into an opaque origin, so even a bypass of the rest
// reaches nothing of ours. default-src 'none' then means no script
// runs at all. Images and inline styles stay on - they are what an
// email IS, and the reader chose to open it.
const webViewCSP = "sandbox; default-src 'none'; img-src data: http: https:; " +
	"style-src 'unsafe-inline'; font-src data:; form-action 'none'"

// WebView serves the hosted copy of a sent email.
func (h *Handler) WebView(c fiber.Ctx) error {
	emailID, err := h.signer().VerifyWebViewToken(c.Params("token"))
	if err != nil {
		status := fiber.StatusNotFound
		msg := pageBody("This link is invalid or incomplete.")
		if strings.Contains(err.Error(), "expired") {
			status = fiber.StatusGone
			msg = "This message is no longer available online."
		}

		return pageResponse(c, status, "Message unavailable", msg)
	}

	e, err := h.Runtime.Store.Email.GetAny(c.Context(), emailID)
	if err != nil || e == nil {
		return pageResponse(c, fiber.StatusNotFound, "Message unavailable",
			"This message is no longer available online.")
	}

	c.Set(fiber.HeaderContentType, fiber.MIMETextHTMLCharsetUTF8)
	c.Set(fiber.HeaderContentSecurityPolicy, webViewCSP)
	if e.HTMLBody != "" {
		return c.SendString(e.HTMLBody)
	}

	return c.SendString("<pre>" + html.EscapeString(e.TextBody) + "</pre>")
}

// redirectToTracked sends the reader on to the link a message carried.
//
// The scheme is checked again here although the writer of tracked_links
// (tracking.ProcessHTML) only ever captures http and https: this is an
// open redirect on our own domain by design, and the one thing that
// would turn it from "any page the sender linked" into "run this in the
// reader's browser" is a javascript: or data: target. Two writers of
// that table agreeing is what keeps this a formality - the check is for
// the day there is a third.
func redirectToTracked(c fiber.Ctx, target string) error {
	u, err := url.Parse(target)
	if err != nil || (u.Scheme != "http" && u.Scheme != "https") {
		return pageResponse(c, fiber.StatusNotFound, "Link invalid", "This link is invalid or incomplete.")
	}

	return c.Redirect().Status(fiber.StatusFound).To(target)
}

// pageBody is markup pageResponse writes into the page UNESCAPED. A
// distinct type rather than string so that a constant literal converts
// on its own while a value built at runtime has to be wrapped - and the
// wrap is where the reader looks for the html.EscapeString that has to
// have happened. Every one of these pages is open to the world.
type pageBody string

// pageResponse renders the minimal hosted page shell. title is escaped
// here, body is trusted - see pageBody.
func pageResponse(c fiber.Ctx, status int, title string, body pageBody) error {
	c.Set(fiber.HeaderContentType, fiber.MIMETextHTMLCharsetUTF8)

	return c.Status(status).SendString(fmt.Sprintf(`<!DOCTYPE html>
<html><head><meta charset="utf-8"><meta name="viewport" content="width=device-width, initial-scale=1">
<title>%s</title>
<style>
body { font-family: system-ui, sans-serif; max-width: 32rem; margin: 4rem auto; padding: 0 1rem; color: #222; }
button { font-size: 1rem; padding: 0.6rem 1.4rem; cursor: pointer; }
</style></head>
<body><h1>%s</h1>%s</body></html>`,
		html.EscapeString(title), html.EscapeString(title), body))
}
