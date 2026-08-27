// Mailyard, Copyright (c) 2021-2026 YouSysAdmin

package email

import (
	"context"
	"database/sql"
	"errors"
	"strings"
	"time"

	"github.com/yousysadmin/mailyard/internal/core/ids"
	"github.com/yousysadmin/mailyard/internal/database"
	"github.com/yousysadmin/mailyard/internal/domain/store"
	emailmodel "github.com/yousysadmin/mailyard/internal/models/email"
)

// Filter aliases the interface's filter type for local brevity.
type Filter = store.EmailFilter

// Store persists the email log, which doubles as the delivery queue.
// Project scoped: a method taking projID answers nothing for a row
// another project owns.
type Store struct {
	database.Base
}

// NewStore takes optional read replicas, used by the log listing and the status counts (database.replica_reads.email_log).
// The quota count before a send and every queue method stay on the
// primary.
//
// Whether they arrive at all is the operator's call - see
// env.ReplicaReadsConfig.
func NewStore(db *sql.DB, replicas ...*sql.DB) *Store {
	return &Store{Base: database.NewBase(db, replicas...)}
}

// emailColumns is split out of emailSelect because the claim below
// returns the same shape from a RETURNING clause, and scanEmail reads
// them positionally - two hand-maintained lists would drift.
const emailColumns = `id, project_id, created_by, api_key_id, smtp_server_id, smtp_group_id, sender, recipients,
       subject, template_name, html_body, text_body, attachments_json, headers_json,
       list_unsubscribe_url, list_unsubscribe_mailto, list_unsubscribe_post, unsubscribe_list_id,
       status, error_message, attempts, max_attempts, next_attempt_at, claimed_at,
       created_at, scheduled_at, sent_at,
       tracked, opened_at, clicked_at, open_count, click_count, delivered_via`

const emailSelect = `
SELECT ` + emailColumns + `
FROM emails`

// idWindow is how far either side of an id's own timestamp the row it
// names can have been created.
//
// The id is minted by ids.New() and the row is written moments later, so
// in practice the gap is microseconds. An hour is not a guess at that
// gap - it is a bound wide enough that no realistic delay can escape it
// while still naming at most two of a WEEK-long partition. Nothing here
// depends on it being tight: missing the window costs a second query,
// never a wrong answer.
const idWindow = time.Hour

// ----------------------------------------------------------------------------
// Statements
// ----------------------------------------------------------------------------

// claimDueSQL grabs a batch of due rows in a single statement. The inner
// select locks the candidates, the outer update marks them processing,
// and RETURNING gives the rows back.
//
// SKIP LOCKED is what makes several workers safe together: once a node
// has locked a row the others simply do not see it, so nobody fights
// over the same batch.
//
// We deliberately do not repeat the status filter on the outer UPDATE.
// Postgres re-checks the qualifier of a FOR UPDATE row after it takes
// the lock, so anything claimed in the meantime drops out by itself.
//
// The two statuses are written as literals instead of parameters, and
// that matters more than it looks. idx_emails_queue is a partial index,
// and Postgres will only use one if it can prove the query predicate
// implies the index predicate. It cannot prove that about
// `status = ANY(ARRAY[$1,$2])`. Because pgx prepares its statements,
// Postgres is free to switch to a generic plan after the fifth run, and
// from then on every poll scans every partition.
//
// They are typed out rather than built from the status constants.
// TestTheClaimNamesTheStatusesItMeans catches a rename, and the query
// evaluator behind the schema and tenancy guards looks constants up by
// package name, which our aliased import would hide from it.
const claimDueSQL = `
UPDATE emails
SET status = ?, claimed_at = ?, attempts = attempts + 1
WHERE id IN (
    SELECT id FROM emails
    WHERE status IN ('queued', 'scheduled')
      AND next_attempt_at <= ?
    ORDER BY next_attempt_at ASC
    LIMIT ?
    FOR UPDATE SKIP LOCKED
)
RETURNING ` + emailColumns

