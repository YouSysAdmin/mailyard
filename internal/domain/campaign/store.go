// Mailyard, Copyright (c) 2021-2026 YouSysAdmin

// Package campaign is the persistence, handler, and runner surface
// for bulk sends. The runner (runner.go) drains sending campaigns
// into the email queue in throttled batches. Routes live behind
// requireAuth + requireProject in server/routes.go.
package campaign

import (
	"context"
	"database/sql"
	"errors"
	"log/slog"
	"strings"
	"time"

	"github.com/yousysadmin/mailyard/internal/core/ids"

	"github.com/yousysadmin/mailyard/internal/database"
	cmodel "github.com/yousysadmin/mailyard/internal/models/campaign"
)

// Store persists campaigns and their per-recipient messages. Project
// scoped: a method taking projID answers nothing for a row another
// project owns.
type Store struct {
	database.Base
}

// NewStore builds the store on db, wiring the shared query helpers
// through database.Base.
func NewStore(db *sql.DB) *Store {
	return &Store{Base: database.NewBase(db)}
}

const campaignSelect = `
SELECT id, project_id, created_by, name, subject, from_email, from_name,
       template_id, language, template_data, status, list_id, smtp_group_id, send_rate,
       send_at_local_time, ab_test_enabled, ab_variants,
       scheduled_at, started_at, completed_at, next_batch_at, created_at, updated_at
FROM campaigns`

// Get returns one campaign within projID, or nil when there is no such
// row.
func (s *Store) Get(ctx context.Context, projID, id string) (*cmodel.Campaign, error) {
	row := s.QueryRow(ctx, campaignSelect+` WHERE project_id = ? AND id = ?`, projID, id)
	c, err := scanCampaign(row)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, nil
	}

	return c, err
}

// GetAny is the runner-side lookup (already claimed, no proj scope).
func (s *Store) GetAny(ctx context.Context, id string) (*cmodel.Campaign, error) {
	row := s.QueryRow(ctx, campaignSelect+` WHERE id = ?`, id)
	c, err := scanCampaign(row)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, nil
	}

	return c, err
}

// List returns every campaign in projID.
func (s *Store) List(ctx context.Context, projID string) ([]*cmodel.Campaign, error) {
	rows, err := s.Query(ctx, campaignSelect+` WHERE project_id = ? ORDER BY created_at DESC`, projID)
	if err != nil {
		return nil, err
	}

	defer func() { _ = rows.Close() }()
	var out []*cmodel.Campaign
	for rows.Next() {
		c, err := scanCampaign(rows)
		if err != nil {
			return nil, err
		}

		out = append(out, c)
	}

	return out, rows.Err()
}

// Put inserts the campaign, or updates the row's CONTENT when its id
// already exists.
//
// The lifecycle columns - status, scheduled_at, started_at,
// completed_at, next_batch_at - are deliberately absent from the
// conflict update. Every change to those goes through a guarded
// statement (Launch, TransitionStatus, SetRunState) that names the
// states it may move FROM, because an unguarded status write here is
// exactly how a concurrent Cancel was silently undone: Get read
// draft, the operator cancelled, and Put wrote the stale status back.
func (s *Store) Put(ctx context.Context, c *cmodel.Campaign) error {
	if c.CreatedAt.IsZero() {
		c.CreatedAt = time.Now().UTC()
	}

	_, err := s.Exec(ctx, `
        INSERT INTO campaigns (
            id, project_id, created_by, name, subject, from_email, from_name,
            template_id, language, template_data, status, list_id, smtp_group_id, send_rate,
            send_at_local_time, ab_test_enabled, ab_variants,
            scheduled_at, started_at, completed_at, next_batch_at, created_at, updated_at
        ) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
        ON CONFLICT(id) DO UPDATE SET
            name               = excluded.name,
            subject            = excluded.subject,
            from_email         = excluded.from_email,
            from_name          = excluded.from_name,
            template_id        = excluded.template_id,
            language           = excluded.language,
            template_data      = excluded.template_data,
            smtp_group_id      = excluded.smtp_group_id,
            list_id            = excluded.list_id,
            send_rate          = excluded.send_rate,
            send_at_local_time = excluded.send_at_local_time,
            ab_test_enabled    = excluded.ab_test_enabled,
            ab_variants        = excluded.ab_variants,
            updated_at         = excluded.updated_at
    `,
		c.ID, c.ProjectID, c.CreatedBy, c.Name, c.Subject, c.FromEmail, c.FromName,
		c.TemplateID, c.Language, database.MustJSON(c.TemplateData), c.Status, c.ListID, database.NullStr(c.SMTPGroupID),
		c.SendRate, c.SendAtLocalTime, c.ABTestEnabled, database.MustJSON(c.ABVariants),
		database.NullTime(c.ScheduledAt), database.NullTime(c.StartedAt),
		database.NullTime(c.CompletedAt), database.NullTime(c.NextBatchAt),
		c.CreatedAt, database.NullTime(c.UpdatedAt),
	)

	return err
}

