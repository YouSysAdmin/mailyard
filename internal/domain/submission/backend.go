// Mailyard, Copyright (c) 2021-2026 YouSysAdmin

package submission

import (
	"context"
	"encoding/base64"
	"errors"
	"io"
	"log/slog"
	"net"
	"net/mail"
	"strconv"
	"strings"
	"time"

	"github.com/emersion/go-sasl"
	"github.com/emersion/go-smtp"

	"github.com/yousysadmin/mailyard/internal/core/mailparse"
	"github.com/yousysadmin/mailyard/internal/core/quota"
	"github.com/yousysadmin/mailyard/internal/core/safego"
	"github.com/yousysadmin/mailyard/internal/domain/email"
	"github.com/yousysadmin/mailyard/internal/domain/sandbox"
	"github.com/yousysadmin/mailyard/internal/domain/store"
	akmodel "github.com/yousysadmin/mailyard/internal/models/apikey"
	emailmodel "github.com/yousysadmin/mailyard/internal/models/email"
	perm "github.com/yousysadmin/mailyard/internal/models/permission"
	sbmodel "github.com/yousysadmin/mailyard/internal/models/sandbox"
	scmodel "github.com/yousysadmin/mailyard/internal/models/smtpcredential"

	"github.com/yousysadmin/mailyard/internal/core/iplimit"
)

// Sender is the slice of email.Service the submission listener needs. An interface
// so backend tests can run without a database.
type Sender interface {
	Send(ctx context.Context, projID, createdBy, apiKeyID string, req *email.SendRequest) (*emailmodel.Email, []string, error)
}

// Capturer is the slice of sandbox.Service the listener needs, for
// the same reason Sender is an interface.
type Capturer interface {
	Capture(ctx context.Context, req *sandbox.Request) (*sbmodel.Email, error)
}

// Backend authenticates sessions against the smtp_credentials table
// (or the api_keys table, see session.Auth) and hands accepted
// messages into the outbound pipeline - or, for a credential marked
// sandbox, into the project sandbox instead.
type Backend struct {
	Credentials    store.SMTPCredentialStore
	Keys           store.APIKeyStore
	Sender         Sender
	Sandbox        Capturer
	Log            *slog.Logger
	MaxMessageSize int64
	Limiter        *iplimit.Limiter
}

// touchInterval throttles last_used_at writes, mirroring the HTTP
// machine surface.
const touchInterval = time.Minute

// NewSession builds a Session.
func (b *Backend) NewSession(c *smtp.Conn) (smtp.Session, error) {
	remote := ""
	if c != nil && c.Conn() != nil {
		remote = c.Conn().RemoteAddr().String()
	}

	ip := remote
	if host, _, err := net.SplitHostPort(remote); err == nil {
		ip = host
	}

	if !b.Limiter.Allow(ip) {
		b.Log.Warn("submission: rate limited", "ip", ip)

		return nil, &smtp.SMTPError{Code: 421, EnhancedCode: smtp.EnhancedCode{4, 7, 0}, Message: "rate limit exceeded"}
	}

	return &session{backend: b, ip: ip}, nil
}

// headerDisableTracking turns tracking off for one submission. An
// SMTP client has no JSON body to put a flag in, so the instruction
// rides on a header.
//
// Namespaced rather than a bare X-Disable-Tracking: this is read from
// mail an arbitrary application composed, and a generic name is one
// another vendor's tooling may already set for its own purposes -
// which would silently switch tracking off here.
const headerDisableTracking = "X-Mailyard-Disable-Tracking"

// headerSandbox captures one message into the project sandbox instead
// of sending it, for a session that authenticated with an ORDINARY
// credential.
//
// One direction only. There is deliberately no header that turns the
// sandbox off, because the credential is what decides and a header
// that could override it would put the decision back in application
// code - where a value left over from a merge sends test mail to real
// customers. A sandbox credential reaches capture before this header
// is even read.
const headerSandbox = "X-Mailyard-Sandbox"

// headerSandboxRetention shortens how long one captured message is
// kept, in days. Longer than the platform window is clamped down to
// it, so this can ask for less and never for more.
const headerSandboxRetention = "X-Mailyard-Sandbox-Retention"