// recoverStuckSQL returns rows abandoned in processing to the queue.
//
// The status it looks for is LITERAL, for the reason spelled out at
// length on claimDueSQL: it is the other query with no date predicate
// over a table partitioned by date, so it too depends on being able to
// use the PARTIAL idx_emails_queue, and a parameter is not something
// Postgres can prove anything about under a generic plan. `processing`
// is in that index's predicate precisely to serve this sweep.
//
// Typed out and covered by TestTheClaimNamesTheStatusesItMeans, for the
// reason on claimDueSQL above.
//
// A row handed to a PULL relay node is processing for the life of its
// assignment, which is longer than a claim - the node is delivering
// it, and taking it back while the node still holds it delivers it
// twice. So a live assignment excludes the row here, and the
// assignment sweep (relaynode) is what returns it when the node stops
// saying it has it.
const recoverStuckSQL = `
UPDATE emails
SET status = ?, claimed_at = NULL, next_attempt_at = ?
WHERE status = 'processing' AND claimed_at < ?
  AND NOT EXISTS (
      SELECT 1 FROM relay_assignments ra
      WHERE ra.email_id = emails.id AND ra.expires_at > ?
  )`

// addressMatchClause is the predicate that decides whether a stored
// message involves one address.
//
// Four branches, because these columns hold a mailbox rather than a bare
// address: withRegisteredName stores `"Acme" <no-reply@acme.com>` where
// a project registered a name, and a recipient is whatever the caller
// sent, so `Bob Smith <bob@x.test>` is an ordinary value. Matching only
// the bare forms makes erasure answer 200 with deleted:0 while the
// subject's mail and its attachment blobs stay.
//
// The angle brackets and quotes are load-bearing: they keep bob@x.test
// from matching notbob@x.test as a substring. EscapeLike stops the
// address carrying wildcards into the pattern. This statement deletes,
// so both matter.
//
// One clause shared by two statements, since the key-collecting query
// and the DELETE must select the same rows - a wider key set drops a
// blob a live row still names, a narrower one leaks the object.
//
// A package constant, so both statements stay compile-time constants and
// TestNoDynamicSQL can compute them. Returning it from a helper would
// make both unverifiable.
const addressMatchClause = `(LOWER(sender) = ?
                 OR LOWER(sender) LIKE ? ESCAPE '\'
                 OR LOWER(recipients) LIKE ? ESCAPE '\'
                 OR LOWER(recipients) LIKE ? ESCAPE '\')`

// pruneByID answers the partition window an id implies, for a lookup
// that would otherwise visit every one.
//
// `emails` is range partitioned by created_at, so `WHERE id = ?` on its
// own makes Postgres seek in every live partition. That is not a scan,
// but it is not free either - at 2M rows across 104 partitions a lookup
// by id costs around 488us against 15us when the window is named, and
// the cost grows with every partition added. The tracking pixel pays it
// on every open.
//
// A v7 id carries the millisecond it was minted, which is what makes
// this possible. It is only roughly created_at, though: the id is minted
// before the insert, and a caller setting created_at itself, an import
// or a fixture, can push the two arbitrarily far apart. So we keep the
// window generous and every caller falls back to the unpruned query when
// it finds nothing. The fast path is only a hint - correctness comes
// from the fallback.
func pruneByID(id string) (from, to time.Time, ok bool) {
	minted, ok := ids.MintedAt(id)
	if !ok {
		return time.Time{}, time.Time{}, false
	}

	return minted.Add(-idWindow), minted.Add(idWindow), true
}

// Get returns one email within projID, or nil when there is no such
// row.
func (s *Store) Get(ctx context.Context, projID, id string) (*emailmodel.Email, error) {
	if from, to, ok := pruneByID(id); ok {
		e, err := scanEmail(s.QueryRow(ctx,
			emailSelect+` WHERE project_id = ? AND id = ? AND created_at >= ? AND created_at < ?`,
			projID, id, from, to))
		if err == nil {
			return e, nil
		}

		if !errors.Is(err, sql.ErrNoRows) {
			return nil, err
		}
	}

	// Either the id carries no timestamp, or the row is outside the
	// window. Ask the whole table rather than answer "no such message"
	// on the strength of an approximation.
	row := s.QueryRow(ctx, emailSelect+` WHERE project_id = ? AND id = ?`, projID, id)
	e, err := scanEmail(row)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, nil
	}

	return e, err
}

