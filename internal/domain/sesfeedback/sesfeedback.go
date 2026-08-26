// Mailyard, Copyright (c) 2021-2026 YouSysAdmin

// Package sesfeedback receives Amazon SES bounce and complaint
// notifications over SNS.
//
// SES exists outside the return-path model entirely. It replaces the
// envelope sender with its own so the receiver's bounce comes back to
// Amazon, which means no address we could put in MAIL FROM would ever
// see one. Feedback arrives here instead, over HTTPS.
//
// What does not change is attribution: the notification carries the
// original headers, so smtpclient.HeaderEmailID names the message and
// bounce.Intake writes the result under exactly the rules the DSN
// path uses.
package sesfeedback

import (
	"context"
	"encoding/json/v2"
	"log/slog"
	"net/http"
	"strings"
	"time"

	"github.com/gofiber/fiber/v3"

	"github.com/yousysadmin/mailyard/internal/core/clientip"
	"github.com/yousysadmin/mailyard/internal/core/env"
	"github.com/yousysadmin/mailyard/internal/core/safedial"
	"github.com/yousysadmin/mailyard/internal/core/sestopics"
	"github.com/yousysadmin/mailyard/internal/core/smtpclient"
	"github.com/yousysadmin/mailyard/internal/core/snsmsg"
	"github.com/yousysadmin/mailyard/internal/domain/bounce"
	bmodel "github.com/yousysadmin/mailyard/internal/models/bounce"
	emailmodel "github.com/yousysadmin/mailyard/internal/models/email"
)

// Handler owns POST /webhooks/ses. Public by necessity - SNS has no
// session and no API key to present - so authentication is the SNS
// signature plus the topic allowlist, and nothing else.
type Handler struct {
	Runtime   *env.Runtime
	Allowlist *sestopics.Allowlist
	verifier  *snsmsg.Verifier
}

// New builds the handler with an SSRF-guarded client for fetching
// signing certificates.
func New(rt *env.Runtime, allow *sestopics.Allowlist) *Handler {
	return &Handler{
		Runtime:   rt,
		Allowlist: allow,
		verifier: &snsmsg.Verifier{
			HTTP:   safedial.Client(10*time.Second, false),
			MaxAge: time.Hour,
		},
	}
}

// maxBody bounds one notification. SES notifications with original
// headers run to a few kilobytes.
const maxBody = 1 << 20

// Receive handles every SNS delivery: subscription confirmations and
// notifications alike.
//
// It answers 200 to anything it accepts AND to anything it decides to
// drop, because SNS retries a non-2xx for hours and there is nothing
// on the other end that fixing a retry would help. A refusal is a log
// line, not a status code. The one exception is a body we could not
// authenticate, which is answered 403 so a misconfigured topic is
// visible in the SNS delivery status rather than silently succeeding.
func (h *Handler) Receive(c fiber.Ctx) error {
	body := c.Body()
	if len(body) > maxBody {
		h.log().Warn("ses: notification too large, ignoring", "bytes", len(body))

		return c.SendStatus(fiber.StatusOK)
	}

	msg, err := snsmsg.Parse(body)
	if err != nil {
		h.log().Warn("ses: unparseable sns delivery", "err", err, "client_ip", clientip.From(c))

		return c.SendStatus(fiber.StatusBadRequest)
	}

	// The allowlist, not the signature, is what makes this ours. A
	// valid signature only proves SOME AWS account sent it, and anyone
	// can make an account and point a topic here.
	allowed, aerr := h.Allowlist.Allowed(c.Context(), msg.TopicARN)
	if aerr != nil {
		// Could not answer the authorization question. Refusing is the
		// only honest response - and 503 rather than 403, because SNS
		// retries a 5xx and this one is our fault.
		h.log().Error("ses: could not read the topic allowlist", "err", aerr)

		return c.SendStatus(fiber.StatusServiceUnavailable)
	}

	if !allowed {
		// Debug, not Warn. The endpoint is public and always
		// registered, so anyone who finds the URL could otherwise fill
		// the log by posting to it. A valid topic with a bad signature
		// stays a Warn below - that one is genuinely an event.
		h.log().Debug("ses: notification from an unlisted topic, refusing",
			"topic", msg.TopicARN, "client_ip", clientip.From(c))

		return c.SendStatus(fiber.StatusForbidden)
	}

	if err := h.verifier.Verify(msg); err != nil {
		h.log().Warn("ses: notification failed signature verification",
			"topic", msg.TopicARN, "client_ip", clientip.From(c), "err", err)

		return c.SendStatus(fiber.StatusForbidden)
	}

	switch msg.Type {
	case snsmsg.TypeSubscriptionConfirmation:
		h.confirm(msg)

		return c.SendStatus(fiber.StatusOK)
	case snsmsg.TypeUnsubscribeConfirmation:
		h.log().Warn("ses: topic unsubscribed, feedback will stop arriving", "topic", msg.TopicARN)

		return c.SendStatus(fiber.StatusOK)
	}

	h.record(c.Context(), msg.TopicARN, msg.Message)

	return c.SendStatus(fiber.StatusOK)
}