// Delete removes one campaign from projID.
func (s *Store) Delete(ctx context.Context, projID, id string) error {
	_, err := s.Exec(ctx, `DELETE FROM campaigns WHERE project_id = ? AND id = ?`, projID, id)

	return err
}

// Launch is Send's one guarded statement: it stamps the launch
// columns and moves the campaign out of draft or scheduled in the
// same UPDATE, so the state it checked is the state it changes.
// False means an operator moved the campaign first - the unguarded
// Put this replaces overwrote a concurrent Cancel with the stale
// status, resurrecting a cancelled campaign.
func (s *Store) Launch(ctx context.Context, projID, id, status string, scheduledAt, startedAt, nextBatchAt *time.Time) (bool, error) {
	res, err := s.Exec(ctx, `
        UPDATE campaigns
        SET status = ?, scheduled_at = ?, started_at = ?, next_batch_at = ?, updated_at = ?
        WHERE project_id = ? AND id = ? AND status IN (?, ?)`,
		status, database.NullTime(scheduledAt), database.NullTime(startedAt),
		database.NullTime(nextBatchAt), time.Now().UTC(),
		projID, id, cmodel.StatusDraft, cmodel.StatusScheduled)
	if err != nil {
		return false, err
	}

	n, err := res.RowsAffected()

	return n > 0, err
}

// TransitionStatus moves the campaign between lifecycle states only
// when it currently sits in one of the from states. Returns false
// when the guard failed (concurrent transition or invalid state).
func (s *Store) TransitionStatus(ctx context.Context, projID, id, to string, from ...string) (bool, error) {
	if len(from) == 0 {
		return false, errors.New("transition requires at least one from state")
	}

	var b strings.Builder
	b.WriteString(`UPDATE campaigns SET status = ?, updated_at = ? WHERE project_id = ? AND id = ? AND status IN (`)
	args := []any{to, time.Now().UTC(), projID, id}
	for i, f := range from {
		if i > 0 {
			b.WriteString(", ")
		}

		b.WriteString("?")
		args = append(args, f)
	}

	b.WriteString(")")
	res, err := s.Exec(ctx, b.String(), args...)
	if err != nil {
		return false, err
	}

	n, err := res.RowsAffected()

	return n > 0, err
}

// SetRunState updates the runner-owned columns, but only while the
// campaign still sits in one of the from states. Reports whether it
// fired.
//
// The guard is the whole point and it is not optional, so from is
// checked like TransitionStatus checks it.
//
// Without it this statement was `WHERE id = ?` and it silently undid
// the operator. Pause and Cancel go through TransitionStatus, which is
// guarded - but the runner writes the status too, at the END of a batch
// it had already started. So pausing while a batch was in flight got
// the pause applied, the batch finished, and this UPDATE put the
// campaign back to sending and it carried on. Cancel went further: the
// remaining rows were skipped, CountPending then answered zero, and
// complete() stamped the CANCELLED campaign as sent and emitted
// campaign.completed.
func (s *Store) SetRunState(ctx context.Context, id string, status string, startedAt, completedAt, nextBatchAt *time.Time, from ...string) (bool, error) {
	if len(from) == 0 {
		return false, errors.New("set run state requires at least one from state")
	}

	var b strings.Builder
	b.WriteString(`
        UPDATE campaigns
        SET status = ?, started_at = COALESCE(?, started_at),
            completed_at = ?, next_batch_at = ?, updated_at = ?
        WHERE id = ? AND status IN (`)
	args := []any{status, database.NullTime(startedAt), database.NullTime(completedAt),
		database.NullTime(nextBatchAt), time.Now().UTC(), id}
	for i, f := range from {
		if i > 0 {
			b.WriteString(", ")
		}

		b.WriteString("?")
		args = append(args, f)
	}

	b.WriteString(")")
	res, err := s.Exec(ctx, b.String(), args...)
	if err != nil {
		return false, err
	}

	n, err := res.RowsAffected()

	return n > 0, err
}