// GetAny loads an email without a project scope. Reserved for the
// signed public tracking surface (the token IS the authorization).
//
// The hottest caller of the pruning above: this runs on every open and
// every click, unauthenticated, and the URL that reaches it never
// expires.
func (s *Store) GetAny(ctx context.Context, id string) (*emailmodel.Email, error) {
	if from, to, ok := pruneByID(id); ok {
		e, err := scanEmail(s.QueryRow(ctx,
			emailSelect+` WHERE id = ? AND created_at >= ? AND created_at < ?`, id, from, to))
		if err == nil {
			return e, nil
		}

		if !errors.Is(err, sql.ErrNoRows) {
			return nil, err
		}
	}

	row := s.QueryRow(ctx, emailSelect+` WHERE id = ?`, id)
	e, err := scanEmail(row)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, nil
	}

	return e, err
}

// List returns the project's emails, newest first.
//
// Search matches a RECIPIENT address or the SUBJECT, in two clauses
// because the columns are asked different questions. recipients is a
// JSON array and the needle is quoted (%"a@b.com"%) so a partial
// address cannot match. subject is prose, so a plain substring.
//
// Not the body: the largest column on the largest table, and a
// question nobody asks of a delivery log.
//
// The term goes through EscapeLike, or a bare % matches everything and
// reads as a broken filter.
func (s *Store) List(ctx context.Context, projID string, f Filter) ([]*emailmodel.Email, error) {
	query := emailSelect + ` WHERE project_id = ?`
	args := []any{projID}
	if f.Status != "" {
		query += ` AND status = ?`
		args = append(args, f.Status)
	}

	// The cursor is `(created_at, id)`, and the row-value comparison is
	// what makes it one condition rather than two that can disagree.
	// Without the id, a tie in created_at straddling a page boundary
	// SKIPS every row sharing that timestamp - they show on neither page,
	// so the log is missing a message that was sent. BeforeID may be
	// absent, which keeps a caller passing only `?before=` working.
	if f.Before != nil && f.BeforeID != "" {
		query += ` AND (created_at, id) < (?, ?)`
		args = append(args, *f.Before, f.BeforeID)
	} else if f.Before != nil {
		query += ` AND created_at < ?`
		args = append(args, *f.Before)
	}

	if term := strings.TrimSpace(f.Search); term != "" {
		query += ` AND (recipients LIKE ? ESCAPE '\' OR subject ILIKE ? ESCAPE '\')`
		escaped := database.EscapeLike(term)
		args = append(args, `%"`+escaped+`"%`, "%"+escaped+"%")
	}

	// id in the ORDER BY as well, or the cursor orders by something the
	// query does not - and the tie it exists to break is decided by
	// whatever the plan happens to return.
	query += ` ORDER BY created_at DESC, id DESC LIMIT ?`
	limit := f.Limit
	if limit <= 0 || limit > 200 {
		limit = 50
	}

	args = append(args, limit)

	rows, err := s.ReadQuery(ctx, query, args...)
	if err != nil {
		return nil, err
	}

	defer func() { _ = rows.Close() }()
	var out []*emailmodel.Email
	for rows.Next() {
		e, err := scanEmail(rows)
		if err != nil {
			return nil, err
		}

		out = append(out, e)
	}

	return out, rows.Err()
}

