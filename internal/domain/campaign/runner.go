// Mailyard, Copyright (c) 2021-2026 YouSysAdmin

package campaign

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"maps"
	"math/rand/v2"
	"sync"
	"sync/atomic"
	"time"

	"github.com/yousysadmin/mailyard/internal/core/ids"
	"github.com/yousysadmin/mailyard/internal/core/smtpclient"

	"github.com/yousysadmin/mailyard/internal/core/quota"
	"github.com/yousysadmin/mailyard/internal/core/render"
	"github.com/yousysadmin/mailyard/internal/core/safego"
	"github.com/yousysadmin/mailyard/internal/core/tracking"
	"github.com/yousysadmin/mailyard/internal/domain/email"
	"github.com/yousysadmin/mailyard/internal/domain/store"
	cmodel "github.com/yousysadmin/mailyard/internal/models/campaign"
	emailmodel "github.com/yousysadmin/mailyard/internal/models/email"
	submodel "github.com/yousysadmin/mailyard/internal/models/subscriber"
	whmodel "github.com/yousysadmin/mailyard/internal/models/webhook"
)

// claimLease bounds how long a crashed batch blocks its campaign.
const claimLease = 2 * time.Minute

// Runner drains sending campaigns: fan-out on first claim, then
// throttled batches of pending messages rendered per subscriber and
// handed to the email service. One goroutine, started from serve.go.
type Runner struct {
	Store        *store.Store
	EmailService *email.Service
	Log          *slog.Logger

	// Emit fans campaign lifecycle events to webhooks (nil-safe).
	Emit func(ctx context.Context, projID, event, sender string, payload any)

	// Tracking signs pixel / click / unsubscribe / web view URLs.
	// When disabled (no public_url) campaigns send untracked.
	Tracking *tracking.Signer

	// BatchSize caps messages per batch, PollInterval paces the loop.
	BatchSize    int
	PollInterval time.Duration

	// Broadcast, when set, carries a wake to the other nodes. Set it
	// before Start. See Wake.
	Broadcast func()

	wake    chan struct{}
	stop    chan struct{}
	done    chan struct{}
	once    sync.Once
	started atomic.Bool
}

// NewRunner builds a Runner.
func NewRunner(st *store.Store, svc *email.Service, log *slog.Logger,
	emit func(ctx context.Context, projID, event, sender string, payload any),
	signer *tracking.Signer, batchSize int, pollInterval time.Duration) *Runner {
	if emit == nil {
		emit = func(context.Context, string, string, string, any) {}
	}

	if signer == nil {
		signer = tracking.NewSigner("", "")
	}

	return &Runner{
		Store:        st,
		EmailService: svc,
		Log:          log,
		Emit:         emit,
		Tracking:     signer,
		BatchSize:    batchSize,
		PollInterval: pollInterval,
		wake:         make(chan struct{}, 1),
		stop:         make(chan struct{}),
		done:         make(chan struct{}),
	}
}

// Wake nudges every node (campaign started or resumed).
// Non-blocking. The console node that starts a campaign is usually
// not the node that will run it.
func (r *Runner) Wake() {
	r.WakeLocal()
	if r.Broadcast != nil {
		r.Broadcast()
	}
}

// WakeLocal nudges this process only. The cross-node listener calls
// this - Wake would rebroadcast every notification it received and
// the nudge would circle the cluster forever.
func (r *Runner) WakeLocal() {
	select {
	case r.wake <- struct{}{}:
	default:
	}
}

// Start blocks in the poll loop until Stop or ctx cancellation. Run
// it in a goroutine.
func (r *Runner) Start(ctx context.Context) {
	r.started.Store(true)
	defer close(r.done)
	r.Log.Info("campaign: runner started",
		"batch_size", r.BatchSize, "poll_interval", r.PollInterval.String())
	ticker := time.NewTicker(r.PollInterval)
	defer ticker.Stop()
	for {
		// Guard each poll rather than the loop: a panic on one batch
		// must cost that batch, not the runner. Without this the whole
		// process dies and every campaign stalls.
		func() {
			defer safego.Recover(r.Log, "campaign: poll")
			r.pollOnce(ctx)
		}()
		select {
		case <-ticker.C:
		case <-r.wake:
		case <-r.stop:
			return
		case <-ctx.Done():
			return
		}
	}
}

