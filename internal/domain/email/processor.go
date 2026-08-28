// Mailyard, Copyright (c) 2021-2026 YouSysAdmin

package email

import (
	"context"
	"crypto/tls"
	"encoding/base64"
	"errors"
	"fmt"
	"log/slog"
	"maps"
	"slices"
	"strings"

	"github.com/yousysadmin/mailyard/internal/core/blob"
	"github.com/yousysadmin/mailyard/internal/core/dkim"
	"github.com/yousysadmin/mailyard/internal/core/queue"
	"github.com/yousysadmin/mailyard/internal/core/render"
	"github.com/yousysadmin/mailyard/internal/core/safetext"
	"github.com/yousysadmin/mailyard/internal/core/smtpclient"
	"github.com/yousysadmin/mailyard/internal/core/transport"
	"github.com/yousysadmin/mailyard/internal/domain/store"
	bouncemodel "github.com/yousysadmin/mailyard/internal/models/bounce"
	emailmodel "github.com/yousysadmin/mailyard/internal/models/email"
	ssmodel "github.com/yousysadmin/mailyard/internal/models/smtpserver"
	supmodel "github.com/yousysadmin/mailyard/internal/models/suppression"
)

// Processor is the worker-side half of the pipeline: pick the SMTP
// server, build the MIME message, send, classify the failure.
// Implements core/queue.Processor. Constructed once in cli/serve.go.
// AutoSuppress mirrors sending.auto_suppress_on_reject.
type Processor struct {
	Store        *store.Store
	Log          *slog.Logger
	AutoSuppress bool

	// Blob rehydrates offloaded attachment content at send time.
	Blob blob.Store

	// BounceAddress is sending.bounce_address, the return path for
	// mail leaving through the SHARED POOL only - the one case where
	// the sending IPs belong to the platform, so a platform domain can
	// honestly authorize them. A project's own servers use the
	// project's address, and empty on either side means the From
	// address. See returnPathFor.
	BounceAddress string

	// AllowPrivateSMTP is sending.allow_private_smtp_targets: the
	// operator saying a project's server may sit on this network. It
	// clears the guard Server.Spec set from tenancy, and only that -
	// the shared pool and relay nodes never had it.
	AllowPrivateSMTP bool

	// RelayClient supplies the certificate a worker presents to a
	// relay node, and the authority to verify the node against. Nil
	// when relay nodes are not configured, in which case a node can
	// never be picked either.
	RelayClient RelayClientSource

	// send is the delivery leg. Nil means opening the provider the
	// candidate names, which is what production does - the seam exists
	// so the failover walk can be tested against scripted outcomes
	// instead of real listeners or a real API.

	// Pull hands a message to a relay node that cannot be dialled -
	// see PullAssigner. Nil where no node pulls, which is every
	// community installation: the candidate is then dialled as any
	// other server is.
	Pull PullAssigner

	send func(context.Context, transport.Spec, *smtpclient.Message) error
}

// PullAssigner is the seam to relay nodes in pull mode. Target says
// whether the candidate server is such a node and names it, Assign
// hands it the finished bytes. After Assign the row is the node's: the
// processor answers Handed and the node's report ends it.
type PullAssigner interface {
	Target(ctx context.Context, srv *ssmodel.Server) (nodeID string, ok bool, err error)
	Assign(ctx context.Context, e *emailmodel.Email, nodeID, serverID, envelopeFrom string, raw []byte) error
}

// RelayClientSource hands out the worker's client identity. An
// interface so this package does not import the control-plane domain,
// and so a test can supply a pair without a database.
type RelayClientSource interface {
	// WorkerTLS returns a config carrying our client certificate and
	// trusting the relay authority, with ServerName set to host.
	WorkerTLS(ctx context.Context, host string) (*tls.Config, error)
}

// nodeTLS answers how to reach this candidate.
//
// Nil for every ordinary server, which is what keeps the existing
// dial untouched. For a node it is mutual TLS: the node verifies our
// certificate, and we verify its, so there is no password on this hop
// at all - which is the whole reason a node can sit on somebody
// else's network.
func (p *Processor) nodeTLS(ctx context.Context, srv *ssmodel.Server) (*tls.Config, error) {
	if !srv.IsNode() {
		return nil, nil
	}

	if p.RelayClient == nil {
		// A node row exists but this process cannot present an
		// identity to it. Refusing is the only honest answer: dialling
		// without the certificate would fail at the handshake anyway,
		// and much less clearly.
		return nil, fmt.Errorf("server %s is a relay node but no client identity is configured", srv.Name)
	}

	return p.RelayClient.WorkerTLS(ctx, srv.Host)
}