// Put inserts the email row (no upsert - rows are created once by
// the service, then mutated through the queue methods).
//
// It also counts the send into email_volume, IN THE SAME STATEMENT. Not a
// second Exec: two statements without a transaction lose the count if the
// process dies between them, and the count is what a plan limit reads. A
// data-modifying CTE makes the pair atomic for free.
func (s *Store) Put(ctx context.Context, e *emailmodel.Email) error {
	if e.CreatedAt.IsZero() {
		e.CreatedAt = time.Now().UTC()
	}

	_, err := s.Exec(ctx, `
        WITH mail AS (
        INSERT INTO emails (
            id, project_id, created_by, api_key_id, smtp_server_id, smtp_group_id, sender, recipients,
            subject, template_name, html_body, text_body, attachments_json, headers_json,
            list_unsubscribe_url, list_unsubscribe_mailto, list_unsubscribe_post, unsubscribe_list_id,
            status, error_message, attempts, max_attempts, next_attempt_at, claimed_at,
            created_at, scheduled_at, sent_at, tracked
        ) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
        RETURNING project_id, created_at
        )
        INSERT INTO email_volume (project_id, minute, accepted)
        SELECT project_id, date_trunc('minute', created_at), 1 FROM mail
        ON CONFLICT (project_id, minute) DO UPDATE
            SET accepted = email_volume.accepted + 1
    `,
		e.ID, e.ProjectID, e.CreatedBy, database.NullStr(e.APIKeyID),
		database.NullStr(e.SMTPServerID), database.NullStr(e.SMTPGroupID), e.Sender,
		database.MustJSON(e.Recipients), e.Subject, e.TemplateName, e.HTMLBody, e.TextBody,
		database.MustJSON(e.Attachments), database.MustJSON(e.Headers),
		e.ListUnsubscribeURL, e.ListUnsubscribeMailto, e.ListUnsubscribePost,
		database.NullStr(e.UnsubscribeListID),
		e.Status, e.ErrorMessage, e.Attempts, e.MaxAttempts,
		database.NullTime(e.NextAttemptAt), database.NullTime(e.ClaimedAt),
		e.CreatedAt, database.NullTime(e.ScheduledAt), database.NullTime(e.SentAt),
		e.Tracked,
	)

	return err
}

// Reset returns a failed row to the queue for a manual retry:
// attempts start over and the row is due immediately.
func (s *Store) Reset(ctx context.Context, projID, id string) (bool, error) {
	res, err := s.Exec(ctx, `
        UPDATE emails
        SET status = ?, error_message = '', attempts = 0, next_attempt_at = ?, claimed_at = NULL
        WHERE project_id = ? AND id = ? AND status = ?
    `, emailmodel.StatusQueued, time.Now().UTC(), projID, id, emailmodel.StatusFailed)
	if err != nil {
		return false, err
	}

	n, err := res.RowsAffected()

	return n > 0, err
}

// CountByStatus powers the dashboard-style summary.
func (s *Store) CountByStatus(ctx context.Context, projID string) (map[string]int, error) {
	rows, err := s.ReadQuery(ctx, `SELECT status, COUNT(*) FROM emails WHERE project_id = ? GROUP BY status`, projID)
	if err != nil {
		return nil, err
	}

	defer func() { _ = rows.Close() }()
	out := map[string]int{}
	for rows.Next() {
		var status string
		var n int
		if err := rows.Scan(&status, &n); err != nil {
			return nil, err
		}

		out[status] = n
	}

	return out, rows.Err()
}

// ----------------------------------------------------------------------------
// Queue source
// ----------------------------------------------------------------------------
// Not project-scoped: the worker drains every tenant.

// ClaimDue takes up to limit due messages for this node in one
// statement, leaving them locked to it. Never on a read replica.
func (s *Store) ClaimDue(ctx context.Context, now time.Time, limit int) ([]*emailmodel.Email, error) {
	rows, err := s.Query(ctx, claimDueSQL,
		emailmodel.StatusProcessing, now, now, limit)
	if err != nil {
		return nil, err
	}

	defer func() { _ = rows.Close() }()

	var claimed []*emailmodel.Email
	for rows.Next() {
		e, err := scanEmail(rows)
		if err != nil {
			return nil, err
		}

		claimed = append(claimed, e)
	}

	// Rows are already claimed at this point, so a scan or iteration
	// error must not be swallowed: returning them as unclaimed would
	// leave the batch stranded in processing until RecoverStuck.
	return claimed, rows.Err()
}

// Requeue returns a transiently-failed row to the queue.
//
// created_at is in the predicate because the table is partitioned by
// it: without it Postgres has to visit every live partition to find
// one row. The worker holds the row it claimed, so it costs nothing
// to say which week the row is in.
func (s *Store) Requeue(ctx context.Context, id string, createdAt time.Time, next time.Time, errMsg string) error {
	_, err := s.Exec(ctx, `
        UPDATE emails
        SET status = ?, next_attempt_at = ?, error_message = ?, claimed_at = NULL
        WHERE id = ? AND created_at = ?
    `, emailmodel.StatusQueued, next, errMsg, id, createdAt)

	return err
}