// takeControlHeader reports whether a control header is present and
// truthy, and REMOVES it either way.
//
// Removing matters: these headers are instructions to Mailyard, not
// part of the message. The submission listener does not currently forward custom
// headers at all, so nothing leaks today - but that is a property of
// the code five hundred lines away, not a decision anybody made here,
// and the day headers start being forwarded this control must not go
// out to the recipient's server with them.
func takeControlHeader(headers map[string]string, name string) bool {
	if headers == nil {
		return false
	}

	found, present := "", false
	for k, v := range headers {
		if strings.EqualFold(k, name) {
			found, present = v, true
			delete(headers, k)
		}
	}

	if !present {
		return false
	}

	// A bare header means yes - a client that bothered to add it meant
	// something by it - but an explicit negative value is honoured, so
	// a template that always emits the header can still say no.
	switch strings.ToLower(strings.TrimSpace(found)) {
	case "0", "false", "no", "off":
		return false
	default:
		return true
	}
}

// principal is the authenticated identity behind a session, whichever
// credential type produced it. The listener only ever needs the
// project to charge the send to, the user to attribute it to, and
// something to name in the log.
type principal struct {
	projectID string
	createdBy string

	// apiKeyID is set only on the api-key path, and lands on the
	// email row. Credential logins leave it empty.
	apiKeyID string

	// label identifies the credential in log lines.
	label string

	// smtpGroupID routes everything submitted on this session to a
	// named server pool. An SMTP client cannot pass a routing field
	// with the message, so the binding rides on the credential it
	// authenticated with. Empty uses the project's default group.
	smtpGroupID string

	// credentialID names the submission credential, for the sandbox
	// row. Empty on the api-key path, where apiKeyID says it instead.
	credentialID string

	// sandbox captures everything on this session instead of sending
	// it. Same reasoning as smtpGroupID and stronger: an SMTP client
	// has nowhere to put a flag, and this is one that must not be put
	// there anyway.
	sandbox bool
}

// session carries one SMTP connection. The principal stays set for
// the connection lifetime once AUTH succeeds - Reset only clears the
// per-message envelope.
type session struct {
	backend *Backend
	ip      string
	auth    *principal
	from    string
	to      []string

	// authFailures counts rejected AUTH attempts on this connection.
	// go-smtp answers a failed AUTH with 454 and leaves the connection
	// open, so without a cap one TCP connection - one hit against the
	// per-IP limiter, which is charged in NewSession - buys unlimited
	// password and api-key guesses.
	authFailures int
}

// maxAuthFailures ends a connection's credibility after a few misses.
// Real clients get it right on the first attempt, retry a second time at most.
const maxAuthFailures = 3

// AuthMechanisms implements smtp.Session, naming what this listener
// accepts.
func (s *session) AuthMechanisms() []string { return []string{sasl.Plain} }

// Auth accepts AUTH PLAIN through either credential type:
//
//   - an SMTP submission credential (username "smtp_..." plus its
//     password), the credential built for this listener, and
//   - an API key with scope send as the password, username ignored,
//     which keeps existing integrations working and lets a caller
//     reuse the credential it already holds for the HTTP API.
//
// Every rejection leg returns the same error, matching the uniform
// 401 of the HTTP machine surface.
func (s *session) Auth(mech string) (sasl.Server, error) {
	if mech != sasl.Plain {
		return nil, smtp.ErrAuthUnknownMechanism
	}

	return sasl.NewPlainServer(func(_, username, password string) error {
		// Refuse without touching the database once this connection has
		// burned its attempts. Cheap, and it stops the guessing loop
		// from costing a lookup per try.
		if s.authFailures >= maxAuthFailures {
			s.backend.Log.Warn("submission: auth attempts exhausted on connection", "ip", s.ip)

			return smtp.ErrAuthFailed
		}

		ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()

		var (
			p   *principal
			err error
		)
		if strings.HasPrefix(username, scmodel.UsernamePrefix) {
			p, err = s.authCredential(ctx, username, password)
		} else {
			p, err = s.authAPIKey(ctx, password)
		}

		if err != nil {
			s.authFailures++
			// Charge the per-IP budget per FAILED ATTEMPT, not just per
			// connection. Otherwise an attacker spends one token and
			// guesses freely inside the session, and reconnecting after
			// the per-connection cap costs one token per three guesses.
			// The return value is ignored on purpose: the attempt has
			// already failed, this call exists to consume budget so the
			// next connection from this IP is refused at NewSession.
			s.backend.Limiter.Allow(s.ip)

			return err
		}

		s.auth = p

		return nil
	}), nil
}