// deliver hands the message to whichever provider this candidate names.
//
// The transport is opened per attempt rather than cached on the
// Processor, because a candidate list can mix providers and because an
// SES client is a struct around an HTTP client - cheap enough that
// keeping a pool of them would be caching to avoid an allocation while
// making a credential change need a restart.
func (p *Processor) deliver(ctx context.Context, spec transport.Spec, msg *smtpclient.Message) error {
	if p.send != nil {
		return p.send(ctx, spec, msg)
	}

	t, err := transport.Open(spec)
	if err != nil {
		return err
	}

	return t.Send(ctx, msg)
}

// Process delivers one claimed message and reports what the worker
// should do with it.
func (p *Processor) Process(ctx context.Context, e *emailmodel.Email) queue.Outcome {
	// Re-checked here and not merely at accept time: a scheduled send
	// waits in the queue for days, and the domain it claims can be
	// unverified or deleted while it waits. Permanent, like the
	// no-server case below - retrying cannot re-verify a domain, and
	// the operator can reset the row after fixing DNS.
	if err := RequireVerifiedSender(ctx, p.Store, e.ProjectID, senderAddress(e.Sender)); err != nil {
		return queue.Fail(err)
	}

	candidates, err := p.pickServers(ctx, e)
	if err != nil {
		return queue.Retry(fmt.Errorf("pick smtp server: %w", err))
	}

	if len(candidates) == 0 {
		// No server is a permanent condition for this email: retrying
		// cannot fix configuration. The operator retries manually
		// after adding a server.
		return queue.Fail(errors.New("no enabled smtp server accepts this sender"))
	}

	// Rehydrate offloaded attachments. A blob outage is transient -
	// retry rather than fail the email permanently.
	attachments := make([]emailmodel.Attachment, len(e.Attachments))
	copy(attachments, e.Attachments)
	for i := range attachments {
		a := &attachments[i]
		if a.Content != "" || a.StorageKey == "" {
			continue
		}

		raw, err := LoadAttachment(ctx, p.Blob, a)
		if err != nil {
			return queue.Retry(fmt.Errorf("load attachment %q: %w", a.Filename, err))
		}

		a.Content = base64.StdEncoding.EncodeToString(raw)
	}

	text := e.TextBody
	if text == "" && e.HTMLBody != "" {
		text = render.HTMLToText(e.HTMLBody)
	}

	// The client's own To, Cc and Reply-To, if the request stored
	// them, come out of the header map and into the fields the builder
	// writes them from - left in the map they would be written twice.
	headers := e.Headers
	headerTo, cc, replyTo := headers[HeaderDisplayTo], headers[HeaderDisplayCc], headers[HeaderReplyTo]
	if headerTo != "" || cc != "" || replyTo != "" {
		headers = maps.Clone(headers)
		delete(headers, HeaderDisplayTo)
		delete(headers, HeaderDisplayCc)
		delete(headers, HeaderReplyTo)
	}

	msg := &smtpclient.Message{
		From: e.Sender,
		// EnvelopeFrom is filled per candidate inside the failover
		// loop, not here - see returnPathFor.
		EmailID:               e.ID,
		To:                    e.Recipients,
		HeaderTo:              headerTo,
		Cc:                    cc,
		ReplyTo:               replyTo,
		Subject:               e.Subject,
		HTML:                  e.HTMLBody,
		Text:                  text,
		Attachments:           toClientAttachments(attachments),
		Headers:               headers,
		ListUnsubscribeURL:    e.ListUnsubscribeURL,
		ListUnsubscribeMailto: e.ListUnsubscribeMailto,
		ListUnsubscribePost:   e.ListUnsubscribePost,
	}

	// DKIM. A signer is attached only when the sender's domain is
	// verified to this project AND holds a key - and only when the
	// server it goes through wants one. Providers that rewrite
	// Message-ID and Date and re-sign the result (Amazon SES) break
	// any signature applied here, so a server flagged skip_dkim sends
	// unsigned on purpose and lets the provider's own signature carry
	// DMARC.
	//
	// The signer depends only on the sender's domain, so it is
	// resolved once - but WHETHER to attach it is decided per server
	// inside the failover loop. Two servers in a group can disagree
	// about skip_dkim, and failing over from a signing server to an
	// SES-style one with the signature still attached would deliver
	// exactly the broken signature the flag exists to prevent.
	var signer *dkim.Signer
	if slices.ContainsFunc(candidates, func(s *ssmodel.Server) bool { return !s.SkipsDKIM() }) {
		signer, err = p.signerFor(ctx, e)
		if err != nil {
			// Retry rather than send unsigned. A domain that has a key and
			// cannot use it right now is a transient store or crypto
			// problem, and quietly downgrading to unsigned would land the
			// message in a spam folder with nothing in the log to say why.
			return queue.Retry(fmt.Errorf("dkim signer: %w", err))
		}
	}

	// Failover: walk the candidates until one takes the message.
	//
	// A transient failure is about the server - it would not answer,
	// it was busy, it timed out - so the next one in the group is
	// worth a try, and the whole walk costs the message one attempt.
	// A permanent 5xx is about the message, and stops here: offering a
	// rejected recipient to every server in turn would earn the same
	// refusal from each, take that long to conclude, and run
	// recordRejection once per server, writing a pile of bounce rows
	// for what is one bounce.
	var sendErr error
	for i, candidate := range candidates {
		if i > 0 {
			p.Log.Warn("email: smtp server failed, trying the next in the group",
				"email_id", e.ID, "failed_server", candidates[i-1].Name,
				"next_server", candidate.Name, "err", sendErr)
		}

		srv := candidate
		// Per server, for the reason above.
		msg.Sign = nil
		if signer != nil && !srv.SkipsDKIM() {
			msg.Sign = signer.Sign
		}

		// And so is the return path, for the same shape of reason: the
		// two servers in a group can be the project's own relay and
		// the platform pool, and they need different envelope senders
		// because a receiver checks the return path's SPF against
		// whichever IP connected. Carrying one server's return path
		// onto another's IP is a guaranteed SPF failure.
		msg.EnvelopeFrom = p.returnPathFor(ctx, e, srv)

		// A relay node that PULLS is not dialled at all. The finished
		// bytes - signed, return path set, for THIS candidate - are
		// handed over, and the node's report is what ends the row. A
		// failed hand-over is a failed candidate like any other and
		// the walk goes on.
		if p.Pull != nil && srv.IsNode() {
			nodeID, pulls, perr := p.Pull.Target(ctx, srv)
			if perr != nil {
				sendErr = perr
				continue
			}

			if pulls {
				raw, rerr := transport.RawMessage(msg)
				if rerr != nil {
					return queue.Retry(rerr)
				}

				if aerr := p.Pull.Assign(ctx, e, nodeID, srv.ID, msg.EnvelopeFrom, raw); aerr != nil {
					sendErr = aerr
					continue
				}

				return queue.Handed()
			}
		}

		// A relay node is dialled differently: mutual TLS with our own
		// certificate authority and no AUTH at all. Resolved per
		// candidate, beside msg.Sign and the return path, for the same
		// reason as those - failing over from a node to an ordinary
		// server must not carry the node's transport with it.
		nodeTLS, tlsErr := p.nodeTLS(ctx, srv)
		if tlsErr != nil {
			// Cannot reach this node securely. Treat it like any other
			// unusable candidate and try the next one rather than
			// failing the message outright.
			p.Log.Error("email: could not build the relay node transport",
				"email_id", e.ID, "server", srv.Name, "err", tlsErr)
			sendErr = tlsErr
			continue
		}

		spec := srv.Spec(nodeTLS)
		if p.AllowPrivateSMTP {
			spec.GuardPrivate = false
		}

		sendErr = p.deliver(ctx, spec, msg)
		if sendErr == nil {
			if i > 0 {
				p.Log.Info("email: delivered after failover",
					"email_id", e.ID, "server", srv.Name, "tried", i+1)
			}

			// The winner, not the pinned server. After a failover walk
			// they differ, and this is the only place that knows.
			return queue.DoneVia(srv.ID)
		}

		// Classified by the provider, through one interface. Asserting
		// *smtpclient.SendError here and asking about an SMTP stage name
		// inside recordRejection leaves a provider that is not SMTP
		// unable to say "permanent" at all, with its refusals retried
		// until the queue gives up.
		if f, ok := errors.AsType[transport.Failure](sendErr); ok && f.Permanent() {
			// Permanent rejection: record the bounce and, when the
			// provider named the recipient, suppress the address so
			// future sends skip it.
			p.recordRejection(ctx, e, f)

			return queue.Fail(sendErr)
		}
	}

	return queue.Retry(sendErr)
}

