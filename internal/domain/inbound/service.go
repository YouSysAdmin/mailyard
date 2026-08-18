// Mailyard, Copyright (c) 2021-2026 YouSysAdmin

// Package inbound receives mail for verified domains: the MX-facing
// SMTP listener stores parsed messages per project and emits
// inbound.received webhooks.
package inbound

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"errors"
	"fmt"
	"log/slog"
	"strings"
	"time"

	"github.com/yousysadmin/mailyard/internal/core/ids"

	"github.com/yousysadmin/mailyard/internal/core/blob"
	"github.com/yousysadmin/mailyard/internal/core/dsn"
	"github.com/yousysadmin/mailyard/internal/core/env"
	"github.com/yousysadmin/mailyard/internal/core/mailauth"
	"github.com/yousysadmin/mailyard/internal/core/mailparse"
	"github.com/yousysadmin/mailyard/internal/core/metrics"
	"github.com/yousysadmin/mailyard/internal/core/smtpclient"
	"github.com/yousysadmin/mailyard/internal/database"
	"github.com/yousysadmin/mailyard/internal/domain/bounce"
	"github.com/yousysadmin/mailyard/internal/domain/store"
	bouncemodel "github.com/yousysadmin/mailyard/internal/models/bounce"
	dmodel "github.com/yousysadmin/mailyard/internal/models/domain"
	emailmodel "github.com/yousysadmin/mailyard/internal/models/email"
	imodel "github.com/yousysadmin/mailyard/internal/models/inbound"
	webhookmodel "github.com/yousysadmin/mailyard/internal/models/webhook"
)

var (
	// ErrUnverifiedDomain maps to SMTP 550 relay not permitted.
	ErrUnverifiedDomain = errors.New("recipient domain is not verified")

	// ErrSenderSuppressed maps to SMTP 550 sender suppressed. The
	// project suppression list doubles as an inbound blocklist.
	ErrSenderSuppressed = errors.New("sender is suppressed")

	// ErrDMARCFail maps to SMTP 550. Returned only when the From
	// domain publishes p=reject AND the operator turned
	// inbound.reject_on_dmarc_fail on.
	ErrDMARCFail = errors.New("dmarc policy rejects this message")

	// ErrDuplicate marks an already-ingested message. The listener
	// treats it as success so upstream MTA retries stay idempotent.
	ErrDuplicate = errors.New("duplicate message")
)

// Emitter matches dispatch.Dispatcher.Emit.
type Emitter func(ctx context.Context, projID, event, sender string, payload any)

// Service is the ingest pipeline shared by the SMTP listener (and a
// future webhook ingest path).
type Service struct {
	Domains      store.DomainStore
	Inbound      store.InboundStore
	Suppressions store.SuppressionStore

	// Emails and Bounces serve the DSN pipeline: a report carrying a
	// sending id becomes bounce records and suppressions instead of
	// just a stored message. Nil-safe - without them a report is
	// stored like any other mail.
	//
	// Emails is looked up by id ALONE, with no project scope, and
	// that is correct for mail arriving at OUR MX: the report lands on
	// the platform's own domain and the id is the only thing that says
	// whose message it was about. A transport carrying less authority
	// than that says so with Conn.ReportScope.
	Emails   store.EmailStore
	Bounces  store.BounceStore
	Projects store.ProjectStore
	Emit     Emitter
	Log      *slog.Logger
	MaxSize  int64

	// Hostname names this receiver in the Authentication-Results
	// header it stamps.
	Hostname string

	// RejectOnDMARCFail mirrors inbound.reject_on_dmarc_fail. Off by
	// default: the verdict is always recorded, this only decides
	// whether a p=reject failure is also refused at the SMTP layer.
	RejectOnDMARCFail bool

	// Blob offloads attachment bytes when set. Offload failures fall
	// back to inline storage - receiving mail beats saving DB space.
	Blob blob.Store
}