// Stop halts the loop after the current batch finishes.
//
// Safe on a runner that was never started. An api-role node builds
// one (the console needs Wake to reach the workers) without running
// its loop, and done is closed by Start - so waiting on it there
// would spend the whole timeout, on every shutdown, achieving
// nothing.
func (r *Runner) Stop(timeout time.Duration) {
	r.once.Do(func() { close(r.stop) })
	if !r.started.Load() {
		return
	}

	select {
	case <-r.done:
		r.Log.Info("campaign: runner stopped")
	case <-time.After(timeout):
		r.Log.Warn("campaign: runner stop timed out")
	}
}

// pollOnce promotes due scheduled campaigns, then processes batches
// until no campaign is due.
func (r *Runner) pollOnce(ctx context.Context) {
	now := time.Now().UTC()
	if n, err := r.Store.Campaign.PromoteScheduled(ctx, now); err != nil {
		r.Log.Error("campaign: promote scheduled", "err", err)
	} else if n > 0 {
		r.Log.Info("campaign: scheduled campaigns started", "count", n)
	}

	for {
		select {
		case <-r.stop:
			return
		case <-ctx.Done():
			return
		default:
		}

		c, err := r.Store.Campaign.ClaimDue(ctx, time.Now().UTC(), claimLease)
		if err != nil {
			r.Log.Error("campaign: claim", "err", err)

			return
		}

		if c == nil {
			return
		}

		r.processBatch(ctx, c)
	}
}

// processBatch runs one unit of work for a claimed campaign: fan-out
// when messages do not exist yet, else one throttled batch.
func (r *Runner) processBatch(ctx context.Context, c *cmodel.Campaign) {
	fanned, err := r.Store.Campaign.HasMessages(ctx, c.ID)
	if err != nil {
		r.Log.Error("campaign: has messages", "campaign_id", c.ID, "err", err)

		return
	}

	if !fanned {
		if err := r.fanOut(ctx, c); err != nil {
			r.Log.Error("campaign: fan out", "campaign_id", c.ID, "err", err)

			// Leave the lease in place: the next claim retries after
			// claimLease instead of hot-looping a broken campaign.
			return
		}
	}

	now := time.Now().UTC()
	batch, err := r.Store.Campaign.PendingDue(ctx, c.ID, now, r.BatchSize)
	if err != nil {
		r.Log.Error("campaign: pending due", "campaign_id", c.ID, "err", err)

		return
	}

	quotaPaused := false
	if len(batch) > 0 {
		quotaPaused = r.deliverBatch(ctx, c, batch)
	}

	remaining, err := r.Store.Campaign.CountPending(ctx, c.ID)
	if err != nil {
		r.Log.Error("campaign: count pending", "campaign_id", c.ID, "err", err)

		return
	}

	if remaining == 0 {
		r.complete(ctx, c)

		return
	}

	// Throttle: SendRate spaces the batches, local-time scheduling
	// parks the loop until the next message is due.
	next := time.Now().UTC()
	if c.SendRate > 0 && len(batch) > 0 {
		next = next.Add(time.Duration(float64(len(batch)) / float64(c.SendRate) * float64(time.Minute)))
	}

	if len(batch) == 0 {
		if nd, err := r.Store.Campaign.NextDeliverAt(ctx, c.ID); err == nil && nd != nil && nd.After(next) {
			next = *nd
		} else {
			next = next.Add(r.PollInterval)
		}
	}

	// A quota pause means try again once the window has moved, and it
	// needs a floor of its own: with send_rate 0 next is still NOW, so
	// pollOnce would re-claim this campaign in the same pass and spin
	// at full speed - claim, pending read, subscriber read, render,
	// quota check - until the window rolled, up to an hour of pegging
	// the node and the database over one oversized campaign.
	if quotaPaused {
		if floor := time.Now().UTC().Add(r.PollInterval); next.Before(floor) {
			next = floor
		}
	}

	// Only while it is still sending. An operator who paused or cancelled
	// during the batch above has already written that, and without the
	// guard this overwrites it and carries on.
	moved, err := r.Store.Campaign.SetRunState(ctx, c.ID, cmodel.StatusSending, nil, nil, &next, cmodel.StatusSending)
	if err != nil {
		r.Log.Error("campaign: set run state", "campaign_id", c.ID, "err", err)

		return
	}

	if !moved {
		r.Log.Info("campaign: left alone, no longer sending", "campaign_id", c.ID)
	}
}

