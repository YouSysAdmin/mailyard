// Mailyard, Copyright (c) 2021-2026 YouSysAdmin

package email

import (
	"bytes"
	"context"
	"encoding/base64"
	"fmt"
	"log/slog"
	"maps"
	"net/mail"
	"strings"
	"time"

	"github.com/yousysadmin/mailyard/internal/core/ids"

	"github.com/yousysadmin/mailyard/internal/core/blob"
	"github.com/yousysadmin/mailyard/internal/core/env"
	"github.com/yousysadmin/mailyard/internal/core/metrics"
	"github.com/yousysadmin/mailyard/internal/core/notify"
	"github.com/yousysadmin/mailyard/internal/core/quota"
	"github.com/yousysadmin/mailyard/internal/core/safego"
	"github.com/yousysadmin/mailyard/internal/core/smtpclient"
	coretracking "github.com/yousysadmin/mailyard/internal/core/tracking"
	"github.com/yousysadmin/mailyard/internal/domain/store"
	emailmodel "github.com/yousysadmin/mailyard/internal/models/email"
	whmodel "github.com/yousysadmin/mailyard/internal/models/webhook"
)

// RequestError marks a validation failure the endpoint maps to a 400
// (as opposed to an infrastructure error, which stays a 500).
type RequestError struct{ msg string }

// Error renders the failure for a log or a caller.
func (e *RequestError) Error() string { return e.msg }

// NewRequestError builds a RequestError for callers outside this
// package (the SMTP relay maps them to permanent 5xx replies).
func NewRequestError(msg string) *RequestError { return &RequestError{msg: msg} }

func reqErrf(format string, args ...any) error {
	return &RequestError{msg: fmt.Sprintf(format, args...)}
}

// protectedHeaders are message headers the builder owns. Custom
// headers colliding with them are rejected so a caller cannot spoof
// the envelope or break the MIME structure.
var protectedHeaders = map[string]struct{}{
	"from": {}, "to": {}, "cc": {}, "bcc": {}, "subject": {}, "date": {},
	"mime-version": {}, "content-type": {}, "content-transfer-encoding": {},
	"list-unsubscribe": {}, "list-unsubscribe-post": {}, "return-path": {},
	"message-id": {}, "received": {}, "dkim-signature": {},
}

// HeaderDisplayTo and HeaderDisplayCc are the keys under which the
// client's own To and Cc headers ride in Email.Headers from submission
// to the processor, which lifts them out before building. They are in
// protectedHeaders, so a caller cannot smuggle either in.
const (
	HeaderDisplayTo = "To"
	HeaderDisplayCc = "Cc"
)

// withDisplayRecipients returns the custom headers with the client's
// To and Cc added under the reserved keys, or the headers unchanged
// when the request carries neither. Copies rather than mutating the
// request's map.
func withDisplayRecipients(req *SendRequest) map[string]string {
	if req.HeaderTo == "" && req.Cc == "" {
		return req.Headers
	}

	out := make(map[string]string, len(req.Headers)+2)
	maps.Copy(out, req.Headers)
	if req.HeaderTo != "" {
		out[HeaderDisplayTo] = req.HeaderTo
	}

	if req.Cc != "" {
		out[HeaderDisplayCc] = req.Cc
	}

	return out
}

// validHeaderName reports whether name is an RFC 5322 field name: one
// or more printable US-ASCII characters other than the colon. Leading
// or trailing whitespace fails - it is not part of any name, and a
// receiver that trims it would match a reserved header this check
// would otherwise have missed.
func validHeaderName(name string) bool {
	if name == "" {
		return false
	}

	for i := range len(name) {
		ch := name[i]
		if ch < 33 || ch > 126 || ch == ':' {
			return false
		}
	}

	return true
}

// Service is the send pipeline entry: validate, persist as queued or
// scheduled, wake the worker. Handlers construct it per request from
// the Runtime (cheap field copies).
type Service struct {
	Store       *store.Store
	Sending     env.SendingConfig
	MaxAttempts int
	Wake        func()
	Emit        func(ctx context.Context, event string, e *emailmodel.Email)
	Log         *slog.Logger

	// Blob offloads attachment bytes when set (nil keeps them
	// inline as base64 in the row).
	Blob blob.Store

	// Tracking signs the hosted unsubscribe link for sends scoped to
	// an opt-out list. Nil or disabled means no link is minted.
	Tracking *coretracking.Signer

	// Quota is told what each volume check saw, so somebody hears about
	// a plan limit before and when it refuses a send. Nil means the
	// check just answers, which is how this worked while
	// notification.TypeQuota was declared and raised by nothing.
	Quota func(projID string) quota.Observer
}