// confirm completes the subscription by fetching the SubscribeURL.
//
// Only for an allowlisted topic, which Receive has already checked.
// Without that anyone could subscribe this endpoint to a topic of
// their own and have it confirm itself.
func (h *Handler) confirm(msg *snsmsg.Message) {
	resp, err := h.verifier.HTTP.Get(msg.SubscribeURL)
	if err != nil {
		h.log().Error("ses: confirming the sns subscription failed",
			"topic", msg.TopicARN, "err", err)

		return
	}

	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode != http.StatusOK {
		h.log().Error("ses: confirming the sns subscription returned an error",
			"topic", msg.TopicARN, "status", resp.StatusCode)

		return
	}

	h.log().Info("ses: sns subscription confirmed", "topic", msg.TopicARN)
}

// sesNotification is the payload inside the SNS Message string. Only
// the fields that decide something are named.
type sesNotification struct {
	NotificationType string `json:"notificationType"`

	// The same field under a different name when the operator wired
	// this through a configuration set event destination instead of
	// identity notifications. The docs point at identity notifications
	// because they are simpler, but a config set posts to a topic just
	// as well and only this one key differs - without the alias that
	// setup would authenticate, parse, and quietly record nothing.
	EventType string `json:"eventType"`
	Mail      struct {
		MessageID string `json:"messageId"`
		Headers   []struct {
			Name  string `json:"name"`
			Value string `json:"value"`
		} `json:"headers"`
		HeadersTruncated bool `json:"headersTruncated"`
	} `json:"mail"`
	Bounce struct {
		BounceType        string `json:"bounceType"`
		BounceSubType     string `json:"bounceSubType"`
		BouncedRecipients []struct {
			EmailAddress   string `json:"emailAddress"`
			Status         string `json:"status"`
			DiagnosticCode string `json:"diagnosticCode"`
		} `json:"bouncedRecipients"`
	} `json:"bounce"`
	Complaint struct {
		ComplainedRecipients []struct {
			EmailAddress string `json:"emailAddress"`
		} `json:"complainedRecipients"`
		ComplaintFeedbackType string `json:"complaintFeedbackType"`
	} `json:"complaint"`
}