// fanOut materializes the audience into campaign_messages, assigning
// A/B variants and per-subscriber local delivery times.
func (r *Runner) fanOut(ctx context.Context, c *cmodel.Campaign) error {
	list, err := r.Store.SubscriberList.Get(ctx, c.ProjectID, c.ListID)
	if err != nil {
		return err
	}

	if list == nil {
		return fmt.Errorf("subscriber list %s not found", c.ListID)
	}

	recipients, err := r.Store.SubscriberList.ResolveRecipients(ctx, r.Store.Subscriber, c.ProjectID, list)
	if err != nil {
		return err
	}

	msgs := make([]*cmodel.Message, 0, len(recipients))
	for _, sub := range recipients {
		m := &cmodel.Message{
			CampaignID:   c.ID,
			SubscriberID: sub.ID,
			Status:       cmodel.MsgPending,
		}
		if c.SendAtLocalTime {
			m.DeliverAt = localDeliverAt(c, sub)
		}

		msgs = append(msgs, m)
	}

	assignVariants(c, msgs)

	if len(msgs) > 0 {
		if err := r.Store.Campaign.BulkCreateMessages(ctx, msgs); err != nil {
			return err
		}
	}

	r.Log.Info("campaign: fanned out", "campaign_id", c.ID, "recipients", len(msgs))
	r.Emit(ctx, c.ProjectID, whmodel.EventCampaignStarted, c.FromEmail, eventPayload(c, map[string]int{"recipients": len(msgs)}))

	return nil
}

// deliverBatch renders and queues one batch of messages. It reports
// whether it stopped on a quota rejection, which processBatch turns
// into a delay - a quota pause with no delay is an immediate re-claim.
func (r *Runner) deliverBatch(ctx context.Context, c *cmodel.Campaign, batch []*cmodel.Message) (quotaPaused bool) {
	queued, failed := 0, 0
	for _, m := range batch {
		select {
		case <-r.stop:
			return false
		case <-ctx.Done():
			return false
		default:
		}

		// Has the operator stopped this since the batch was claimed?
		//
		// Shutdown and context were the only two things this loop
		// watched, so Pause and Cancel did not reach it: the whole batch
		// went out and only the NEXT one noticed. A batch is bounded by
		// batchSize and spaced by SendRate, so "cancelled" could mean
		// hundreds more messages, minutes apart, after the button.
		//
		// One indexed read per message, against several queries plus a
		// render and an insert in deliverMessage - and it decides whether
		// this recipient is mailed at all, which is not a thing to batch
		// up and check later.
		//
		// A read error is not treated as a stop: the campaign is sending
		// as far as anything here knows, and refusing to send on a
		// transient blip would strand the batch.
		if status, err := r.Store.Campaign.Status(ctx, c.ProjectID, c.ID); err != nil {
			r.Log.Error("campaign: read status", "campaign_id", c.ID, "err", err)
		} else if status != cmodel.StatusSending {
			r.Log.Info("campaign: batch stopped early",
				"campaign_id", c.ID, "status", status, "queued", queued, "remaining", len(batch)-queued-failed)

			return false
		}

		if err := r.deliverMessage(ctx, c, m); err != nil {
			// A quota rejection is a WAIT, not a failure.
			//
			// quota.Error is the type the HTTP surface answers 429 with
			// and the relay answers 452 with, both meaning try again when
			// the window rolls. Here it was marked MsgFailed like any
			// other error, permanently - so a campaign larger than the
			// plan's hourly limit burnt the whole remainder of its
			// audience the moment the limit was reached, and then
			// "completed". The message stays pending and the batch stops:
			// the loop comes back after next_batch_at, by which time the
			// window has moved.
			if qe, ok := errors.AsType[*quota.Error](err); ok {
				r.Log.Warn("campaign: batch paused on quota",
					"campaign_id", c.ID, "queued", queued, "reason", qe.Error())

				return true
			}

			failed++
			if uerr := r.Store.Campaign.UpdateMessage(ctx, m.ID, cmodel.MsgFailed, err.Error(), ""); uerr != nil {
				r.Log.Error("campaign: update message", "message_id", m.ID, "err", uerr)
			}

			continue
		}

		queued++
	}

	r.Log.Info("campaign: batch processed",
		"campaign_id", c.ID, "queued", queued, "failed", failed)

	return false
}