// NewService builds the ingest pipeline from the runtime.
//
// One constructor, because mail now arrives here through two
// transports: the local MX listener, and a relay node forwarding what
// its own MX accepted. They differ in how the bytes and the peer
// details reach this package and in nothing else - same verified
// domain lookup, same suppression check, same authentication, same
// dedup, same DSN intake. Two construction sites would be two places
// for one of those to be left out, and the one that went missing
// would be invisible: the mail still lands.
//
// Built regardless of inbound.enabled. A node's MX is the whole point
// of this installation not needing a port 25 of its own.
func NewService(rt *env.Runtime) *Service {
	cfg := rt.Config
	svc := &Service{
		Domains:      rt.Store.Domain,
		Inbound:      rt.Store.Inbound,
		Suppressions: rt.Store.Suppression,
		Projects:     rt.Store.Project,
		Bounces:      rt.Store.Bounce,
		Emails:       rt.Store.Email,
		Log:          rt.Log,
		MaxSize:      cfg.Inbound.MaxMessageSize,
		Blob:         rt.Blob,

		Hostname:          cfg.Inbound.Hostname,
		RejectOnDMARCFail: cfg.Inbound.RejectOnDMARCFail,
	}
	if rt.Dispatch != nil {
		svc.Emit = rt.Dispatch.Emit
	}

	return svc
}

// ResolveDomain returns the verified domain owning the recipient
// address, or nil when no project claims it.
func (s *Service) ResolveDomain(ctx context.Context, rcpt string) (*dmodel.Domain, error) {
	at := strings.LastIndex(rcpt, "@")
	if at < 0 || at == len(rcpt)-1 {
		return nil, nil
	}

	return s.Domains.GetVerifiedCovering(ctx, rcpt[at+1:])
}

// Conn carries what the transport knew and the bytes cannot say.
//
// IP and HELO because SPF needs the connecting address and the
// announced name, neither of which is recoverable from the message.
// ReportScope because a transport can also carry less authority than
// the default one.
type Conn struct {
	IP   string
	HELO string

	// ReportScope confines delivery-report attribution to one project.
	//
	// Empty is the ordinary case and means unconfined, which is
	// CORRECT for our own MX: a provider that owns the return path
	// forwards its bounce copy to a mailbox on the platform's domain,
	// so the receiving project and the sending project routinely
	// differ and the id is the only thing that says whose message it
	// was.
	//
	// A relay node enrolled by a TENANT has no such authority. It is a
	// machine on that tenant's network, and letting it file a bounce
	// against a neighbour's message - which needs only a uuid and one
	// real recipient - is the hole that made project nodes wait for
	// this field to exist.
	ReportScope string
}