// Status reads just the lifecycle state, for the delivery loop to ask
// whether it should still be running.
//
// A column rather than the row: deliverBatch asks once per message, and
// the answer decides whether the next one is sent at all.
//
// Project-scoped even though the caller holds a campaign it has already
// claimed, so no exemption is needed - TestEveryTenantQueryNamesTheProject
// asked for this and scoping it is cheaper than arguing the caller is
// safe. The runner has the project id on the campaign in front of it.
func (s *Store) Status(ctx context.Context, projID, id string) (string, error) {
	var status string
	err := s.QueryRow(ctx,
		`SELECT status FROM campaigns WHERE project_id = ? AND id = ?`, projID, id).Scan(&status)
	if errors.Is(err, sql.ErrNoRows) {
		return "", nil
	}

	return status, err
}

// PromoteScheduled flips due scheduled campaigns to sending so the
// claim loop picks them up. Returns how many were promoted.
func (s *Store) PromoteScheduled(ctx context.Context, now time.Time) (int, error) {
	res, err := s.Exec(ctx, `
        UPDATE campaigns
        SET status = ?, started_at = ?, next_batch_at = ?, updated_at = ?
        WHERE status = ? AND scheduled_at <= ?
    `, cmodel.StatusSending, now, now, now, cmodel.StatusScheduled, now)
	if err != nil {
		return 0, err
	}

	n, err := res.RowsAffected()

	return int(n), err
}

// ClaimDue leases one sending campaign whose next batch is due, and
// the lease keeps a crashed batch from blocking the campaign for more
// than leaseFor.
//
// One statement, for the reason claimDueSQL in the email store is one:
// the SELECT-then-guarded-UPDATE it replaces made every node read the
// SAME head campaign and lose to whichever locked it first, answering
// nil for the whole tick even when a second due campaign existed - so
// batch latency grew with the node count. SKIP LOCKED makes a loser
// step over the contested row and take the next due one instead.
// Postgres re-checks a FOR UPDATE row's qualifier after granting the
// lock, so the status and due-time guards hold without a second
// statement.
func (s *Store) ClaimDue(ctx context.Context, now time.Time, leaseFor time.Duration) (*cmodel.Campaign, error) {
	row := s.QueryRow(ctx, `
        UPDATE campaigns SET next_batch_at = ?
        WHERE id = (
            SELECT id FROM campaigns
            WHERE status = ? AND (next_batch_at IS NULL OR next_batch_at <= ?)
            ORDER BY next_batch_at ASC
            LIMIT 1
            FOR UPDATE SKIP LOCKED
        )
        RETURNING id
    `, now.Add(leaseFor), cmodel.StatusSending, now)
	var id string
	if err := row.Scan(&id); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, nil
		}

		return nil, err
	}

	return s.GetAny(ctx, id)
}

// ----------------------------------------------------------------------------
// Messages
// ----------------------------------------------------------------------------

// BulkCreateMessages inserts the fan-out in one transaction.
func (s *Store) BulkCreateMessages(ctx context.Context, msgs []*cmodel.Message) error {
	tx, err := s.DB().BeginTx(ctx, nil)
	if err != nil {
		return err
	}

	defer func() {
		// ErrTxDone is the normal path: Commit already ran. Anything
		// else means the rollback itself failed, which can leave locks
		// held and is worth saying out loud.
		if rerr := tx.Rollback(); rerr != nil && !errors.Is(rerr, sql.ErrTxDone) {
			slog.Warn("store: rollback failed", "err", rerr)
		}
	}()
	stmt, err := tx.PrepareContext(ctx, s.Q(`
        INSERT INTO campaign_messages (id, campaign_id, subscriber_id, status, variant, deliver_at, created_at)
        VALUES (?, ?, ?, ?, ?, ?, ?)
        ON CONFLICT(campaign_id, subscriber_id) DO NOTHING
    `))
	if err != nil {
		return err
	}

	defer func() { _ = stmt.Close() }()
	now := time.Now().UTC()
	for _, m := range msgs {
		if m.ID == "" {
			m.ID = ids.New()
		}

		if m.CreatedAt.IsZero() {
			m.CreatedAt = now
		}

		// A prepared statement: these are bind values, not SQL. The
		// statement text was checked at PrepareContext above.
		//sqlconst:allow bind values on a prepared statement, not a query
		if _, err := stmt.ExecContext(ctx, m.ID, m.CampaignID, m.SubscriberID,
			m.Status, m.Variant, database.NullTime(m.DeliverAt), m.CreatedAt); err != nil {
			return err
		}
	}

	return tx.Commit()
}