// Finalize writes the terminal state. See Requeue for why created_at
// is in the predicate.
//
// delivered_via is written with COALESCE-style care: an empty value
// leaves whatever is there rather than clearing it. A message that
// succeeded and was then finalized again - a retry path, a recovery
// sweep - must not lose the record of which server carried it.
func (s *Store) Finalize(ctx context.Context, id string, createdAt time.Time, status, errMsg, deliveredVia string, sentAt *time.Time) error {
	_, err := s.Exec(ctx, `
        UPDATE emails
        SET status = ?, error_message = ?, sent_at = ?,
            delivered_via = CASE WHEN ? = '' THEN delivered_via ELSE ? END,
            claimed_at = NULL, next_attempt_at = NULL
        WHERE id = ? AND created_at = ?
    `, status, errMsg, database.NullTime(sentAt), deliveredVia, deliveredVia, id, createdAt)

	return err
}

// RecoverStuck returns messages abandoned mid-flight - a node that
// died holding a claim - to the queue.
func (s *Store) RecoverStuck(ctx context.Context, olderThan time.Time) (int, error) {
	now := time.Now().UTC()
	res, err := s.Exec(ctx, recoverStuckSQL,
		emailmodel.StatusQueued, now, olderThan, now)
	if err != nil {
		return 0, err
	}

	n, err := res.RowsAffected()

	return int(n), err
}

func scanEmail(r interface{ Scan(...any) error }) (*emailmodel.Email, error) {
	var e emailmodel.Email
	var recipients, attachments, headers string
	var nextAt, claimedAt, scheduledAt, sentAt sql.NullTime
	var openedAt, clickedAt sql.NullTime
	if err := r.Scan(&e.ID, &e.ProjectID, &e.CreatedBy, database.Str(&e.APIKeyID),
		database.Str(&e.SMTPServerID), database.Str(&e.SMTPGroupID),
		&e.Sender, &recipients, &e.Subject, &e.TemplateName, &e.HTMLBody, &e.TextBody,
		&attachments, &headers,
		&e.ListUnsubscribeURL, &e.ListUnsubscribeMailto, &e.ListUnsubscribePost,
		database.Str(&e.UnsubscribeListID),
		&e.Status, &e.ErrorMessage, &e.Attempts, &e.MaxAttempts,
		&nextAt, &claimedAt, &e.CreatedAt, &scheduledAt, &sentAt,
		&e.Tracked, &openedAt, &clickedAt, &e.OpenCount, &e.ClickCount,
		&e.DeliveredVia); err != nil {
		return nil, err
	}

	database.MustUnmarshalJSON(recipients, &e.Recipients)
	database.MustUnmarshalJSON(attachments, &e.Attachments)
	database.MustUnmarshalJSON(headers, &e.Headers)
	if nextAt.Valid {
		e.NextAttemptAt = new(nextAt.Time)
	}

	if claimedAt.Valid {
		e.ClaimedAt = new(claimedAt.Time)
	}

	if openedAt.Valid {
		e.OpenedAt = new(openedAt.Time)
	}

	if clickedAt.Valid {
		e.ClickedAt = new(clickedAt.Time)
	}

	if scheduledAt.Valid {
		e.ScheduledAt = new(scheduledAt.Time)
	}

	if sentAt.Valid {
		e.SentAt = new(sentAt.Time)
	}

	return &e, nil
}

// AcceptedSince sums the per-minute counter over a window.
//
// What the plan limits read. It replaced COUNT(*) over the emails
// table, 14-30ms per send at 1.2M rows and growing with the project's
// own volume, with a range over at most 1440 tiny rows.
//
// Truncated to the minute, so the answer can include up to a minute
// more than an exact "since" would - see the migration for why the
// buckets are minutes.
func (s *Store) AcceptedSince(ctx context.Context, projID string, since time.Time) (int, error) {
	var n int
	err := s.QueryRow(ctx, `
        SELECT COALESCE(SUM(accepted), 0) FROM email_volume
        WHERE project_id = ? AND minute >= date_trunc('minute', ?::timestamptz)`,
		projID, since.UTC()).Scan(&n)

	return n, err
}