// Ingest stores one received message. The envelope decides routing:
// every recipient must already have been resolved to the same domain by
// the listener's per-RCPT checks, since a mixed-domain session is split
// upstream by the sending MTA retrying per recipient.
func (s *Service) Ingest(ctx context.Context, d *dmodel.Domain, envelopeFrom string, envelopeTo []string, raw []byte, conn Conn) (*imodel.Email, error) {
	sender := strings.ToLower(strings.TrimSpace(envelopeFrom))
	now := time.Now().UTC()

	rec := &imodel.Email{
		ID:         ids.New(),
		ProjectID:  d.ProjectID,
		DomainID:   d.ID,
		Sender:     sender,
		Recipients: envelopeTo,
		Size:       int64(len(raw)),
		Status:     imodel.StatusReceived,
		ReceivedAt: now,
	}

	// The project suppression list doubles as an inbound
	// blocklist. Rejections are persisted for the audit trail.
	if sender != "" {
		suppressed, err := s.Suppressions.IsSuppressed(ctx, d.ProjectID, sender)
		if err != nil {
			return nil, err
		}

		if suppressed {
			rec.Status = imodel.StatusRejected
			rec.ErrorMessage = "sender is on the suppression list"
			if perr := s.Inbound.Put(ctx, rec); perr != nil {
				s.Log.Error("inbound: persist rejected failed", "err", perr)
			}

			return rec, ErrSenderSuppressed
		}
	}

	// Authenticate the sender before anything else looks at the
	// message. Until this ran, "Sender" on a stored inbound row was
	// whatever the peer typed at MAIL FROM and meant nothing at all.
	auth := mailauth.Verify(ctx, mailauth.Config{}, conn.IP, sender, conn.HELO, raw)
	rec.Auth = &imodel.Auth{
		SPF:         auth.SPF,
		DKIM:        auth.DKIM,
		DMARC:       auth.DMARC,
		DMARCPolicy: auth.DMARCPolicy,
		Aligned:     auth.Aligned,
		ClientIP:    conn.IP,
	}
	if !auth.Aligned {
		s.Log.Info("inbound: sender not authenticated",
			"sender", sender, "client_ip", conn.IP,
			"spf", auth.SPF, "dkim", auth.DKIM, "dmarc", auth.DMARC,
			"dmarc_policy", auth.DMARCPolicy)
	}

	// Refuse only when the DOMAIN OWNER asked for it and the operator
	// opted in. Both conditions, because either alone gets it wrong:
	// honoring p=reject unasked silently eats forwarded mail, and
	// refusing on our own judgement overrides a decision that is not
	// ours to make.
	if s.RejectOnDMARCFail && auth.Rejectable() {
		rec.Status = imodel.StatusRejected
		rec.ErrorMessage = "dmarc: sender domain publishes p=reject and no aligned identifier passed"
		if perr := s.Inbound.Put(ctx, rec); perr != nil {
			s.Log.Error("inbound: persist dmarc rejection failed", "err", perr)
		}

		return rec, ErrDMARCFail
	}

	parsed, perr := mailparse.Parse(raw)
	if perr != nil {
		// Malformed MIME is stored raw for debugging rather than
		// bounced - the sender already got a 250 for the envelope
		// checks and operators may still want the bytes.
		rec.Status = imodel.StatusFailed
		rec.ErrorMessage = fmt.Sprintf("parse: %v", perr)
		rec.Raw = raw
		if err := s.Inbound.Put(ctx, rec); err != nil {
			return nil, err
		}

		return rec, nil
	}

	rec.MessageID = parsed.MessageID
	rec.Subject = parsed.Subject
	rec.TextBody = parsed.TextBody
	rec.HTMLBody = parsed.HTMLBody
	rec.Headers = parsed.Headers

	// Stamp the verdict into the headers the way an MTA would, so
	// anything reading the stored message - a rule engine, a webhook
	// consumer, a person - sees the same answer the structured Auth
	// field carries. Overwriting any inbound Authentication-Results is
	// deliberate: a header the sender supplied is a forgery by
	// definition, since only the receiver can produce a real one.
	if rec.Headers == nil {
		rec.Headers = map[string]string{}
	}

	rec.Headers["Authentication-Results"] = mailauth.AuthenticationResults(s.Hostname, auth)

	// Idempotency before attachment offload. The other order wrote a
	// duplicate's attachments into the blob store under a fresh id and
	// then discarded the record, orphaning the objects - which turned
	// an MTA retry loop (or anyone replaying a Message-ID at the open
	// MX port) into unbounded disk growth.
	if rec.MessageID != "" {
		existing, err := s.Inbound.FindByMessageID(ctx, d.ProjectID, rec.MessageID)
		if err != nil {
			return nil, err
		}

		if existing != nil {
			return existing, ErrDuplicate
		}
	} else {
		rec.DedupHash = dedupHash(sender, envelopeTo, parsed.Subject, rec.Size)
		existing, err := s.Inbound.FindByDedupHash(ctx, d.ProjectID, rec.DedupHash)
		if err != nil {
			return nil, err
		}

		if existing != nil {
			return existing, ErrDuplicate
		}
	}

	for i, a := range parsed.Attachments {
		att := imodel.Attachment{
			Filename:    a.Filename,
			ContentType: a.ContentType,
			Size:        a.Size,
		}
		if s.Blob != nil {
			key := fmt.Sprintf("inbound/%s/%d_%s", rec.ID, i, blob.SanitizeFilename(a.Filename))
			if err := s.Blob.Put(ctx, key, bytes.NewReader(a.Content), a.ContentType); err == nil {
				att.StorageKey = key
			} else {
				s.Log.Warn("inbound: attachment offload failed, keeping inline",
					"id", rec.ID, "filename", a.Filename, "err", err)
			}
		}

		if att.StorageKey == "" {
			att.Content = base64.StdEncoding.EncodeToString(a.Content)
		}

		rec.Attachments = append(rec.Attachments, att)
	}

	if err := s.Inbound.Put(ctx, rec); err != nil {
		// The dedup indexes are UNIQUE, so this is where a race is
		// decided. The read above is the fast path and settles the
		// ordinary MTA retry, but two deliveries arriving together both
		// read nothing and both get here - and before migration 00073
		// both also inserted, which is a second webhook and, for a
		// bounce report, a second bounce row and a second suppression.
		//
		// Losing the race is not an error: the message IS ingested, by
		// the other attempt. So it answers exactly as the fast path
		// does, and the attachments this attempt already offloaded are
		// removed, since the surviving row names its own copies and
		// nothing else will ever name these.
		// Nothing names these objects now, whichever way the write
		// failed - the row that would have is not there.
		s.releaseAttachments(ctx, rec)

		if existing := s.duplicateOf(ctx, rec, err); existing != nil {
			return existing, ErrDuplicate
		}

		return nil, err
	}

	if s.Emit != nil {
		s.Emit(ctx, rec.ProjectID, webhookmodel.EventInboundReceived, rec.Sender, map[string]any{
			"id":          rec.ID,
			"domain":      d.Domain,
			"sender":      rec.Sender,
			"recipients":  rec.Recipients,
			"subject":     rec.Subject,
			"message_id":  rec.MessageID,
			"size":        rec.Size,
			"received_at": rec.ReceivedAt,
		})
	}

	metrics.InboundReceived.Inc()
	s.Log.Info("inbound: message received",
		"id", rec.ID, "project_id", rec.ProjectID, "domain", d.Domain,
		"sender", rec.Sender, "size", rec.Size)

	// After the message is safely stored: if it is a failure report
	// addressed to the project's bounce address, feed it into the
	// bounce pipeline. Errors here never fail the ingest - the report
	// itself is preserved above either way.
	s.processReport(ctx, rec, raw, conn.ReportScope)

	return rec, nil
}