// deliverMessage renders the campaign template for one subscriber and
// queues the email.
func (r *Runner) deliverMessage(ctx context.Context, c *cmodel.Campaign, m *cmodel.Message) error {
	sub, err := r.Store.Subscriber.Get(ctx, c.ProjectID, m.SubscriberID)
	if err != nil {
		return err
	}

	if sub == nil || sub.Status != submodel.StatusSubscribed {
		return r.Store.Campaign.UpdateMessage(ctx, m.ID, cmodel.MsgSkipped, "subscriber missing or no longer subscribed", "")
	}

	out, templateID, err := r.renderFor(ctx, c, m, sub)
	if err != nil {
		return err
	}

	sender := c.FromEmail
	if c.FromName != "" {
		// Through the formatter: a name with a comma composed by hand is
		// two addresses to a parser, and a non-ASCII one needs encoding.
		sender = smtpclient.FormatAddress(c.FromName, c.FromEmail)
	}

	req := &email.SendRequest{
		From:    sender,
		ReplyTo: c.ReplyTo,
		To:      []string{sub.Email},
		Subject: out.Subject,
		HTML:    out.HTML,
		Text:    out.Text,
		// The campaign's pool, already resolved to an id when the
		// campaign was created. Empty means the project's default.
		Route: email.Route{GroupID: c.SMTPGroupID},
	}
	if err := r.EmailService.AttachTemplateFiles(ctx, c.ProjectID, templateID, req); err != nil {
		return err
	}

	r.applyTracking(ctx, c, m, req)
	e, _, err := r.EmailService.Send(ctx, c.ProjectID, c.CreatedBy, "", req)
	if err != nil {
		return err
	}

	// The message row must record what Send just did even while ctx is
	// being cancelled for shutdown. Send wrote the email row - if the
	// write below is refused on a dead context, the campaign message
	// stays pending and the next start sends this recipient a second
	// copy of a message that is already in the queue.
	fctx := context.WithoutCancel(ctx)
	if e.Status == emailmodel.StatusSuppressed {
		return r.Store.Campaign.UpdateMessage(fctx, m.ID, cmodel.MsgSkipped, "recipient suppressed", e.ID)
	}

	return r.Store.Campaign.UpdateMessage(fctx, m.ID, cmodel.MsgQueued, "", e.ID)
}

// renderFor renders the campaign content for one subscriber: variant
// template and subject overrides, subscriber language fallback, and
// data merged as campaign data < custom fields < email + name.
func (r *Runner) renderFor(ctx context.Context, c *cmodel.Campaign, m *cmodel.Message, sub *submodel.Subscriber) (*render.Output, string, error) {
	templateID := c.TemplateID
	variantSubject := ""
	if c.ABTestEnabled && m.Variant != "" {
		for _, v := range c.ABVariants {
			if v.Name == m.Variant {
				if v.TemplateID != "" {
					templateID = v.TemplateID
				}

				variantSubject = v.Subject
				break
			}
		}
	}

	data := map[string]any{}
	maps.Copy(data, c.TemplateData)
	maps.Copy(data, sub.CustomFields)
	data["email"] = sub.Email
	data["name"] = sub.Name

	// RenderTemplate injects these for the body. It is applied to the
	// local map as well because the A/B variant subject below is
	// rendered HERE, against this map, and would otherwise fail on a
	// reserved name the body accepts.
	data = tracking.WithSystemVars(data)

	language := sub.Language
	if language == "" {
		language = c.Language
	}

	out, _, err := r.EmailService.RenderTemplate(ctx, c.ProjectID, &email.TemplateRef{
		ID:       templateID,
		Language: language,
		Data:     data,
		Lenient:  true,
	})
	if err != nil {
		return nil, "", err
	}

	if variantSubject != "" {
		rd := &render.Renderer{MissingKeyBehavior: render.MissingKeyZero}
		if vout, verr := rd.Render(&render.Input{Subject: variantSubject}, data); verr == nil {
			out.Subject = vout.Subject
		} else {
			out.Subject = variantSubject
		}
	}

	if out.Subject == "" {
		out.Subject = c.Subject
	}

	return out, templateID, nil
}