// PruneVolumeBefore drops counter rows the windows can no longer read.
//
// The longest window is 24 hours, so anything older than two days is
// dead weight - and this table gets a row per project per active minute,
// which is 1440 a day for a project that never sleeps.
func (s *Store) PruneVolumeBefore(ctx context.Context, before time.Time) (int64, error) {
	res, err := s.Exec(ctx, `DELETE FROM email_volume WHERE minute < ?`, before.UTC())
	if err != nil {
		return 0, err
	}

	return res.RowsAffected()
}

// CountCreatedSince counts rows created in the window.
//
// Not what plan volume limits read any more - that is quota's
// AcceptedSince over email_volume, which counts in the same statement as
// the insert. Nothing calls this, and the index that would serve it is
// idx_emails_proj_created.
func (s *Store) CountCreatedSince(ctx context.Context, projID string, since time.Time) (int, error) {
	var n int
	err := s.QueryRow(ctx, `SELECT COUNT(*) FROM emails WHERE project_id = ? AND created_at >= ?`, projID, since.UTC()).Scan(&n)

	return n, err
}

// CountAllByStatus counts email rows by status across every
// project, for the metrics scrape gauge.
func (s *Store) CountAllByStatus(ctx context.Context) (map[string]int, error) {
	rows, err := s.ReadQuery(ctx, `SELECT status, COUNT(*) FROM emails GROUP BY status`)
	if err != nil {
		return nil, err
	}

	defer func() { _ = rows.Close() }()
	out := map[string]int{}
	for rows.Next() {
		var st string
		var n int
		if err := rows.Scan(&st, &n); err != nil {
			return nil, err
		}

		out[st] = n
	}

	return out, rows.Err()
}

// ----------------------------------------------------------------------------
// Retention
// ----------------------------------------------------------------------------
//
// The retention sweep runs unscoped: it is a platform maintenance
// job, not a tenant operation. Every statement is a delete or update
// by age, so two nodes running it at once converge rather than
// conflict.

// StorageKeysOlderThan collects the blob keys referenced by emails
// created before the cutoff, so the caller can drop the objects
// before the rows that point at them. Rows with inline attachments
// contribute nothing.
func (s *Store) StorageKeysOlderThan(ctx context.Context, before time.Time) ([]string, error) {
	// The in-flight exemption has to match ClearAttachmentsOlderThan
	// and PurgeOlderThan exactly. Collecting a wider set than the
	// statement that clears the rows means deleting blobs still
	// referenced by a live row: a message scheduled past the
	// attachment window kept its storage_key while its object was
	// dropped, so the send failed on a file that no longer existed.
	rows, err := s.Query(ctx, `
        SELECT attachments_json FROM emails
        WHERE created_at < ? AND status NOT IN (?, ?, ?)
          AND attachments_json <> '[]' AND attachments_json <> ''`,
		before, emailmodel.StatusQueued, emailmodel.StatusScheduled, emailmodel.StatusProcessing)
	if err != nil {
		return nil, err
	}

	return storageKeys(rows)
}

// storageKeys reads offloaded blob keys out of a cursor over
// attachments_json. Shared by the four key queries because the loop is
// the same and the WHERE clauses are not - and because a fifth copy is
// how one of them ends up scanning a column the others do not.
func storageKeys(rows *sql.Rows) ([]string, error) {
	defer func() { _ = rows.Close() }()
	var keys []string
	for rows.Next() {
		var raw string
		if err := rows.Scan(&raw); err != nil {
			return nil, err
		}

		var atts []emailmodel.Attachment
		database.MustUnmarshalJSON(raw, &atts)
		for _, a := range atts {
			if a.StorageKey != "" {
				keys = append(keys, a.StorageKey)
			}
		}
	}

	return keys, rows.Err()
}