// processReport turns a delivery status notification arriving at the
// MX into bounce records and suppressions.
//
// The parsing is this channel's business, the writing is not: it goes
// through bounce.Intake, which the SES webhook also uses, so the two
// untrusted channels cannot drift apart on the rules that protect the
// suppression list. See Intake for what those are.
//
// Attribution comes from the header in the RETURNED ORIGINAL HEADERS,
// not from where the report landed. A provider that owns the return
// path forwards its bounce copy to a mailbox on the platform's
// domain, so attributing by the receiving project would file every
// tenant's bounces against the operator - which is what the version
// before this did, and why they were silently discarded.
func (s *Service) processReport(ctx context.Context, rec *imodel.Email, raw []byte, scope string) {
	if s.Bounces == nil || s.Emails == nil {
		return
	}

	report, ok := dsn.Parse(raw)
	if !ok {
		// Ordinary mail. Stored like anything else, nothing more.
		return
	}

	out := bounce.Report{
		EmailID: report.OriginalHeaders[strings.ToLower(smtpclient.HeaderEmailID)],
		Source:  "dsn from " + rec.Sender,
	}
	for _, r := range report.Recipients {
		btype := bouncemodel.TypeSoft
		suppress := false
		switch {
		case r.Action == "complaint":
			btype, suppress = bouncemodel.TypeComplaint, true
		case r.Hard():
			btype, suppress = bouncemodel.TypeHard, true
		}

		reason := r.Diagnostic
		if reason == "" {
			reason = strings.TrimSpace("dsn: " + r.Action + " " + r.Status)
		}

		out.Recipients = append(out.Recipients, bounce.ReportedRecipient{
			Address: r.Address, Type: btype, Suppress: suppress, Reason: reason,
		})
	}

	intake := &bounce.Intake{
		Emails: s.Emails, Bounces: s.Bounces, Suppressions: s.Suppressions, Log: s.Log,
	}

	// Through Intake.Allow rather than by looking the message up
	// differently, because that hook is exactly this question already:
	// it is about the REPORTER's authority, not about what was
	// reported, and the SES receiver uses it to demand that a message
	// actually left through the server whose topic the notification
	// arrived on.
	if scope != "" {
		intake.Allow = func(sent *emailmodel.Email) (bool, string) {
			if sent.ProjectID == scope {
				return true, ""
			}

			return false, "the reporting node belongs to a different project than the message"
		}
	}

	intake.Record(ctx, out)
}