// applyTracking substitutes the system-link sentinels, rewrites links
// for click tracking, injects the open pixel, and stamps the
// List-Unsubscribe headers. No-op when tracking is disabled (the
// sentinels are stripped so no broken links ship).
func (r *Runner) applyTracking(ctx context.Context, c *cmodel.Campaign, m *cmodel.Message, req *email.SendRequest) {
	if !r.Tracking.Enabled() {
		req.Subject = tracking.SubstituteSystemLinks(req.Subject, tracking.Links{})
		req.HTML = tracking.SubstituteSystemLinks(req.HTML, tracking.Links{})
		req.Text = tracking.SubstituteSystemLinks(req.Text, tracking.Links{})

		return
	}

	// The email id is minted here so the web view token can embed it
	// before the row exists. The service honors the pinned id.
	req.ID = ids.New()
	system := tracking.Links{
		WebView:     r.Tracking.WebViewURL(req.ID),
		Unsubscribe: r.Tracking.UnsubscribeURL(m.ID),
	}
	req.Subject = tracking.SubstituteSystemLinks(req.Subject, system)
	req.HTML = tracking.SubstituteSystemLinks(req.HTML, system)
	req.Text = tracking.SubstituteSystemLinks(req.Text, system)

	// Keyed on the EMAIL id, like every other tracked send - the id was
	// minted just above precisely so it exists before the row does.
	// Link tallies stay grouped by campaign.
	html, links := r.Tracking.ProcessHTML(req.HTML, tracking.TrackOpts{
		EmailID: req.ID, LinkScope: c.ID, Opens: true, Clicks: true,
	})
	req.HTML = html
	req.Tracked = true
	for _, l := range links {
		if err := r.Store.Campaign.UpsertTrackedLink(ctx, &cmodel.TrackedLink{
			ProjectID: c.ProjectID, CampaignID: c.ID, OriginalURL: l.URL, Hash: l.Hash,
		}); err != nil {
			r.Log.Error("campaign: upsert tracked link", "campaign_id", c.ID, "err", err)
		}
	}

	req.ListUnsubscribeURL = system.Unsubscribe
	req.ListUnsubscribePost = true
}

// complete finalizes the campaign and emits campaign.completed with
// the message stats.
func (r *Runner) complete(ctx context.Context, c *cmodel.Campaign) {
	now := time.Now().UTC()

	// From SENDING only. A cancel skips the unsent remainder, which
	// makes CountPending answer zero and brings us straight here - so
	// unguarded this reported a cancelled campaign as sent, completed_at
	// and campaign.completed webhook included.
	moved, err := r.Store.Campaign.SetRunState(ctx, c.ID, cmodel.StatusSent, nil, &now, nil, cmodel.StatusSending)
	if err != nil {
		r.Log.Error("campaign: complete", "campaign_id", c.ID, "err", err)

		return
	}

	// Not ours to finish. Emitting the event anyway would tell every
	// webhook consumer the campaign completed after the operator
	// stopped it.
	if !moved {
		r.Log.Info("campaign: not completed, no longer sending", "campaign_id", c.ID)

		return
	}

	totals, _, err := r.Store.Campaign.MessageStats(ctx, c.ID)
	if err != nil {
		totals = map[string]int{}
	}

	r.Log.Info("campaign: completed", "campaign_id", c.ID, "stats", totals)
	r.Emit(ctx, c.ProjectID, whmodel.EventCampaignCompleted, c.FromEmail, eventPayload(c, totals))
}

// assignVariants shuffles the audience and slices it by the variant
// split percentages, remainder to the last variant (old platform
// semantics).
func assignVariants(c *cmodel.Campaign, msgs []*cmodel.Message) {
	if !c.ABTestEnabled || len(c.ABVariants) == 0 || len(msgs) == 0 {
		return
	}

	rand.Shuffle(len(msgs), func(i, j int) { msgs[i], msgs[j] = msgs[j], msgs[i] })
	idx := 0
	for _, v := range c.ABVariants {
		count := len(msgs) * v.SplitPercentage / 100
		if count == 0 {
			count = 1
		}

		end := min(idx+count, len(msgs))
		for i := idx; i < end; i++ {
			msgs[i].Variant = v.Name
		}

		idx = end
	}

	last := c.ABVariants[len(c.ABVariants)-1].Name
	for i := idx; i < len(msgs); i++ {
		msgs[i].Variant = last
	}
}

// localDeliverAt maps the campaign's scheduled instant to the same
// wall-clock time in the subscriber's timezone. Subscribers without a
// usable timezone keep the plain instant, and times already past are
// delivered immediately (nil).
func localDeliverAt(c *cmodel.Campaign, sub *submodel.Subscriber) *time.Time {
	ref := c.ScheduledAt
	if ref == nil {
		ref = c.StartedAt
	}
	if ref == nil {
		return nil
	}

	if sub.Timezone == "" {
		return ref
	}

	loc, err := time.LoadLocation(sub.Timezone)
	if err != nil {
		return ref
	}

	utc := ref.UTC()
	y, mo, d := utc.Date()
	h, mi, _ := utc.Clock()
	local := time.Date(y, mo, d, h, mi, 0, 0, loc).UTC()

	return &local
}

func eventPayload(c *cmodel.Campaign, counts map[string]int) map[string]any {
	return map[string]any{
		"id":         c.ID,
		"project_id": c.ProjectID,
		"name":       c.Name,
		"from_email": c.FromEmail,
		"list_id":    c.ListID,
		"counts":     counts,
	}
}