// HasMessages reports whether fan-out already ran for the campaign.
func (s *Store) HasMessages(ctx context.Context, campaignID string) (bool, error) {
	var n int
	err := s.QueryRow(ctx, `SELECT COUNT(*) FROM campaign_messages WHERE campaign_id = ?`, campaignID).Scan(&n)

	return n > 0, err
}

const messageSelect = `
SELECT id, campaign_id, subscriber_id, email_id, status, error_message, variant,
       deliver_at, sent_at, opened_at, clicked_at, created_at
FROM campaign_messages`

// PendingDue returns pending messages whose deliver_at (when set) has
// arrived, oldest first.
func (s *Store) PendingDue(ctx context.Context, campaignID string, now time.Time, limit int) ([]*cmodel.Message, error) {
	rows, err := s.Query(ctx, messageSelect+`
        WHERE campaign_id = ? AND status = ?
          AND (deliver_at IS NULL OR deliver_at <= ?)
        ORDER BY created_at ASC
        LIMIT ?
    `, campaignID, cmodel.MsgPending, now, limit)
	if err != nil {
		return nil, err
	}

	defer func() { _ = rows.Close() }()

	return collectMessages(rows)
}

// CountPending counts every pending message including future
// deliver_at rows (completion check).
func (s *Store) CountPending(ctx context.Context, campaignID string) (int, error) {
	var n int
	err := s.QueryRow(ctx, `
        SELECT COUNT(*) FROM campaign_messages WHERE campaign_id = ? AND status = ?
    `, campaignID, cmodel.MsgPending).Scan(&n)

	return n, err
}

// NextDeliverAt returns the earliest future deliver_at among pending
// messages, or nil when none is scheduled.
func (s *Store) NextDeliverAt(ctx context.Context, campaignID string) (*time.Time, error) {
	var t sql.NullTime
	err := s.QueryRow(ctx, `
        SELECT MIN(deliver_at) FROM campaign_messages
        WHERE campaign_id = ? AND status = ? AND deliver_at IS NOT NULL
    `, campaignID, cmodel.MsgPending).Scan(&t)
	if err != nil {
		return nil, err
	}

	if !t.Valid {
		return nil, nil
	}

	return &t.Time, nil
}

// UpdateMessage writes status, error, and optionally the email link.
func (s *Store) UpdateMessage(ctx context.Context, id, status, errMsg, emailID string) error {
	// NullStr here, or every call without an email id fails.
	//
	// email_id is a uuid column, so Postgres types the parameter as uuid,
	// and the parameter is bound before the CASE picks a branch. An empty
	// string is therefore rejected as a malformed uuid whichever branch
	// would have run. Both callers that pass no id hit this: the runner
	// marking a message failed, and the runner marking one skipped. The
	// statement prepares cleanly, which is why the schema guard cannot
	// see it, and then answers 22P02 on execute.
	//
	// The damage is quiet. The row stays pending, CountPending never
	// reaches zero, complete() never runs, and the campaign redelivers
	// the same failing message on every batch. One recipient
	// unsubscribing between fan-out and delivery is enough to trigger it.
	//
	// The CASE stays, and now tests for NULL, so a caller with no id
	// still leaves an id that was already recorded alone.
	_, err := s.Exec(ctx, `
        UPDATE campaign_messages
        SET status = ?, error_message = ?,
            email_id = COALESCE(?::uuid, email_id)
        WHERE id = ?
    `, status, errMsg, database.NullStr(emailID), id)

	return err
}