// duplicateOf answers the row that beat this one, when err is one of
// the dedup indexes refusing a second copy. Nil for any other error.
//
// The index is named rather than reading any 23505 as a duplicate. A
// unique violation on some other key is a real fault, and treating it as
// "already ingested" would answer 250 to a message that was never
// stored - the worst possible way to lose mail, since the sender is told
// it arrived.
//
// The lookup can be trusted to find it: Postgres raises 23505 only once
// the other transaction has COMMITTED (a conflicting insert BLOCKS while
// it is still open), and this runs outside a transaction of its own, so
// the follow-up read takes a fresh snapshot that includes it.
func (s *Service) duplicateOf(ctx context.Context, rec *imodel.Email, err error) *imodel.Email {
	var (
		existing *imodel.Email
		lerr     error
	)
	switch {
	case database.UniqueViolation(err, "idx_inbound_message_id"):
		existing, lerr = s.Inbound.FindByMessageID(ctx, rec.ProjectID, rec.MessageID)
	case database.UniqueViolation(err, "idx_inbound_dedup"):
		existing, lerr = s.Inbound.FindByDedupHash(ctx, rec.ProjectID, rec.DedupHash)
	default:
		return nil
	}

	if lerr != nil || existing == nil {
		// It refused a duplicate and then the row was not there. Report
		// the original failure instead of inventing an id: a temporary
		// failure has the sender retry, which is the safe direction.
		s.Log.Warn("inbound: dedup index refused a write but the winning row was not found",
			"project_id", rec.ProjectID, "message_id", rec.MessageID, "err", lerr)

		return nil
	}

	return existing
}

// releaseAttachments removes what this attempt offloaded, for a record
// that did not survive. Best effort and logged: a leftover object costs
// disk, and failing the ingest over it would lose the mail instead.
func (s *Service) releaseAttachments(ctx context.Context, rec *imodel.Email) {
	if s.Blob == nil {
		return
	}

	for _, a := range rec.Attachments {
		if a.StorageKey == "" {
			continue
		}

		if err := s.Blob.Delete(ctx, a.StorageKey); err != nil {
			s.Log.Warn("inbound: could not remove the attachment of a discarded record",
				"id", rec.ID, "key", a.StorageKey, "err", err)
		}
	}
}

// dedupHash fingerprints messages that carry no Message-ID so MTA
// retries do not create duplicates.
func dedupHash(sender string, recipients []string, subject string, size int64) string {
	h := sha256.Sum256(fmt.Appendf(nil, "%s|%s|%s|%d",
		strings.ToLower(sender), strings.ToLower(strings.Join(recipients, ",")), subject, size))

	return hex.EncodeToString(h[:])
}