// recordRejection writes the bounce row and (optionally) the
// suppression for a permanent rejection. Failures here are logged,
// never fatal - the email row's failed status is the source of truth.
func (p *Processor) recordRejection(ctx context.Context, e *emailmodel.Email, f transport.Failure) {
	// Only a rejection that names a recipient acts on one. Whether it
	// does is the provider's answer, not ours: SMTP names one at RCPT TO
	// and holds the SENDER in the same field at MAIL FROM, and SES never
	// names one at all. Getting that wrong suppresses the project's own
	// sender or bounce address, and the suppression list doubles as an
	// inbound blocklist - so the mistake would not stay on the sending
	// side.
	recipient := f.RejectedRecipient()
	if recipient == "" {
		return
	}

	if err := p.Store.Bounce.Put(ctx, &bouncemodel.Bounce{
		ProjectID: e.ProjectID,
		EmailID:   e.ID,
		Recipient: recipient,
		Type:      bouncemodel.TypeHard,
		Reason:    f.Error(),
	}); err != nil {
		p.Log.Error("email: record bounce", "email_id", e.ID, "err", err)
	}

	if !p.AutoSuppress {
		return
	}

	if err := p.Store.Suppression.Upsert(ctx, &supmodel.Suppression{
		ProjectID: e.ProjectID,
		Email:     recipient,
		Kind:      supmodel.KindBounce,
		Reason:    f.Error(),
	}); err != nil {
		p.Log.Error("email: auto suppress", "email_id", e.ID, "err", err)
	} else {
		p.Log.Info("email: recipient auto-suppressed", "email_id", e.ID, "recipient", safetext.MaskAddress(recipient))
	}
}