// NewService builds the Service from the Runtime. Safe before the
// queue or dispatcher exist (Wake and Emit become no-ops).
func NewService(rt *env.Runtime) *Service {
	wake := func() {}
	if rt.Queue != nil {
		wake = rt.Queue.Wake
	}

	emit := func(context.Context, string, *emailmodel.Email) {}
	if rt.Dispatch != nil {
		d := rt.Dispatch
		emit = func(ctx context.Context, event string, e *emailmodel.Email) {
			d.Emit(ctx, e.ProjectID, event, e.Sender, EventPayload(e))
		}
	}

	return &Service{
		Store:       rt.Store,
		Sending:     rt.Config.Sending,
		MaxAttempts: rt.Config.Worker.MaxAttempts,
		Wake:        wake,
		Emit:        emit,
		Log:         rt.Log,
		Blob:        rt.Blob,
		Tracking:    rt.Tracking,

		// Read from the runtime LAZILY: the campaign runner builds a
		// service before serve.go has a Raiser, and capturing the field
		// here would give that one a nil forever.
		//
		// Filed off the send path. It is one upsert, deduped, and only
		// while a project is at 80% of a window - but that is the busiest
		// moment a project has, and a courtesy must not be on the
		// critical path of the thing it is a courtesy about.
		Quota: func(projID string) quota.Observer {
			if rt.Notify == nil {
				return nil
			}

			return func(window string, used, limit int, plan string) {
				// Asked BEFORE the goroutine, not inside it. This runs
				// on every accepted message and answers no for almost
				// all of them - spawning first meant every send paid
				// for a goroutine to learn there was nothing to say.
				if !notify.QuotaWorthRaising(used, limit) {
					return
				}

				raiser, id, log := rt.Notify, projID, rt.Log
				go func() {
					defer safego.Recover(log, "quota notification", "project_id", id)
					raiser.QuotaObserver(context.Background(), id)(window, used, limit, plan)
				}()
			}
		},
	}
}

// EventPayload is the webhook "data" shape for email events: the
// delivery state without the (potentially large) bodies and
// attachments.
func EventPayload(e *emailmodel.Email) map[string]any {
	return map[string]any{
		"id":            e.ID,
		"project_id":    e.ProjectID,
		"sender":        e.Sender,
		"recipients":    e.Recipients,
		"subject":       e.Subject,
		"template_name": e.TemplateName,
		"status":        e.Status,
		"error_message": e.ErrorMessage,
		"attempts":      e.Attempts,
		"created_at":    e.CreatedAt,
		"sent_at":       e.SentAt,
	}
}

// SendRequest is one send, already bound and field-validated by the
// endpoint. From and To entries may carry display names. ID lets the
// campaign runner pin the email id up front (tracking links embed
// it before the row exists). The ListUnsubscribe fields stamp
// RFC 2369 / 8058 headers.
type SendRequest struct {
	ID   string
	From string

	// To is the envelope - who the message is delivered to.
	To []string

	// HeaderTo and Cc are the To and Cc headers as the CLIENT wrote
	// them, when they differ from the envelope. Only submission sets
	// them: a recipient the client put in Bcc arrives as an RCPT TO
	// with no header naming it, and printing the envelope as To showed
	// every Bcc address to every other recipient. Stored under the
	// reserved keys in Email.Headers, which is why Validate refuses
	// those keys from a caller.
	HeaderTo string
	Cc       string

	Subject               string
	HTML                  string
	Text                  string
	Headers               map[string]string
	Attachments           []emailmodel.Attachment
	SendAt                *time.Time
	TemplateName          string
	ListUnsubscribeURL    string
	ListUnsubscribeMailto string
	ListUnsubscribePost   bool

	// UnsubscribeListID scopes to send to a transactional opt-out
	// list: recipients who opted out of it are dropped, and the
	// system variable renders a one-click link bound to it.
	UnsubscribeListID string

	// Track lets a caller turn tracking off for this one message,
	// whatever the project default says.
	Track TrackPref

	// Tracked says the body ALREADY carries tracking, applied by the
	// campaign runner. It stops Send processing the same body twice,
	// which would wrap the click redirects in click redirects.
	Tracked bool

	// Route selects the SMTP server or group. Zero value means the
	// project's default group. Endpoints fill it through
	// Service.ResolveRoute, which turns a caller's group slug into an
	// id and rejects one that does not exist.
	Route Route
}