// MarkMessageByEmail syncs the message with its email's terminal
// status (called from the queue worker's finalize hook).
func (s *Store) MarkMessageByEmail(ctx context.Context, emailID, status, errMsg string) error {
	var sentAt any
	if status == cmodel.MsgSent {
		sentAt = time.Now().UTC()
	}

	_, err := s.Exec(ctx, `
        UPDATE campaign_messages SET status = ?, error_message = ?, sent_at = ?
        WHERE email_id = ?
    `, status, errMsg, sentAt, emailID)

	return err
}

// SkipPending marks every remaining pending message skipped
// (campaign cancelled). Returns the count.
func (s *Store) SkipPending(ctx context.Context, campaignID, reason string) (int, error) {
	res, err := s.Exec(ctx, `
        UPDATE campaign_messages SET status = ?, error_message = ?
        WHERE campaign_id = ? AND status = ?
    `, cmodel.MsgSkipped, reason, campaignID, cmodel.MsgPending)
	if err != nil {
		return 0, err
	}

	n, err := res.RowsAffected()

	return int(n), err
}

// ListMessages pages a campaign's messages for the console. The
// campaign join enforces the project scope.
func (s *Store) ListMessages(ctx context.Context, projID, campaignID string, limit, offset int) ([]*cmodel.Message, error) {
	if limit <= 0 || limit > 500 {
		limit = 100
	}

	rows, err := s.Query(ctx, `
        SELECT m.id, m.campaign_id, m.subscriber_id, m.email_id, m.status, m.error_message,
               m.variant, m.deliver_at, m.sent_at, m.opened_at, m.clicked_at, m.created_at,
               COALESCE(s.email, '')
        FROM campaign_messages m
        JOIN campaigns c ON c.id = m.campaign_id
        -- LEFT: a subscriber deleted after the fan-out leaves the
        -- message row, and losing the whole row from the list would
        -- hide a send that actually happened.
        LEFT JOIN subscribers s ON s.id = m.subscriber_id
        WHERE c.project_id = ? AND m.campaign_id = ?
        ORDER BY m.created_at ASC
        LIMIT ? OFFSET ?
    `, projID, campaignID, limit, max(offset, 0))
	if err != nil {
		return nil, err
	}

	defer func() { _ = rows.Close() }()

	return collectMessagesWithEmail(rows)
}

// MessageStats returns per-status counts, optionally split by variant.
func (s *Store) MessageStats(ctx context.Context, campaignID string) (map[string]int, map[string]map[string]int, error) {
	rows, err := s.Query(ctx, `
        SELECT status, variant, COUNT(*) FROM campaign_messages
        WHERE campaign_id = ? GROUP BY status, variant
    `, campaignID)
	if err != nil {
		return nil, nil, err
	}

	defer func() { _ = rows.Close() }()
	totals := map[string]int{}
	byVariant := map[string]map[string]int{}
	for rows.Next() {
		var status, variant string
		var n int
		if err := rows.Scan(&status, &variant, &n); err != nil {
			return nil, nil, err
		}

		totals[status] += n
		if variant != "" {
			if byVariant[variant] == nil {
				byVariant[variant] = map[string]int{}
			}

			byVariant[variant][status] += n
		}
	}

	return totals, byVariant, rows.Err()
}

func scanCampaign(r interface{ Scan(...any) error }) (*cmodel.Campaign, error) {
	var c cmodel.Campaign
	var data, variants string
	var scheduledAt, startedAt, completedAt, nextBatchAt, updatedAt sql.NullTime
	if err := r.Scan(&c.ID, &c.ProjectID, &c.CreatedBy, &c.Name, &c.Subject,
		&c.FromEmail, &c.FromName, &c.TemplateID, &c.Language, &data, &c.Status,
		&c.ListID, database.Str(&c.SMTPGroupID), &c.SendRate, &c.SendAtLocalTime, &c.ABTestEnabled, &variants,
		&scheduledAt, &startedAt, &completedAt, &nextBatchAt, &c.CreatedAt, &updatedAt); err != nil {
		return nil, err
	}

	database.MustUnmarshalJSON(data, &c.TemplateData)
	database.MustUnmarshalJSON(variants, &c.ABVariants)
	if scheduledAt.Valid {
		c.ScheduledAt = new(scheduledAt.Time)
	}

	if startedAt.Valid {
		c.StartedAt = new(startedAt.Time)
	}

	if completedAt.Valid {
		c.CompletedAt = new(completedAt.Time)
	}

	if nextBatchAt.Valid {
		c.NextBatchAt = new(nextBatchAt.Time)
	}

	if updatedAt.Valid {
		c.UpdatedAt = new(updatedAt.Time)
	}

	return &c, nil
}