// pickServers resolves every server this message may go out through,
// in the order to try them. See ResolveCandidates.
func (p *Processor) pickServers(ctx context.Context, e *emailmodel.Email) ([]*ssmodel.Server, error) {
	return ResolveCandidates(ctx, p.Store, e.ProjectID, senderAddress(e.Sender),
		Route{ServerID: e.SMTPServerID, GroupID: e.SMTPGroupID})
}

// pickServer is the single-answer form, kept for the tests that pin
// resolution order without going through a delivery.
func (p *Processor) pickServer(ctx context.Context, e *emailmodel.Email) (*ssmodel.Server, error) {
	candidates, err := p.pickServers(ctx, e)
	if err != nil || len(candidates) == 0 {
		return nil, err
	}

	return candidates[0], nil
}

// returnPathFor answers where delivery reports go, decided by the
// server carrying the message.
//
// A receiver checks SPF for the return path's domain against the IP
// that connected, so the address only works on a domain that
// authorizes that IP - which is whoever owns the server. A shared-pool
// row (no ProjectID) takes sending.bounce_address, a project's own
// server takes the project's, and neither borrows the other's.
//
// NEVER EMPTY for a message somebody sent. Where the owner configured
// no address the From address is the envelope, which is the aligned
// default a receiver expects. An empty string means a NULL sender to
// every transport downstream, and a pull node honours that literally
// - it has no From to fall back on, so the message left as
// MAIL FROM:<> and no report on it could ever come back.
//
// SES owns the envelope and discards this. Nothing here detects that,
// because the value is simply unused on that path.
func (p *Processor) returnPathFor(ctx context.Context, e *emailmodel.Email, srv *ssmodel.Server) string {
	if srv != nil && srv.ProjectID == "" {
		return orSender(p.BounceAddress, e)
	}

	w, err := p.Store.Project.Get(ctx, e.ProjectID)
	if err != nil {
		// Degrade to the aligned default rather than fail the send.
		// A missing return path costs bounce reporting, a failed send
		// costs the message.
		p.Log.Warn("email: return path lookup failed", "project_id", e.ProjectID, "err", err)

		return orSender("", e)
	}

	if w == nil {
		return orSender("", e)
	}

	return orSender(w.BounceAddress, e)
}

// orSender is the configured return path, or the From address when
// there is none.
func orSender(configured string, e *emailmodel.Email) string {
	if configured != "" {
		return configured
	}

	return smtpclient.EnvelopeAddress(e.Sender)
}

// signerFor returns the DKIM signer for the sender domain, or nil when
// the message goes out unsigned.
//
// nil is ordinary: a project may send from any address its server
// accepts, but only from domains it has proven it owns can it sign.
// An error means "should be signed but cannot", which the caller
// retries rather than sending unsigned.
func (p *Processor) signerFor(ctx context.Context, e *emailmodel.Email) (*dkim.Signer, error) {
	host := senderDomain(e.Sender)
	if host == "" {
		return nil, nil
	}

	d, err := p.Store.Domain.GetVerifiedCovering(ctx, host)
	if err != nil {
		return nil, err
	}

	// Not claimed, claimed by a different project, or no key yet.
	// The project check matters: domain names are globally unique
	// here, so without it one tenant could sign as another tenant's
	// verified domain simply by putting it in From.
	if d == nil || d.ProjectID != e.ProjectID || !d.CanSign() {
		return nil, nil
	}

	return dkim.NewSigner(d.Domain, d.DKIMSelector, d.DKIMPrivateKey)
}

// senderDomain is the lowercase host part of an RFC 5322 address.
func senderDomain(from string) string {
	_, host, ok := strings.CutLast(smtpclient.EnvelopeAddress(from), "@")
	if !ok || host == "" {
		return ""
	}

	return strings.ToLower(host)
}