// ResolveRoute turns the caller-facing selectors into a Route.
//
// The slug is translated here, once, and never stored: this is the
// only point where an unknown group can still be answered with a bad
// request rather than a message that sits in the queue and then fails
// delivery for a reason nobody can see from the outside.
func (s *Service) ResolveRoute(ctx context.Context, projID, serverID, groupSlug string) (Route, error) {
	route := Route{ServerID: serverID}
	if groupSlug == "" {
		return route, nil
	}

	if s.Store.SMTPGroup == nil {
		return route, reqErrf("smtp server groups are unavailable")
	}

	g, err := s.Store.SMTPGroup.GetBySlug(ctx, projID, groupSlug)
	if err != nil {
		return route, err
	}

	if g == nil {
		return route, reqErrf("smtp server group %q does not exist", groupSlug)
	}

	route.GroupID = g.ID

	return route, nil
}

// Validate runs every pre-flight check without touching the emails
// table. Returned errors are *RequestError (caller mistakes) or
// infrastructure errors.
//
// Two halves. ValidateShape asks whether the MESSAGE is well formed and
// needs no store, and runs on every path that builds one - the sandbox
// included, which skips the rest by design. The remainder asks whether
// this project may DELIVER it: ownership of the sender, a server to
// carry it, the list a one-click link binds to.
func (s *Service) Validate(ctx context.Context, projID string, req *SendRequest) error {
	if err := s.ValidateShape(req); err != nil {
		return err
	}

	// Ownership of the From domain, before anything about servers.
	// It is the more fundamental failure of the two: being told to
	// configure an SMTP server is unhelpful when the real answer is
	// that this project may not send as this address at all.
	sender := senderAddress(req.From)
	if err := RequireVerifiedSender(ctx, s.Store, projID, sender); err != nil {
		return err
	}

	// Same resolution the processor will run at delivery time, through
	// the same function, so an accepted send is one that something can
	// actually carry - including the platform pool, for a project that
	// has configured no server of its own.
	srv, err := ResolveServer(ctx, s.Store, projID, sender, req.Route)
	if err != nil {
		return err
	}

	if srv == nil {
		return reqErrf("no enabled smtp server accepts sender %q, configure one first", sender)
	}

	// A one-click link is bound to one address. With several
	// recipients on one message there is no correct link to embed, so
	// refuse rather than mint one that would unsubscribe the wrong
	// person.
	if req.UnsubscribeListID != "" {
		if len(req.To) > 1 {
			return reqErrf("a send scoped to an unsubscribe list must have exactly one recipient, got %d", len(req.To))
		}

		l, err := s.Store.UnsubscribeList.Get(ctx, projID, req.UnsubscribeListID)
		if err != nil {
			return err
		}

		if l == nil {
			return reqErrf("unsubscribe list %q not found in this project", req.UnsubscribeListID)
		}

		if !l.Active {
			return reqErrf("unsubscribe list %q is inactive", l.Name)
		}
	}

	// Strict sender mode: the project requires every From address to
	// be registered under /api/senders.
	w, err := s.Store.Project.Get(ctx, projID)
	if err != nil {
		return err
	}

	if w != nil && w.StrictSenders {
		reg, err := s.Store.Sender.GetByEmail(ctx, projID, sender)
		if err != nil {
			return err
		}

		if reg == nil {
			return reqErrf("sender %q is not a registered sender address (strict mode is on for this project)", sender)
		}
	}

	return nil
}