func scanMessage(r interface{ Scan(...any) error }) (*cmodel.Message, error) {
	var m cmodel.Message
	var deliverAt, sentAt, openedAt, clickedAt sql.NullTime
	if err := r.Scan(&m.ID, &m.CampaignID, &m.SubscriberID, database.Str(&m.EmailID), &m.Status,
		&m.ErrorMessage, &m.Variant, &deliverAt, &sentAt, &openedAt, &clickedAt, &m.CreatedAt); err != nil {
		return nil, err
	}

	if deliverAt.Valid {
		m.DeliverAt = new(deliverAt.Time)
	}

	if sentAt.Valid {
		m.SentAt = new(sentAt.Time)
	}

	if openedAt.Valid {
		m.OpenedAt = new(openedAt.Time)
	}

	if clickedAt.Valid {
		m.ClickedAt = new(clickedAt.Time)
	}

	return &m, nil
}

// collectMessagesWithEmail reads the console list shape, which
// carries one extra joined column. Separate from collectMessages
// rather than a flag: the two queries select different things, and a
// scan that guesses which is a scan that will eventually guess wrong.
func collectMessagesWithEmail(rows *sql.Rows) ([]*cmodel.Message, error) {
	var out []*cmodel.Message
	for rows.Next() {
		var m cmodel.Message
		var deliverAt, sentAt, openedAt, clickedAt sql.NullTime
		if err := rows.Scan(&m.ID, &m.CampaignID, &m.SubscriberID, database.Str(&m.EmailID), &m.Status,
			&m.ErrorMessage, &m.Variant, &deliverAt, &sentAt, &openedAt, &clickedAt,
			&m.CreatedAt, &m.Email); err != nil {
			return nil, err
		}

		if deliverAt.Valid {
			m.DeliverAt = new(deliverAt.Time)
		}

		if sentAt.Valid {
			m.SentAt = new(sentAt.Time)
		}

		if openedAt.Valid {
			m.OpenedAt = new(openedAt.Time)
		}

		if clickedAt.Valid {
			m.ClickedAt = new(clickedAt.Time)
		}

		out = append(out, &m)
	}

	return out, rows.Err()
}

func collectMessages(rows *sql.Rows) ([]*cmodel.Message, error) {
	var out []*cmodel.Message
	for rows.Next() {
		m, err := scanMessage(rows)
		if err != nil {
			return nil, err
		}

		out = append(out, m)
	}

	return out, rows.Err()
}

// ----------------------------------------------------------------------------
// Tracking
// ----------------------------------------------------------------------------

// GetMessageAny loads a message without project scope, for the
// signed public tracking surface.
func (s *Store) GetMessageAny(ctx context.Context, id string) (*cmodel.Message, error) {
	row := s.QueryRow(ctx, messageSelect+` WHERE id = ?`, id)
	m, err := scanMessage(row)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, nil
	}

	return m, err
}

// MarkOpened stamps the first open (later opens keep the original
// time).
// MarkOpened stamps the first open, reporting whether a row changed.
//
// The bool matters: the UPDATE matches nothing both when the message
// was already open and when the id names no row at all, and the second
// case means a signed pixel URL points at nothing. Swallowing that
// distinction is what made an open rate stuck at zero impossible to
// diagnose from outside.
func (s *Store) MarkOpened(ctx context.Context, messageID string, t time.Time) (bool, error) {
	res, err := s.Exec(ctx, `
        UPDATE campaign_messages SET opened_at = ? WHERE id = ? AND opened_at IS NULL
    `, t, messageID)
	if err != nil {
		return false, err
	}

	n, err := res.RowsAffected()
	if err != nil {
		return false, err
	}

	return n > 0, nil
}