// authCredential resolves a submission credential login.
func (s *session) authCredential(ctx context.Context, username, password string) (*principal, error) {
	if s.backend.Credentials == nil {
		return nil, smtp.ErrAuthFailed
	}

	cred, err := s.backend.Credentials.GetByUsername(ctx, username)
	if err != nil {
		s.backend.Log.Error("submission: credential lookup failed", "err", err)

		return nil, smtp.ErrAuthFailed
	}

	now := time.Now().UTC()
	if cred == nil || !scmodel.HashEquals(password, cred.PasswordHash) ||
		!cred.IsValid() || !cred.AllowsIP(s.ip) {
		s.backend.Log.Warn("submission: auth rejected", "ip", s.ip, "username", username)

		return nil, smtp.ErrAuthFailed
	}

	if cred.LastUsedAt == nil || now.Sub(*cred.LastUsedAt) > touchInterval {
		if err := s.backend.Credentials.TouchLastUsed(ctx, cred.ID, now); err != nil {
			s.backend.Log.Warn("submission: touch last used failed", "credential_id", cred.ID, "err", err)
		}
	}

	return &principal{
		projectID:    cred.ProjectID,
		createdBy:    cred.CreatedBy,
		label:        "credential " + cred.ID,
		smtpGroupID:  cred.SMTPGroupID,
		credentialID: cred.ID,
		sandbox:      cred.Sandbox,
	}, nil
}

// authAPIKey resolves the legacy api-key login (key as the password).
func (s *session) authAPIKey(ctx context.Context, password string) (*principal, error) {
	if !strings.HasPrefix(password, akmodel.Prefix) {
		return nil, smtp.ErrAuthFailed
	}

	k, err := s.backend.Keys.GetByPrefix(ctx, akmodel.TokenPrefix(password))
	if err != nil {
		s.backend.Log.Error("submission: key lookup failed", "err", err)

		return nil, smtp.ErrAuthFailed
	}

	now := time.Now().UTC()
	// emails:write is what sending is called everywhere else now. The
	// send scope this replaces was the same grant under a word that
	// could only ever mean one thing.
	//
	// Except on a SANDBOX key, which is judged on the sandbox instead -
	// the same rule the HTTP accepting routes apply, because it is the
	// same question. A sandbox key holds nothing on emails by design,
	// so asking for emails:write here would refuse the AUTH of the one
	// credential a developer was handed to test with, and an SMTP
	// client reports that as a bad password.
	sending := perm.ResourceEmails
	if k != nil && k.Sandbox {
		sending = perm.ResourceSandbox
	}

	if k == nil || !akmodel.HashEquals(password, k.KeyHash) || !k.IsValid(now) ||
		!k.AllowsIP(s.ip) ||
		!perm.ForKey(k.Permissions, k.Sandbox).Has(sending, perm.ActionWrite) {
		s.backend.Log.Warn("submission: auth rejected", "ip", s.ip)

		return nil, smtp.ErrAuthFailed
	}

	if k.LastUsedAt == nil || now.Sub(*k.LastUsedAt) > touchInterval {
		if err := s.backend.Keys.TouchLastUsed(ctx, k.ID, now); err != nil {
			s.backend.Log.Warn("submission: touch last used failed", "key_id", k.ID, "err", err)
		}
	}

	return &principal{
		projectID: k.ProjectID,
		createdBy: k.CreatedBy,
		apiKeyID:  k.ID,
		label:     "api key " + k.ID,
		sandbox:   k.Sandbox,
	}, nil
}

// Reset implements smtp.Session, clearing the envelope between
// messages on one connection.
func (s *session) Reset() {
	s.from = ""
	s.to = nil
}

// Logout implements smtp.Session.
func (s *session) Logout() error { return nil }

// Mail implements smtp.Session, recording the envelope sender.
func (s *session) Mail(from string, _ *smtp.MailOptions) error {
	if s.auth == nil {
		return smtp.ErrAuthRequired
	}

	if from != "" {
		if addr, err := mail.ParseAddress(from); err == nil {
			s.from = addr.Address
		} else {
			s.from = from
		}
	}

	return nil
}