// ValidateShape is the half of Validate that needs no store: is this a
// message the builder may be handed. Line breaks in anything that
// becomes a header, address syntax, the recipient ceiling, a subject
// and a body, header names against the reserved list, attachment
// sizes, and the List-Unsubscribe pair.
//
// Separate because the sandbox returned BEFORE Validate, and so before
// every one of these - a sandbox request with "\r\nBcc:" in
// list_unsubscribe_url wrote a raw message carrying a forged header.
// Nothing was sent, but "one place decides what may reach Build" was
// not true, and the ceilings an operator configured did not apply to
// a surface any key could reach.
func (s *Service) ValidateShape(req *SendRequest) error {
	// net/mail accepts a bare CR or LF inside a trailing comment -
	// `a@b.c (x\r\nBcc: ...)` parses, and Build writes the string
	// verbatim - so parsing is not the whole check. Proven by running
	// it: the comment became a second header the platform then signed.
	if strings.ContainsAny(req.From, "\r\n") {
		return reqErrf("from address contains a line break")
	}

	if _, err := mail.ParseAddress(req.From); err != nil {
		return reqErrf("from address %q is invalid", req.From)
	}

	if strings.ContainsAny(req.HeaderTo, "\r\n") || strings.ContainsAny(req.Cc, "\r\n") {
		return reqErrf("a recipient header contains a line break")
	}

	if len(req.To) == 0 {
		return reqErrf("at least one recipient is required")
	}

	if s.Sending.MaxRecipients > 0 && len(req.To) > s.Sending.MaxRecipients {
		return reqErrf("too many recipients: %d exceeds the limit of %d", len(req.To), s.Sending.MaxRecipients)
	}

	for _, rcpt := range req.To {
		if strings.ContainsAny(rcpt, "\r\n") {
			return reqErrf("recipient address contains a line break")
		}

		if _, err := mail.ParseAddress(rcpt); err != nil {
			return reqErrf("recipient address %q is invalid", rcpt)
		}
	}

	if req.Subject == "" {
		return reqErrf("subject is required")
	}

	if req.HTML == "" && req.Text == "" {
		return reqErrf("either html or text body is required")
	}

	for key := range req.Headers {
		// The name is checked BEFORE the reserved-name lookup, and
		// against RFC 5322 ftext (printable ASCII, no colon) rather
		// than a character blocklist: "Bcc " with a trailing space is
		// not in protectedHeaders, and a lenient receiver folds the
		// space away and honours it as Bcc.
		if !validHeaderName(key) {
			return reqErrf("header %q is not a valid header name", key)
		}

		if _, protected := protectedHeaders[strings.ToLower(key)]; protected {
			if hint := reservedHeaderHint[strings.ToLower(key)]; hint != "" {
				return reqErrf("header %q is reserved and cannot be overridden - %s", key, hint)
			}

			return reqErrf("header %q is reserved and cannot be overridden", key)
		}

		if strings.ContainsAny(req.Headers[key], "\r\n") {
			return reqErrf("header %q contains invalid characters", key)
		}
	}

	if len(req.Attachments) > 0 {
		if err := smtpclient.ValidateAttachments(toClientAttachments(req.Attachments),
			s.Sending.MaxAttachmentSize, s.Sending.MaxTotalAttachmentSize); err != nil {
			return &RequestError{msg: err.Error()}
		}
	}

	if req.SendAt != nil && req.SendAt.Before(time.Now().UTC().Add(-time.Minute)) {
		return reqErrf("send_at is in the past")
	}

	return normalizeUnsubscribeLinks(req)
}

// withRegisteredName returns the From to store: the caller's own if it
// already names somebody, otherwise the registered sender's name over
// the same address.
//
// A caller who writes their own name keeps it - the registered one is a
// default for an address, not an override of the message. A bare address
// with no registered sender, or one registered without a name, comes back
// unchanged, and a lookup failure is not fatal: a missing display name
// must never be the reason mail does not go out.
func (s *Service) withRegisteredName(ctx context.Context, projID, from string) (string, error) {
	addr := smtpclient.EnvelopeAddress(from)
	if addr != strings.TrimSpace(from) {
		// The caller sent "Name <addr>" - theirs wins.
		return from, nil
	}

	reg, err := s.Store.Sender.GetByEmail(ctx, projID, strings.ToLower(addr))
	if err != nil {
		slog.Warn("could not read the registered sender for a display name",
			"project_id", projID, "sender", addr, "err", err)

		return from, nil
	}

	if reg == nil || reg.Name == "" {
		return from, nil
	}

	return smtpclient.FormatAddress(reg.Name, addr), nil
}

// resolveSystemLinks swaps the reserved {{ mailyard_* }} placeholders
// in the stored message for this message's real URLs.
//
// It runs after the id is minted, because that is what the placeholders
// were standing in for: a template is rendered before any particular
// message exists, so the render substitutes something stable and this
// pass finishes the job.
//
// An absent URL REMOVES its placeholder rather than leaving it. A send
// with no opt-out scope, or an install with no public URL, would
// otherwise ship `/__mailyard_unsubscribe__` as an href, which a mail
// client resolves against nothing and shows as a dead link.
//
// Campaign sends arrive already substituted by the runner, so the guard
// finds nothing and this returns without minting a token.
func (s *Service) resolveSystemLinks(e *emailmodel.Email, unsubURL string) {
	if !hasSystemLinks(e) {
		return
	}

	links := coretracking.Links{Unsubscribe: unsubURL}
	if s.Tracking != nil && s.Tracking.Enabled() {
		links.WebView = s.Tracking.WebViewURL(e.ID)
	}

	e.Subject = coretracking.SubstituteSystemLinks(e.Subject, links)
	e.HTMLBody = coretracking.SubstituteSystemLinks(e.HTMLBody, links)
	e.TextBody = coretracking.SubstituteSystemLinks(e.TextBody, links)
}