// record turns one SES notification payload into bounce rows.
//
// Takes the payload rather than the SNS envelope, and a plain context
// rather than the request, because by this point the transport has
// done its job and everything left is about what the message SAYS.
func (h *Handler) record(ctx context.Context, topicARN, payload string) {
	var n sesNotification
	if err := json.Unmarshal([]byte(payload), &n); err != nil {
		h.log().Warn("ses: notification payload is not json", "err", err)

		return
	}

	kind := n.NotificationType
	if kind == "" {
		kind = n.EventType
	}

	report := bounce.Report{
		EmailID: headerValue(n.Mail.Headers, smtpclient.HeaderEmailID),
		Source:  "ses " + strings.ToLower(kind),
	}
	if report.EmailID == "" && n.Mail.HeadersTruncated {
		// Worth saying separately: the operator turned original
		// headers on and SES cut them anyway, which is a different fix
		// from having left them off. SES truncates once the original
		// headers reach 10 KB, so the message itself is the problem.
		h.log().Warn("ses: original headers were truncated at 10 kb, notification not attributed",
			"ses_message_id", n.Mail.MessageID)
	}

	switch kind {
	case "Bounce":
		// Permanent is the only kind that means the address is gone.
		// Transient and Undetermined are recorded and do not suppress:
		// a full mailbox is not a dead one.
		hard := n.Bounce.BounceType == "Permanent"
		btype := bmodel.TypeSoft
		if hard {
			btype = bmodel.TypeHard
		}

		for _, r := range n.Bounce.BouncedRecipients {
			reason := r.DiagnosticCode
			if reason == "" {
				reason = strings.TrimSpace("ses: " + n.Bounce.BounceType + " " + n.Bounce.BounceSubType + " " + r.Status)
			}

			report.Recipients = append(report.Recipients, bounce.ReportedRecipient{
				Address: r.EmailAddress, Type: btype, Suppress: hard, Reason: reason,
			})
		}
	case "Complaint":
		for _, r := range n.Complaint.ComplainedRecipients {
			reason := "ses: complaint"
			if n.Complaint.ComplaintFeedbackType != "" {
				reason = "ses: complaint " + n.Complaint.ComplaintFeedbackType
			}

			report.Recipients = append(report.Recipients, bounce.ReportedRecipient{
				Address: r.EmailAddress, Type: bmodel.TypeComplaint, Suppress: true, Reason: reason,
			})
		}
	default:
		// Delivery and the rest carry no failure to record. Not an
		// error, just nothing to do.
		return
	}

	if len(report.Recipients) == 0 {
		return
	}

	// The topic decides which server, and the server decides which
	// project. A notification may only concern a message that actually
	// left through a server publishing to this topic.
	//
	// Attribution already comes from the header, so this is not how a
	// bounce finds its message - it is what stops one tenant's topic
	// reporting about another tenant's mail. Uuids made that a weak
	// hole rather than an open door, but weak is not closed.
	intake := &bounce.Intake{
		Emails:       h.Runtime.Store.Email,
		Bounces:      h.Runtime.Store.Bounce,
		Suppressions: h.Runtime.Store.Suppression,
		Log:          h.log(),
		Allow:        h.deliveredViaTopic(ctx, topicARN),
	}
	intake.Record(ctx, report)
}

// headerValue finds a header in the SES header list, case insensitive
// because the list preserves whatever casing the message used.
func headerValue(headers []struct {
	Name  string `json:"name"`
	Value string `json:"value"`
}, name string) string {
	for _, h := range headers {
		if strings.EqualFold(h.Name, name) {
			return strings.TrimSpace(h.Value)
		}
	}

	return ""
}

// deliveredViaTopic builds the entitlement check for one topic.
//
// A notification from topic T may only concern a message that left
// through a server publishing to T. That is what turns "some AWS
// account we trust" into "the account that actually sent this".
//
// A message with no recorded delivering server is refused rather than
// waved through - one predating this column, or one that never left.
// Neither is something an SES topic can have observed.
func (h *Handler) deliveredViaTopic(ctx context.Context, topicARN string) func(*emailmodel.Email) (bool, string) {
	return func(sent *emailmodel.Email) (bool, string) {
		if sent.DeliveredVia == "" {
			return false, "the message has no recorded delivering server"
		}

		arn, err := h.topicOfServer(ctx, sent.DeliveredVia)
		if err != nil {
			h.log().Error("ses: could not resolve the delivering server", "err", err)

			return false, "could not resolve the delivering server"
		}

		if arn != topicARN {
			return false, "the message did not leave through a server publishing to this topic"
		}

		return true, ""
	}
}

// topicOfServer finds the SES topic configured on a delivery row.
// Which table it lives in is not known here, so both are asked - a
// shared server and a project server are the same thing to SES.
func (h *Handler) topicOfServer(ctx context.Context, serverID string) (string, error) {
	if srv, err := h.Runtime.Store.SharedSMTP.Get(ctx, serverID); err != nil {
		return "", err
	} else if srv != nil {
		return srv.SESTopicARN, nil
	}

	srv, err := h.Runtime.Store.SMTPServer.GetAny(ctx, serverID)
	if err != nil || srv == nil {
		return "", err
	}

	return srv.SESTopicARN, nil
}

func (h *Handler) log() *slog.Logger { return h.Runtime.Log }