// StorageKeysForProject collects every offloaded blob key the project
// owns, in-flight rows included.
//
// For project DELETION, where the email rows go by cascade rather than
// by a statement of ours - so there is no in-flight exemption to
// mirror. Nothing is left behind to send.
func (s *Store) StorageKeysForProject(ctx context.Context, projID string) ([]string, error) {
	rows, err := s.Query(ctx, `
        SELECT attachments_json FROM emails
        WHERE project_id = ?
          AND attachments_json <> '[]' AND attachments_json <> ''`, projID)
	if err != nil {
		return nil, err
	}

	return storageKeys(rows)
}

// StorageKeysForProjectOlderThan collects the keys PurgeProjectOlderThan
// is about to make unreachable.
//
// The WHERE clause mirrors that statement exactly, for the reason
// StorageKeysOlderThan gives: a wider set here drops an object a live
// row still points at.
func (s *Store) StorageKeysForProjectOlderThan(ctx context.Context, projID string, before time.Time) ([]string, error) {
	rows, err := s.Query(ctx, `
        SELECT attachments_json FROM emails
        WHERE project_id = ? AND created_at < ? AND status NOT IN (?, ?, ?)
          AND attachments_json <> '[]' AND attachments_json <> ''`,
		projID, before, emailmodel.StatusQueued, emailmodel.StatusScheduled, emailmodel.StatusProcessing)
	if err != nil {
		return nil, err
	}

	return storageKeys(rows)
}

// StorageKeysForAddress collects the keys PurgeForAddress is about to
// make unreachable. Same match, same exemption, same reason - both use
// addressMatchClause so they cannot drift.
func (s *Store) StorageKeysForAddress(ctx context.Context, projID, email string) ([]string, error) {
	rows, err := s.Query(ctx, `
        SELECT attachments_json FROM emails
        WHERE project_id = ? AND status NOT IN (?, ?, ?)
          AND `+addressMatchClause+`
          AND attachments_json <> '[]' AND attachments_json <> ''`,
		addressNeedles(projID, email)...)
	if err != nil {
		return nil, err
	}

	return storageKeys(rows)
}

// PurgeOlderThan deletes email rows created before the cutoff.
// Rows still in flight (queued, scheduled, processing) are left
// alone however old they are - deleting one would strand work the
// queue is about to claim.
func (s *Store) PurgeOlderThan(ctx context.Context, before time.Time) (int64, error) {
	res, err := s.Exec(ctx, `
        DELETE FROM emails
        WHERE created_at < ? AND status NOT IN (?, ?, ?)`,
		before, emailmodel.StatusQueued, emailmodel.StatusScheduled, emailmodel.StatusProcessing)
	if err != nil {
		return 0, err
	}

	return res.RowsAffected()
}

// ClearBodiesOlderThan blanks rendered content while keeping the
// delivery record. Same in-flight exemption as PurgeOlderThan: the
// worker still needs the body to send.
func (s *Store) ClearBodiesOlderThan(ctx context.Context, before time.Time) (int64, error) {
	res, err := s.Exec(ctx, `
        UPDATE emails SET html_body = '', text_body = ''
        WHERE created_at < ? AND status NOT IN (?, ?, ?)
          AND (html_body <> '' OR text_body <> '')`,
		before, emailmodel.StatusQueued, emailmodel.StatusScheduled, emailmodel.StatusProcessing)
	if err != nil {
		return 0, err
	}

	return res.RowsAffected()
}

// ClearAttachmentsOlderThan drops attachment metadata and inline
// bytes. Callers delete the blob objects first (see
// StorageKeysOlderThan) - once this runs the keys are unrecoverable.
func (s *Store) ClearAttachmentsOlderThan(ctx context.Context, before time.Time) (int64, error) {
	res, err := s.Exec(ctx, `
        UPDATE emails SET attachments_json = '[]'
        WHERE created_at < ? AND status NOT IN (?, ?, ?)
          AND attachments_json <> '[]' AND attachments_json <> ''`,
		before, emailmodel.StatusQueued, emailmodel.StatusScheduled, emailmodel.StatusProcessing)
	if err != nil {
		return 0, err
	}

	return res.RowsAffected()
}