// hasSystemLinks reports whether any part of the message still carries
// a placeholder.
//
// Asked before the substitution because minting the web view URL is an
// HMAC, and most messages reference no system variable at all.
func hasSystemLinks(e *emailmodel.Email) bool {
	return coretracking.HasSystemSentinels(e.Subject) ||
		coretracking.HasSystemSentinels(e.HTMLBody) ||
		coretracking.HasSystemSentinels(e.TextBody)
}

// Send validates and persists the email as queued (or scheduled when
// SendAt is set), then wakes the worker. Suppressed recipients are
// filtered out and returned - when every recipient is suppressed the
// row is persisted with status suppressed for the audit trail and
// nothing is delivered. Delivery is asynchronous: poll
// GET /emails/:id/status for the outcome.
func (s *Service) Send(ctx context.Context, projID, createdBy, apiKeyID string, req *SendRequest) (*emailmodel.Email, []string, error) {
	if err := s.Validate(ctx, projID, req); err != nil {
		return nil, nil, err
	}

	var observe quota.Observer
	if s.Quota != nil {
		observe = s.Quota(projID)
	}

	if err := quota.CheckSend(ctx, s.Store, projID, observe); err != nil {
		return nil, nil, err
	}

	allowed, blocked, err := s.Store.Suppression.FilterSuppressedForList(
		ctx, projID, req.UnsubscribeListID, req.To)
	if err != nil {
		return nil, nil, err
	}

	// A scoped send gets its one-click link minted here, bound to the
	// single recipient. See Validate for why there can only be one.
	//
	// The link is only STAMPED here. Substituting it into the body
	// waits until the id exists, below, because the web view variable
	// shares that pass and cannot be built any earlier.
	unsubURL := ""
	if req.UnsubscribeListID != "" && len(allowed) == 1 && s.Tracking != nil && s.Tracking.Enabled() {
		addr := strings.ToLower(smtpclient.EnvelopeAddress(allowed[0]))
		unsubURL = s.Tracking.ListUnsubscribeURL(req.UnsubscribeListID, addr)
		// RFC 8058: mailbox providers surface their own unsubscribe
		// control from these headers.
		if req.ListUnsubscribeURL == "" {
			req.ListUnsubscribeURL = unsubURL
			req.ListUnsubscribePost = true
		}
	}

	// The FROM gets the display name the project registered for that
	// address, when the caller did not send one of their own.
	//
	// senders.name was WRITTEN and READ BACK and never put on a message.
	// The console's picker showed "Name <address>" in its list and sent
	// the bare address, so every send from a registered sender arrived as
	// `From: no-reply@faria.co` with no name - reported from a real
	// inbox. Applied here rather than in the console because the API and
	// the console both send one field, and the name belongs to the
	// ADDRESS, not to whoever is composing.
	from, err := s.withRegisteredName(ctx, projID, req.From)
	if err != nil {
		return nil, nil, err
	}

	now := time.Now().UTC()
	e := &emailmodel.Email{
		ID:                    req.ID,
		ProjectID:             projID,
		CreatedBy:             createdBy,
		APIKeyID:              apiKeyID,
		SMTPServerID:          req.Route.ServerID,
		SMTPGroupID:           req.Route.GroupID,
		Sender:                from,
		Recipients:            allowed,
		Subject:               req.Subject,
		TemplateName:          req.TemplateName,
		HTMLBody:              req.HTML,
		TextBody:              req.Text,
		Attachments:           req.Attachments,
		Headers:               withDisplayRecipients(req),
		ListUnsubscribeURL:    req.ListUnsubscribeURL,
		ListUnsubscribeMailto: req.ListUnsubscribeMailto,
		UnsubscribeListID:     req.UnsubscribeListID,
		ListUnsubscribePost:   req.ListUnsubscribePost,
		Tracked:               req.Tracked,
		Status:                emailmodel.StatusQueued,
		MaxAttempts:           s.MaxAttempts,
		NextAttemptAt:         &now,
		CreatedAt:             now,
	}
	if e.ID == "" {
		e.ID = ids.New()
	}

	s.resolveSystemLinks(e, unsubURL)

	// After the id exists and before the row is written, so the stored
	// body is what actually went out - the email log then shows the
	// message the recipient got, tracking and all. Campaign sends
	// arrive here already processed by the runner and carry Tracked,
	// so this leaves them alone.
	if !e.Tracked {
		s.applyTracking(ctx, projID, e, req.Track)
	}

	if len(allowed) == 0 {
		e.Recipients = req.To
		e.Status = emailmodel.StatusSuppressed
		e.ErrorMessage = "all recipients are suppressed"
		e.NextAttemptAt = nil
		if err := s.Store.Email.Put(ctx, e); err != nil {
			return nil, nil, err
		}

		s.Log.Info("email: suppressed", "email_id", e.ID, "project_id", projID)
		s.Emit(ctx, whmodel.EventEmailSuppressed, e)

		return e, blocked, nil
	}

	if req.SendAt != nil && req.SendAt.After(now) {
		e.Status = emailmodel.StatusScheduled
		e.ScheduledAt = req.SendAt
		e.NextAttemptAt = req.SendAt
	}

	// Offload attachment bytes to the blob store before persisting -
	// the row then carries metadata plus keys, and the processor
	// rehydrates at send time.
	if err := s.offloadAttachments(ctx, e); err != nil {
		return nil, nil, fmt.Errorf("offload attachments: %w", err)
	}

	if err := s.Store.Email.Put(ctx, e); err != nil {
		return nil, nil, err
	}

	if e.Status == emailmodel.StatusQueued {
		s.Wake()
	}

	metrics.EmailsAccepted.Inc()
	s.Log.Info("email: accepted", "email_id", e.ID, "project_id", projID,
		"status", e.Status, "recipients", len(e.Recipients), "suppressed", len(blocked))
	s.Emit(ctx, whmodel.EventEmailQueued, e)

	return e, blocked, nil
}