// MarkClicked stamps the first click. A click implies an open, so the
// open stamp is backfilled too.
func (s *Store) MarkClicked(ctx context.Context, messageID string, t time.Time) error {
	if _, err := s.Exec(ctx, `
        UPDATE campaign_messages SET clicked_at = ? WHERE id = ? AND clicked_at IS NULL
    `, t, messageID); err != nil {
		return err
	}

	// The bool is not interesting here: a click on an already-open
	// message legitimately marks nothing.
	_, err := s.MarkOpened(ctx, messageID, t)

	return err
}

// UpsertTrackedLink registers a rewritten link once per grouping.
// The grouping is (project, campaign): a campaign shares one row per
// URL across its messages, so click_count is the campaign total, and
// transactional links group per project the same way with an empty
// campaign id.
func (s *Store) UpsertTrackedLink(ctx context.Context, l *cmodel.TrackedLink) error {
	if l.ID == "" {
		l.ID = ids.New()
	}

	if l.CreatedAt.IsZero() {
		l.CreatedAt = time.Now().UTC()
	}

	_, err := s.Exec(ctx, `
        INSERT INTO tracked_links (id, project_id, campaign_id, original_url, hash, click_count, created_at)
        VALUES (?, ?, ?, ?, ?, 0, ?)
        ON CONFLICT(project_id, campaign_id, hash) DO NOTHING
    `, l.ID, database.NullStr(l.ProjectID), database.NullStr(l.CampaignID),
		l.OriginalURL, l.Hash, l.CreatedAt)

	return err
}

// GetTrackedLink returns one tracked link within projID, or nil when
// there is no such row.
func (s *Store) GetTrackedLink(ctx context.Context, projID, campaignID, hash string) (*cmodel.TrackedLink, error) {
	row := s.QueryRow(ctx, `
        SELECT id, project_id, campaign_id, original_url, hash, click_count, created_at
        FROM tracked_links
        WHERE project_id = ?
          AND campaign_id IS NOT DISTINCT FROM ?::uuid
          AND hash = ?
    `, projID, database.NullStr(campaignID), hash)
	var l cmodel.TrackedLink
	err := row.Scan(&l.ID, database.Str(&l.ProjectID), database.Str(&l.CampaignID),
		&l.OriginalURL, &l.Hash, &l.ClickCount, &l.CreatedAt)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, nil
	}

	if err != nil {
		return nil, err
	}

	return &l, nil
}

// TrackedLinkURLs answers the original destination behind a set of
// link hashes, project wide, keyed by hash. Hashes nothing was
// registered for are simply absent from the map.
//
// Project wide rather than per scope on purpose: the hash bakes its
// scope in (HashLink hashes scope and URL together), so one hash
// cannot name two different destinations within a project - and the
// caller, holding only a rendered body, has no scope to offer.
func (s *Store) TrackedLinkURLs(ctx context.Context, projID string, hashes []string) (map[string]string, error) {
	out := map[string]string{}
	if len(hashes) == 0 {
		return out, nil
	}

	var b strings.Builder
	b.WriteString(`SELECT hash, original_url FROM tracked_links WHERE project_id = ? AND hash IN (`)
	args := []any{projID}
	for i, h := range hashes {
		if i > 0 {
			b.WriteString(", ")
		}

		b.WriteString("?")
		args = append(args, h)
	}

	b.WriteString(")")
	rows, err := s.Query(ctx, b.String(), args...)
	if err != nil {
		return nil, err
	}
	defer func() { _ = rows.Close() }()

	for rows.Next() {
		var hash, url string
		if err := rows.Scan(&hash, &url); err != nil {
			return nil, err
		}

		out[hash] = url
	}

	return out, rows.Err()
}

// ListTrackedLinks reads one campaign's links.
//
// A transactional link has no campaign, so campaign_id is NULL and
// both halves have to say so: NullStr writing, and IS NOT DISTINCT
// FROM reading, because = NULL is NULL rather than true. Passing ""
// straight through made Postgres refuse the statement outright.
//
// ListTrackedLinks returns every rewritten link with its click tally,
// most clicked first, for the per-campaign analytics readout.
func (s *Store) ListTrackedLinks(ctx context.Context, campaignID string) ([]*cmodel.TrackedLink, error) {
	rows, err := s.Query(ctx, `
        SELECT id, project_id, campaign_id, original_url, hash, click_count, created_at
        FROM tracked_links WHERE campaign_id = ?
        ORDER BY click_count DESC, original_url ASC
    `, campaignID)
	if err != nil {
		return nil, err
	}

	defer func() { _ = rows.Close() }()
	var out []*cmodel.TrackedLink
	for rows.Next() {
		var l cmodel.TrackedLink
		if err := rows.Scan(&l.ID, database.Str(&l.ProjectID), database.Str(&l.CampaignID), &l.OriginalURL,
			&l.Hash, &l.ClickCount, &l.CreatedAt); err != nil {
			return nil, err
		}

		out = append(out, &l)
	}

	return out, rows.Err()
}