// PurgeProjectOlderThan erases delivery records for one project,
// for the erasure surface. Same in-flight exemption as the retention
// sweep: a queued or processing row is work the queue still owns.
func (s *Store) PurgeProjectOlderThan(ctx context.Context, projID string, before time.Time) (int64, error) {
	res, err := s.Exec(ctx, `
        DELETE FROM emails
        WHERE project_id = ? AND created_at < ? AND status NOT IN (?, ?, ?)`,
		projID, before, emailmodel.StatusQueued, emailmodel.StatusScheduled, emailmodel.StatusProcessing)
	if err != nil {
		return 0, err
	}

	return res.RowsAffected()
}

// addressNeedles is every argument the two address statements take, in
// order: the project, the three exempt statuses, then the four forms
// addressMatchClause matches. Lowercased HERE, because the clause
// compares LOWER(column): a caller that does not fold gets a DELETE
// that matches nothing and reports deleted: 0 - the quiet failure the
// erasure path exists to not have.
func addressNeedles(projID, email string) []any {
	email = strings.ToLower(email)
	esc := database.EscapeLike(email)

	return []any{
		projID, emailmodel.StatusQueued, emailmodel.StatusScheduled, emailmodel.StatusProcessing,
		email, `%<` + esc + `>%`, `%"` + esc + `"%`, `%<` + esc + `>%`,
	}
}

// PurgeForAddress erases records where the address is the sender or
// appears among the recipients. See addressMatchClause for what
// "involves" means and why it is not just an equality.
func (s *Store) PurgeForAddress(ctx context.Context, projID, email string) (int64, error) {
	res, err := s.Exec(ctx, `
        DELETE FROM emails
        WHERE project_id = ? AND status NOT IN (?, ?, ?)
          AND `+addressMatchClause, addressNeedles(projID, email)...)
	if err != nil {
		return 0, err
	}

	return res.RowsAffected()
}

// ----------------------------------------------------------------------------
// Tracking
// ----------------------------------------------------------------------------

// MarkOpened records an open and reports whether it was the first.
//
// Unique opens are the number people quote; total opens count a reader
// who reopened the message weeks later. One statement returns both by
// looking at whether opened_at was already set.
//
// The count is returned because the caller needs it to decide whether to
// record another event row. The tracking pixel is unauthenticated and
// its URL sits in the recipient's mailbox forever, so it can be replayed
// without limit - the counter costs nothing to increment repeatedly, the
// events table does. See trackedEventsPerEmail.
//
// createdAt is in the predicate for the same reason Requeue and Finalize
// take it. The table is range partitioned by that column, so without it
// Postgres opens an Update node on every live partition - roughly 792us
// per update against 75us at a hundred partitions, on the hottest
// unauthenticated path we have. The tracking handler has already read
// the row, so this is that row's created_at rather than a guess.
func (s *Store) MarkOpened(ctx context.Context, id string, createdAt, at time.Time) (bool, int64, error) {
	var (
		first bool
		count int64
	)
	err := s.QueryRow(ctx, `
        UPDATE emails
        SET opened_at  = COALESCE(opened_at, ?),
            open_count = open_count + 1
        WHERE id = ? AND created_at = ?
        RETURNING opened_at = ?, open_count
    `, at, id, createdAt, at).Scan(&first, &count)
	if errors.Is(err, sql.ErrNoRows) {
		return false, 0, nil
	}

	return first, count, err
}

// MarkClicked records a click against the email. A click implies the
// message was opened - some clients fetch no images at all, so the
// pixel may never have fired - which is why opened_at is filled here
// too rather than left empty on a message somebody demonstrably read.
// The click count comes back for the same reason MarkOpened's does: the
// caller uses it to stop recording event rows for a URL being replayed.
// createdAt is in the predicate for the partition reason too.
func (s *Store) MarkClicked(ctx context.Context, id string, createdAt, at time.Time) (int64, error) {
	var count int64
	err := s.QueryRow(ctx, `
        UPDATE emails
        SET clicked_at  = COALESCE(clicked_at, ?),
            opened_at   = COALESCE(opened_at, ?),
            click_count = click_count + 1
        WHERE id = ? AND created_at = ?
        RETURNING click_count
    `, at, at, id, createdAt).Scan(&count)
	if errors.Is(err, sql.ErrNoRows) {
		return 0, nil
	}

	return count, err
}