// Retry re-queues a failed email with a fresh attempt budget.
func (s *Service) Retry(ctx context.Context, projID, id string) (*emailmodel.Email, error) {
	e, err := s.Store.Email.Get(ctx, projID, id)
	if err != nil {
		return nil, err
	}

	if e == nil {
		return nil, nil
	}

	if e.Status != emailmodel.StatusFailed {
		return nil, reqErrf("only failed emails can be retried (status is %q)", e.Status)
	}

	ok, err := s.Store.Email.Reset(ctx, projID, id)
	if err != nil {
		return nil, err
	}

	if !ok {
		return nil, reqErrf("email is no longer in a retryable state")
	}

	s.Wake()

	return s.Store.Email.Get(ctx, projID, id)
}

// offloadAttachments moves inline base64 content into the blob store,
// keyed under the email id. No-op without a store.
func (s *Service) offloadAttachments(ctx context.Context, e *emailmodel.Email) error {
	if s.Blob == nil {
		return nil
	}

	for i := range e.Attachments {
		a := &e.Attachments[i]
		if a.Content == "" || a.StorageKey != "" {
			continue
		}

		raw, err := base64.StdEncoding.DecodeString(a.Content)
		if err != nil {
			// Validation upstream already decoded this - treat a
			// mismatch here as a caller mistake.
			return reqErrf("attachment %q is not valid base64", a.Filename)
		}

		key := fmt.Sprintf("emails/%s/%d_%s", e.ID, i, blob.SanitizeFilename(a.Filename))
		if err := s.Blob.Put(ctx, key, bytes.NewReader(raw), a.ContentType); err != nil {
			return err
		}

		a.StorageKey = key
		a.Size = int64(len(raw))
		a.Content = ""
	}

	return nil
}

// LoadAttachment returns the decoded bytes of one attachment,
// reading inline content or the blob store as appropriate.
func LoadAttachment(ctx context.Context, bs blob.Store, a *emailmodel.Attachment) ([]byte, error) {
	return blob.Load(ctx, bs, a.StorageKey, a.Content, a.Filename)
}

// toClientAttachments converts the model attachments to the transport
// type (identical fields, distinct packages by design).
func toClientAttachments(in []emailmodel.Attachment) []smtpclient.Attachment {
	out := make([]smtpclient.Attachment, len(in))
	for i, a := range in {
		out[i] = smtpclient.Attachment{Filename: a.Filename, Content: a.Content, ContentType: a.ContentType}
	}

	return out
}