// EventSeries buckets a campaign's raw tracking events per day.
//
// Series length is bounded by the retention sweep on tracking_events,
// not by this query - a purged event leaves the per-message and
// per-link counters intact but drops out of the series.
func (s *Store) EventSeries(ctx context.Context, campaignID, eventType string) ([]cmodel.DayCount, error) {
	rows, err := s.Query(ctx, `
        SELECT to_char(e.created_at, 'YYYY-MM-DD') AS day, COUNT(*)
        FROM tracking_events e
        JOIN campaign_messages m ON m.id = e.campaign_message_id
        WHERE m.campaign_id = ? AND e.event_type = ?
        GROUP BY day ORDER BY day ASC
    `, campaignID, eventType)
	if err != nil {
		return nil, err
	}

	defer func() { _ = rows.Close() }()
	var out []cmodel.DayCount
	for rows.Next() {
		var d cmodel.DayCount
		if err := rows.Scan(&d.Day, &d.Count); err != nil {
			return nil, err
		}

		out = append(out, d)
	}

	return out, rows.Err()
}

// IncrementLinkClicks adds one to a tracked link's tally.
func (s *Store) IncrementLinkClicks(ctx context.Context, id string) error {
	_, err := s.Exec(ctx, `UPDATE tracked_links SET click_count = click_count + 1 WHERE id = ?`, id)

	return err
}

// InsertTrackingEvent appends one raw hit to the event log.
func (s *Store) InsertTrackingEvent(ctx context.Context, ev *cmodel.TrackingEvent) error {
	if ev.ID == "" {
		ev.ID = ids.New()
	}

	if ev.CreatedAt.IsZero() {
		ev.CreatedAt = time.Now().UTC()
	}

	_, err := s.Exec(ctx, `
        INSERT INTO tracking_events (id, email_id, campaign_message_id, event_type,
                                     tracked_link_id, ip, user_agent, created_at)
        VALUES (?, ?, ?, ?, ?, ?, ?, ?)
    `, ev.ID, database.NullStr(ev.EmailID), database.NullStr(ev.CampaignMessageID), ev.EventType,
		database.NullStr(ev.TrackedLinkID), ev.IP, ev.UserAgent, ev.CreatedAt)

	return err
}

// EngagementStats returns unique opened and clicked message counts.
func (s *Store) EngagementStats(ctx context.Context, campaignID string) (opened, clicked int, err error) {
	err = s.QueryRow(ctx, `
        SELECT COUNT(opened_at), COUNT(clicked_at)
        FROM campaign_messages WHERE campaign_id = ?
    `, campaignID).Scan(&opened, &clicked)

	return opened, clicked, err
}

// PurgeTrackingEventsOlderThan trims open and click events. The
// per-message and per-link counters they rolled up into are kept --
// those are the numbers the campaign reports, and they are already
// aggregated.
func (s *Store) PurgeTrackingEventsOlderThan(ctx context.Context, before time.Time) (int64, error) {
	res, err := s.Exec(ctx, `DELETE FROM tracking_events WHERE created_at < ?`, before)
	if err != nil {
		return 0, err
	}

	return res.RowsAffected()
}

// GetMessageByEmail finds the campaign message an email belongs to,
// or (nil, nil) for a transactional send.
//
// Tracking keys on the email id now, so this is how a campaign's own
// per-message opened_at / clicked_at and its aggregate counts keep
// working: the handler marks the email, then asks whether there is
// also a campaign message behind it.
func (s *Store) GetMessageByEmail(ctx context.Context, emailID string) (*cmodel.Message, error) {
	if emailID == "" {
		return nil, nil
	}

	row := s.QueryRow(ctx, messageSelect+` WHERE email_id = ?`, emailID)
	m, err := scanMessage(row)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, nil
	}

	return m, err
}