// Rcpt implements smtp.Session, adding one envelope recipient.
func (s *session) Rcpt(to string, _ *smtp.RcptOptions) error {
	if s.auth == nil {
		return smtp.ErrAuthRequired
	}

	addr := to
	if parsed, err := mail.ParseAddress(to); err == nil {
		addr = parsed.Address
	}

	s.to = append(s.to, addr)

	return nil
}

// Data reads the raw message (bounded by MaxMessageSize), parses it,
// and hands it to the outbound pipeline using the SMTP envelope
// (MAIL FROM and RCPT TO) rather than the header addresses, matching
// normal MTA behavior.
func (s *session) Data(r io.Reader) (err error) {
	// Same reasoning as the inbound listener: go-smtp does not guard
	// its session goroutines, and this one also feeds a MIME parser.
	// Authenticated, so the exposure is narrower, but a panic here
	// still takes down every other tenant's delivery.
	defer func() {
		if rec := recover(); rec != nil {
			safego.Report(s.backend.Log, "submission: data", rec, "from", s.from, "remote_ip", s.ip)
			err = &smtp.SMTPError{
				Code:         451,
				EnhancedCode: smtp.EnhancedCode{4, 3, 0},
				Message:      "temporary failure processing message",
			}
		}
	}()
	if s.auth == nil {
		return smtp.ErrAuthRequired
	}

	limit := s.backend.MaxMessageSize
	if limit <= 0 {
		limit = 26214400
	}

	// One extra byte so overflow is detectable.
	lr := &io.LimitedReader{R: r, N: limit + 1}
	raw, err := io.ReadAll(lr)
	if err != nil {
		return &smtp.SMTPError{Code: 451, EnhancedCode: smtp.EnhancedCode{4, 3, 0}, Message: "read error"}
	}

	if int64(len(raw)) > limit {
		return &smtp.SMTPError{Code: 552, EnhancedCode: smtp.EnhancedCode{5, 3, 4}, Message: "message size exceeds limit"}
	}

	// The sandbox branch, and it comes before the parse on purpose.
	//
	// Everything below this point - the parse, and then the whole of
	// email.Service.Validate - is about getting a message delivered:
	// verified sender domain, an enabled SMTP server, suppression
	// list, plan quota. A developer sending from test@localhost in a
	// project with nothing configured fails every one of those, and
	// that developer is exactly who the sandbox exists for. A
	// malformed message is captured too, because unparseable MIME is
	// a thing somebody comes here to look at.
	if s.auth.sandbox {
		return s.capture(raw)
	}

	parsed, perr := mailparse.Parse(raw)
	if perr != nil {
		return &smtp.SMTPError{Code: 554, EnhancedCode: smtp.EnhancedCode{5, 6, 0}, Message: "malformed message"}
	}

	// A sandbox opt-IN from an ordinary credential. The other
	// direction does not exist: see headerSandbox.
	if takeControlHeader(parsed.Headers, headerSandbox) {
		return s.capture(raw)
	}

	attachments := make([]emailmodel.Attachment, 0, len(parsed.Attachments))
	for _, a := range parsed.Attachments {
		attachments = append(attachments, emailmodel.Attachment{
			Filename:    a.Filename,
			ContentType: a.ContentType,
			Content:     base64.StdEncoding.EncodeToString(a.Content),
		})
	}

	// The envelope is who gets the message, the headers are what it
	// says. An SMTP client's Bcc is an RCPT TO with no header naming
	// it, so the To header is the CLIENT's, never rebuilt from the
	// envelope - that printed every Bcc address to every recipient.
	req := &email.SendRequest{
		From:        s.from,
		To:          s.to,
		HeaderTo:    strings.Join(parsed.To, ", "),
		Cc:          strings.Join(parsed.Cc, ", "),
		Subject:     parsed.Subject,
		HTML:        parsed.HTMLBody,
		Text:        parsed.TextBody,
		Attachments: attachments,
		Route:       email.Route{GroupID: s.auth.smtpGroupID},
	}
	if takeControlHeader(parsed.Headers, headerDisableTracking) {
		req.Track = email.TrackOff
	}

	// An application submitting its own bulk mail composes an ordinary
	// RFC 2369 header rather than reaching for a Mailyard-specific one,
	// so that is what is honored here. The builder owns the header, so
	// the value is lifted out and stamped back by the same code that
	// writes it for a campaign - which is also what stops a caller
	// smuggling a second List-Unsubscribe past validation.
	req.ListUnsubscribeURL, req.ListUnsubscribeMailto, req.ListUnsubscribePost =
		email.TakeUnsubscribeHeaders(parsed.Headers)

	// The client's Reply-To is carried the same way: the builder owns
	// the header, so the value travels as a field and is written once.
	req.ReplyTo = strings.TrimSpace(parsed.Headers[email.HeaderReplyTo])

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	e, blocked, serr := s.backend.Sender.Send(ctx, s.auth.projectID, s.auth.createdBy, s.auth.apiKeyID, req)
	if serr != nil {
		return s.mapSendError(serr)
	}

	s.backend.Log.Info("submission: message accepted",
		"email_id", e.ID, "project_id", e.ProjectID, "auth", s.auth.label,
		"recipients", len(e.Recipients), "suppressed", len(blocked))

	return nil
}

