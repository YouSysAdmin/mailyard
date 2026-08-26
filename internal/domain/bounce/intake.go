// Mailyard, Copyright (c) 2021-2026 YouSysAdmin

package bounce

import (
	"context"
	"log/slog"
	"strings"

	"github.com/yousysadmin/mailyard/internal/core/safetext"
	"github.com/yousysadmin/mailyard/internal/core/smtpclient"
	"github.com/yousysadmin/mailyard/internal/domain/store"
	bmodel "github.com/yousysadmin/mailyard/internal/models/bounce"
	emailmodel "github.com/yousysadmin/mailyard/internal/models/email"
	supmodel "github.com/yousysadmin/mailyard/internal/models/suppression"
)

// Report is one piece of delivery feedback, in the shape both
// channels reduce to: a DSN arriving at the MX and an SES notification
// arriving over HTTPS.
//
// Neither channel says which project it belongs to and neither is
// asked to. EmailID does, because it came out of the message itself.
type Report struct {
	// EmailID is the value of smtpclient.HeaderEmailID recovered from
	// the returned original headers.
	EmailID string

	// Recipients is what the report is about.
	Recipients []ReportedRecipient

	// Source names the channel, for the log line only.
	Source string
}

// ReportedRecipient is one address a report speaks about.
type ReportedRecipient struct {
	Address string

	// Type is the bounce classification the channel already decided.
	Type string

	// Suppress is whether this outcome should stop future sending.
	// Soft failures and delays do not.
	Suppress bool

	// Reason is the human-readable diagnostic.
	Reason string
}

// maxReportRecipients caps one report. Real ones name a handful, a
// crafted one can name thousands, each costing a lookup and up to two
// writes inside whatever deadline the caller is holding.
const maxReportRecipients = 100

// maxReason bounds the diagnostic, which comes off the wire from an
// untrusted reporter and can be a megabyte of part body. The API
// ingest path caps reasons at the same length.
const maxReason = 1000

// Intake writes one report's bounces and suppressions.
//
// One function for both channels, so the rules that protect the
// suppression list cannot drift between them. Anyone on the internet
// can post a convincing DSN at the MX, and anyone with an AWS account
// can point an SNS topic at the webhook, so both are equally
// untrusted and both are held to the same two rules:
//
//  1. The id must name a real email row. It is a uuid, so it cannot
//     be guessed, and it is what decides the project - the report is
//     filed against whoever SENT the message, never against whoever
//     received the report. That distinction is the point: a provider
//     forwards its feedback to a mailbox on the platform's domain.
//  2. Each reported recipient must be one THAT MESSAGE went to.
//     Holding a valid id is not enough to suppress an arbitrary
//     address.
//
// Returns the number of recipients acted on, for the caller's log.
type Intake struct {
	Emails       store.EmailStore
	Bounces      store.BounceStore
	Suppressions store.SuppressionStore
	Log          *slog.Logger

	// Allow is an OPTIONAL extra condition on the message a report
	// claims to be about. Nil means no extra condition.
	//
	// The two rules below are universal and every channel gets them.
	// This one is for a channel that knows something more: the SES
	// receiver knows which SNS topic the notification arrived on, and
	// therefore which server - so it can require that the message
	// actually left through that server. A DSN arriving on our MX
	// knows nothing comparable and passes nil.
	//
	// It is a hook rather than a field on Report because the rule is
	// about the REPORTER's authority, not about what was reported.
	Allow func(sent *emailmodel.Email) (ok bool, why string)
}

// Record files one feedback report and returns how many bounces it
// produced. The single entry point for both untrusted channels.
func (i *Intake) Record(ctx context.Context, r Report) int {
	if i.Emails == nil || i.Bounces == nil {
		return 0
	}

	id := strings.TrimSpace(r.EmailID)
	if id == "" {
		// Either the reporter returned no original headers, or the
		// message predates the header. Logged rather than guessed at:
		// matching on the reported address alone would let anyone who
		// can reach the channel suppress an address by describing it.
		i.Log.Warn("bounce: report carries no sending id, not attributed", "source", r.Source)

		return 0
	}

	sent, err := i.Emails.GetAny(ctx, id)
	if err != nil {
		i.Log.Error("bounce: report email lookup failed", "email_id", id, "source", r.Source, "err", err)

		return 0
	}

	if sent == nil {
		i.Log.Warn("bounce: report names an unknown sending id, ignoring",
			"email_id", id, "source", r.Source)

		return 0
	}

	if i.Allow != nil {
		if ok, why := i.Allow(sent); !ok {
			i.Log.Warn("bounce: reporter is not entitled to report on this message, ignoring",
				"email_id", id, "source", r.Source, "reason", why)

			return 0
		}
	}

	recipients := r.Recipients
	if len(recipients) > maxReportRecipients {
		i.Log.Warn("bounce: report names too many recipients, truncating",
			"count", len(recipients), "cap", maxReportRecipients, "source", r.Source)
		recipients = recipients[:maxReportRecipients]
	}

	acted := 0
	for _, rec := range recipients {
		if !addressedTo(sent.Recipients, rec.Address) {
			i.Log.Warn("bounce: report names a recipient that message never went to, ignoring",
				"email_id", id, "recipient", rec.Address, "source", r.Source)
			continue
		}

		// Through safetext rather than a byte slice: a DSN diagnostic
		// cut mid-rune is invalid UTF-8, Postgres refuses the INSERT
		// with 22021, and the continue below then skips the bounce AND
		// the suppression - the dead mailbox keeps being mailed.
		reason := safetext.Clamp(rec.Reason, maxReason)

		if err := i.Bounces.Put(ctx, &bmodel.Bounce{
			ProjectID: sent.ProjectID,
			EmailID:   sent.ID,
			Recipient: rec.Address,
			Type:      rec.Type,
			Reason:    reason,
		}); err != nil {
			i.Log.Error("bounce: record failed", "recipient", safetext.MaskAddress(rec.Address), "err", err)
			continue
		}

		if rec.Suppress && i.Suppressions != nil {
			kind := supmodel.KindBounce
			if rec.Type == bmodel.TypeComplaint {
				kind = supmodel.KindComplaint
			}

			if err := i.Suppressions.Upsert(ctx, &supmodel.Suppression{
				ProjectID: sent.ProjectID,
				Email:     rec.Address,
				Kind:      kind,
				Reason:    reason,
			}); err != nil {
				i.Log.Error("bounce: suppression failed", "recipient", safetext.MaskAddress(rec.Address), "err", err)
				continue
			}
		}

		acted++
		i.Log.Info("bounce: report processed",
			"project_id", sent.ProjectID, "email_id", sent.ID, "recipient", rec.Address,
			"type", rec.Type, "suppressed", rec.Suppress, "source", r.Source)
	}

	return acted
}

// addressedTo reports whether the message actually went to addr. The
// stored recipients may carry display names, a report never does.
func addressedTo(recipients []string, addr string) bool {
	want := strings.ToLower(strings.TrimSpace(addr))
	for _, r := range recipients {
		if strings.EqualFold(strings.TrimSpace(smtpclient.EnvelopeAddress(r)), want) {
			return true
		}
	}

	return false
}