// capture stores the message in the project sandbox and answers 250.
//
// 250 is not a lie. The client asked for a message to be accepted, and
// it was - by a credential whose entire meaning is "accept and keep".
// Answering anything else would make every sandbox test look like a
// failure to the library that sent it, which is the opposite of what
// a test harness needs.
//
// The control headers are not stripped here, unlike on the sending
// path. In a sandbox the point is to show exactly what the
// application put on the wire, and quietly editing that would hide
// the header a developer is trying to confirm they set.
func (s *session) capture(raw []byte) error {
	if s.backend.Sandbox == nil {
		s.backend.Log.Error("submission: sandbox credential with no sandbox service", "auth", s.auth.label)

		return &smtp.SMTPError{Code: 451, EnhancedCode: smtp.EnhancedCode{4, 3, 0}, Message: "temporary failure"}
	}

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	e, err := s.backend.Sandbox.Capture(ctx, &sandbox.Request{
		ProjectID:     s.auth.projectID,
		Source:        sbmodel.SourceSubmission,
		CredentialID:  s.auth.credentialID,
		APIKeyID:      s.auth.apiKeyID,
		ClientIP:      s.ip,
		EnvelopeFrom:  s.from,
		Recipients:    s.to,
		Raw:           raw,
		RetentionDays: sandboxRetentionHeader(raw),
	})
	if err != nil {
		s.backend.Log.Error("submission: sandbox capture failed", "auth", s.auth.label, "err", err)

		return &smtp.SMTPError{Code: 451, EnhancedCode: smtp.EnhancedCode{4, 3, 0}, Message: "temporary failure"}
	}

	s.backend.Log.Info("submission: message captured into the sandbox",
		"sandbox_id", e.ID, "project_id", e.ProjectID, "auth", s.auth.label,
		"recipients", len(e.Recipients))

	return nil
}

// sandboxRetentionHeader reads a per-message retention window off the
// raw bytes. Zero when absent, malformed or unparseable - the
// platform default then applies, and the service clamps anything
// longer than it in any case.
func sandboxRetentionHeader(raw []byte) int {
	parsed, err := mailparse.Parse(raw)
	if err != nil || parsed.Headers == nil {
		return 0
	}

	for k, v := range parsed.Headers {
		if !strings.EqualFold(k, headerSandboxRetention) {
			continue
		}

		n, cerr := strconv.Atoi(strings.TrimSpace(v))
		if cerr != nil || n < 0 {
			return 0
		}

		return n
	}

	return 0
}

// mapSendError translates pipeline errors into SMTP replies: caller
// mistakes become permanent 550s carrying the reason, anything else a
// temporary 451 that hides internals.
func (s *session) mapSendError(err error) error {
	if re, ok := errors.AsType[*email.RequestError](err); ok {
		return &smtp.SMTPError{Code: 550, EnhancedCode: smtp.EnhancedCode{5, 6, 0}, Message: re.Error()}
	}

	if qe, ok := errors.AsType[*quota.Error](err); ok {
		// Transient by design - the window rolls or the plan grows.
		return &smtp.SMTPError{Code: 452, EnhancedCode: smtp.EnhancedCode{4, 7, 0}, Message: qe.Error()}
	}

	s.backend.Log.Error("submission: send failed", "ip", s.ip, "err", err)

	return &smtp.SMTPError{Code: 451, EnhancedCode: smtp.EnhancedCode{4, 3, 0}, Message: "temporary failure"}
}
