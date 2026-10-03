// Mailyard, Copyright (c) 2021-2026 YouSysAdmin

package template

import (
	"context"
	"database/sql"
	"errors"
	"log/slog"
	"strings"
	"time"

	"github.com/jackc/pgx/v5/pgconn"

	"github.com/yousysadmin/mailyard/internal/database"
	"github.com/yousysadmin/mailyard/internal/domain/store"
	tmodel "github.com/yousysadmin/mailyard/internal/models/template"
)

// Store persists templates, their versions and localizations. Project
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

// ----------------------------------------------------------------------------
// Templates
// ----------------------------------------------------------------------------
const templateSelect = `
SELECT id, project_id, name, description, default_language, active_version_id,
       sample_data, created_by, last_edited_by, created_at, updated_at
FROM templates`

// Get returns one template within projID, or nil when there is no such
// row.
func (s *Store) Get(ctx context.Context, projID, id string) (*tmodel.Template, error) {
	row := s.QueryRow(ctx, templateSelect+` WHERE project_id = ? AND id = ?`, projID, id)
	t, err := scanTemplate(row)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, nil
	}

	return t, err
}

// GetByName returns one template by name within projID, or nil when
// there is no such row. Names compare without regard to case.
func (s *Store) GetByName(ctx context.Context, projID, name string) (*tmodel.Template, error) {
	row := s.QueryRow(ctx, templateSelect+` WHERE project_id = ? AND lower(name) = lower(?)`, projID, name)
	t, err := scanTemplate(row)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, nil
	}

	return t, err
}

// Find is List narrowed by f, with the count of what f matches.
func (s *Store) Find(ctx context.Context, projID string, f store.ListFilter) ([]*tmodel.Template, int, error) {
	var where strings.Builder
	where.WriteString(` WHERE project_id = ?`)
	args := []any{projID}
	if f.Query != "" {
		where.WriteString(` AND name ILIKE ? ESCAPE '\'`)
		args = append(args, "%"+database.EscapeLike(f.Query)+"%")
	}

	var total int
	if err := s.QueryRow(ctx, `SELECT COUNT(*) FROM templates`+where.String(), args...).Scan(&total); err != nil {
		return nil, 0, err
	}

	var sb strings.Builder
	sb.WriteString(templateSelect)
	sb.WriteString(where.String())
	sb.WriteString(` ORDER BY name ASC`)
	if f.Limit > 0 {
		sb.WriteString(` LIMIT ? OFFSET ?`)
		args = append(args, f.Limit, f.Offset)
	}

	rows, err := s.Query(ctx, sb.String(), args...)
	if err != nil {
		return nil, 0, err
	}

	defer func() { _ = rows.Close() }()
	out := []*tmodel.Template{}
	for rows.Next() {
		t, err := scanTemplate(rows)
		if err != nil {
			return nil, 0, err
		}

		out = append(out, t)
	}

	return out, total, rows.Err()
}

// List returns every template in projID.
func (s *Store) List(ctx context.Context, projID string) ([]*tmodel.Template, error) {
	rows, err := s.Query(ctx, templateSelect+` WHERE project_id = ? ORDER BY name ASC`, projID)
	if err != nil {
		return nil, err
	}

	defer func() { _ = rows.Close() }()
	var out []*tmodel.Template
	for rows.Next() {
		t, err := scanTemplate(rows)
		if err != nil {
			return nil, err
		}

		out = append(out, t)
	}

	return out, rows.Err()
}

// Put inserts the template, or updates the row when its id already
// exists. It never writes active_version_id, which SetActiveVersion
// and Create own: a caller holding a row read before an activation
// would otherwise put the old version back.
func (s *Store) Put(ctx context.Context, t *tmodel.Template) error {
	if t.CreatedAt.IsZero() {
		t.CreatedAt = time.Now().UTC()
	}

	_, err := s.Exec(ctx, `
        INSERT INTO templates (
            id, project_id, name, description, default_language,
            sample_data, created_by, last_edited_by, created_at, updated_at
        ) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
        ON CONFLICT(id) DO UPDATE SET
            name              = excluded.name,
            description       = excluded.description,
            default_language  = excluded.default_language,
            sample_data       = excluded.sample_data,
            last_edited_by    = excluded.last_edited_by,
            updated_at        = excluded.updated_at
    `,
		t.ID, t.ProjectID, t.Name, t.Description, t.DefaultLanguage,
		t.SampleData, t.CreatedBy, t.LastEditedBy,
		t.CreatedAt, database.NullTime(t.UpdatedAt),
	)

	return err
}

// Update writes the fields p names and nothing else, so two
// concurrent updates of different fields both land and neither puts
// back a value the other changed. It reports whether the template
// exists.
func (s *Store) Update(ctx context.Context, projID, id string, p *tmodel.Patch) (bool, error) {
	res, err := s.Exec(ctx, `
        UPDATE templates SET
            name             = COALESCE(?, name),
            description      = COALESCE(?, description),
            default_language = COALESCE(?, default_language),
            sample_data      = COALESCE(?, sample_data),
            last_edited_by   = ?,
            updated_at       = ?
        WHERE project_id = ? AND id = ?
    `, p.Name, p.Description, p.DefaultLanguage, p.SampleData, p.LastEditedBy,
		time.Now().UTC(), projID, id)
	if err != nil {
		return false, err
	}

	n, err := res.RowsAffected()

	return n > 0, err
}

// Create writes a template together with its versions, their
// localizations and stylesheets, and the active version, in one
// transaction: a failure part way leaves nothing behind, so the same
// request can be sent again. Zero version numbers are assigned after
// the highest one the drafts name, and the counter starts there.
func (s *Store) Create(ctx context.Context, t *tmodel.Template, drafts []*tmodel.Draft) error {
	if t.CreatedAt.IsZero() {
		t.CreatedAt = time.Now().UTC()
	}

	last := 0
	for _, d := range drafts {
		last = max(last, d.Version.Version)
	}

	for _, d := range drafts {
		if d.Version.Version == 0 {
			last++
			d.Version.Version = last
		}
	}

	tx, err := s.DB().BeginTx(ctx, nil)
	if err != nil {
		return err
	}

	defer func() {
		if rerr := tx.Rollback(); rerr != nil && !errors.Is(rerr, sql.ErrTxDone) {
			slog.Warn("store: rollback failed", "err", rerr)
		}
	}()

	if _, err := tx.ExecContext(ctx, s.Q(`
        INSERT INTO templates (
            id, project_id, name, description, default_language,
            sample_data, created_by, last_edited_by, created_at, last_version
        ) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
    `), t.ID, t.ProjectID, t.Name, t.Description, t.DefaultLanguage,
		t.SampleData, t.CreatedBy, t.LastEditedBy, t.CreatedAt, last); err != nil {
		return err
	}

	var active string
	for _, d := range drafts {
		v := d.Version
		v.TemplateID = t.ID
		if v.CreatedAt.IsZero() {
			v.CreatedAt = t.CreatedAt
		}

		if sh := d.Stylesheet; sh != nil {
			if sh.CreatedAt.IsZero() {
				sh.CreatedAt = t.CreatedAt
			}

			if _, err := tx.ExecContext(ctx, s.Q(`
                INSERT INTO stylesheets (id, project_id, name, css, created_at)
                VALUES (?, ?, ?, ?, ?)
            `), sh.ID, t.ProjectID, sh.Name, sh.CSS, sh.CreatedAt); err != nil {
				return err
			}

			v.StylesheetID = new(sh.ID)
		}

		if _, err := tx.ExecContext(ctx, s.Q(`
            INSERT INTO template_versions (id, template_id, version, stylesheet_id, sample_data, created_at)
            VALUES (?, ?, ?, ?, ?, ?)
        `), v.ID, v.TemplateID, v.Version, nullPtr(v.StylesheetID), v.SampleData, v.CreatedAt); err != nil {
			return err
		}

		for _, l := range d.Localizations {
			l.VersionID = v.ID
			if l.CreatedAt.IsZero() {
				l.CreatedAt = t.CreatedAt
			}

			if _, err := tx.ExecContext(ctx, s.Q(`
                INSERT INTO template_localizations (
                    id, version_id, language, subject_template, html_template, text_template, created_at
                ) VALUES (?, ?, ?, ?, ?, ?, ?)
            `), l.ID, l.VersionID, l.Language, l.Subject, l.HTML, l.Text, l.CreatedAt); err != nil {
				return err
			}
		}

		if d.Active {
			active = v.ID
		}
	}

	if active != "" {
		if _, err := tx.ExecContext(ctx, s.Q(`
            UPDATE templates SET active_version_id = ? WHERE project_id = ? AND id = ?
        `), active, t.ProjectID, t.ID); err != nil {
			return err
		}
	}

	if err := tx.Commit(); err != nil {
		return err
	}

	t.ActiveVersionID = nil
	if active != "" {
		t.ActiveVersionID = new(active)
	}

	return nil
}

// CampaignUsing names a campaign that will still render the template:
// one not yet sent or cancelled that uses it, directly or as an A/B
// variant. Empty when there is none.
func (s *Store) CampaignUsing(ctx context.Context, projID, templateID string) (string, error) {
	var name string
	err := s.QueryRow(ctx, `
        SELECT name FROM campaigns
        WHERE project_id = ?
          AND status IN ('draft', 'scheduled', 'sending', 'paused')
          AND (template_id = ?
               OR ab_variants::jsonb @> jsonb_build_array(jsonb_build_object('template_id', ?::text)))
        ORDER BY created_at
        LIMIT 1
    `, projID, templateID, templateID).Scan(&name)
	if errors.Is(err, sql.ErrNoRows) {
		return "", nil
	}

	return name, err
}

// Delete removes one template from projID.
//
// Its attachments are marked deleted in the same statement and kept,
// detached by the foreign key, for the messages that reference them.
func (s *Store) Delete(ctx context.Context, projID, id string) error {
	_, err := s.Exec(ctx, `
        WITH detached AS (
            UPDATE template_attachments SET deleted_at = now()
            WHERE project_id = ? AND template_id = ? AND deleted_at IS NULL
        )
        DELETE FROM templates WHERE project_id = ? AND id = ?`, projID, id, projID, id)

	return err
}

// SetActiveVersion pins versionID as the template's active revision.
// The subquery double-checks the version belongs to this template so
// a foreign version id cannot be activated.
func (s *Store) SetActiveVersion(ctx context.Context, projID, id, versionID string) error {
	res, err := s.Exec(ctx, `
        UPDATE templates SET active_version_id = ?
        WHERE project_id = ? AND id = ?
          AND EXISTS (SELECT 1 FROM template_versions v WHERE v.id = ? AND v.template_id = templates.id)
    `, versionID, projID, id, versionID)
	if err != nil {
		return err
	}

	n, err := res.RowsAffected()
	if err != nil {
		return err
	}

	if n == 0 {
		return ErrVersionMismatch
	}

	return nil
}

// ----------------------------------------------------------------------------
// Versions
// ----------------------------------------------------------------------------

// ErrVersionMismatch is returned when an activate targets a version
// that does not belong to the template (or the template is missing).
var ErrVersionMismatch = errors.New("version does not belong to this template")

const versionSelect = `
SELECT v.id, v.template_id, v.version, v.stylesheet_id, v.sample_data, v.created_at
FROM template_versions v
JOIN templates t ON t.id = v.template_id`

// GetVersion returns one version within projID, or nil when there is
// no such row.
func (s *Store) GetVersion(ctx context.Context, projID, templateID, versionID string) (*tmodel.Version, error) {
	row := s.QueryRow(ctx, versionSelect+`
        WHERE t.project_id = ? AND v.template_id = ? AND v.id = ?`,
		projID, templateID, versionID)
	v, err := scanVersion(row)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, nil
	}

	return v, err
}

// ListVersions returns the versions in projID.
func (s *Store) ListVersions(ctx context.Context, projID, templateID string) ([]*tmodel.Version, error) {
	rows, err := s.Query(ctx, versionSelect+`
        WHERE t.project_id = ? AND v.template_id = ? ORDER BY v.version ASC`,
		projID, templateID)
	if err != nil {
		return nil, err
	}

	defer func() { _ = rows.Close() }()
	var out []*tmodel.Version
	for rows.Next() {
		v, err := scanVersion(rows)
		if err != nil {
			return nil, err
		}

		out = append(out, v)
	}

	return out, rows.Err()
}

// PutVersion upserts a version. A zero Version number is assigned the
// next number from the template's counter, which only goes up, so a
// number is never handed out twice even after the highest version is
// deleted. The project check runs first so a guessed template id in
// another tenant is a no-op.
func (s *Store) PutVersion(ctx context.Context, projID string, v *tmodel.Version) error {
	var owned int
	err := s.QueryRow(ctx, `SELECT COUNT(*) FROM templates WHERE project_id = ? AND id = ?`,
		projID, v.TemplateID).Scan(&owned)
	if err != nil {
		return err
	}

	if owned == 0 {
		return sql.ErrNoRows
	}

	if v.CreatedAt.IsZero() {
		v.CreatedAt = time.Now().UTC()
	}

	if v.Version == 0 {
		err := s.QueryRow(ctx, `
            UPDATE templates SET last_version = last_version + 1
            WHERE project_id = ? AND id = ?
            RETURNING last_version`, projID, v.TemplateID).Scan(&v.Version)
		if err != nil {
			return err
		}
	}

	_, err = s.Exec(ctx, `
        INSERT INTO template_versions (id, template_id, version, stylesheet_id, sample_data, created_at)
        VALUES (?, ?, ?, ?, ?, ?)
        ON CONFLICT(id) DO UPDATE SET
            stylesheet_id = excluded.stylesheet_id,
            sample_data   = excluded.sample_data
    `, v.ID, v.TemplateID, v.Version, nullPtr(v.StylesheetID), v.SampleData, v.CreatedAt)

	return err
}

// ErrVersionActive is a delete refused because the version is the
// one the template sends.
var ErrVersionActive = errors.New("this version is active")

// DeleteVersion removes one version from projID, refusing the active
// one. The statement skips it and the foreign key on
// active_version_id refuses one activated while the delete ran.
func (s *Store) DeleteVersion(ctx context.Context, projID, templateID, versionID string) error {
	res, err := s.Exec(ctx, `
        DELETE FROM template_versions
        WHERE id = ? AND template_id = ?
          AND EXISTS (SELECT 1 FROM templates t WHERE t.id = ? AND t.project_id = ?)
          AND NOT EXISTS (SELECT 1 FROM templates t WHERE t.id = ? AND t.active_version_id = template_versions.id)
    `, versionID, templateID, templateID, projID, templateID)
	if pg, ok := errors.AsType[*pgconn.PgError](err); ok && pg.Code == "23503" {
		return ErrVersionActive
	}

	if err != nil {
		return err
	}

	n, err := res.RowsAffected()
	if err != nil {
		return err
	}

	if n == 0 {
		return ErrVersionActive
	}

	return nil
}

// ----------------------------------------------------------------------------
// Localizations
// ----------------------------------------------------------------------------
const localizationSelect = `
SELECT l.id, l.version_id, l.language, l.subject_template, l.html_template,
       l.text_template, l.created_at, l.updated_at
FROM template_localizations l
JOIN template_versions v ON v.id = l.version_id
JOIN templates t ON t.id = v.template_id`

// GetLocalization returns one localization within projID, or nil when
// there is no such row.
func (s *Store) GetLocalization(ctx context.Context, projID, versionID, language string) (*tmodel.Localization, error) {
	row := s.QueryRow(ctx, localizationSelect+`
        WHERE t.project_id = ? AND l.version_id = ? AND l.language = ?`,
		projID, versionID, language)
	l, err := scanLocalization(row)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, nil
	}

	return l, err
}

// GetLocalizationByID returns one localization of templateID by id
// within projID, or nil when there is no such row.
func (s *Store) GetLocalizationByID(ctx context.Context, projID, templateID, id string) (*tmodel.Localization, error) {
	row := s.QueryRow(ctx, localizationSelect+`
        WHERE t.project_id = ? AND t.id = ? AND l.id = ?`, projID, templateID, id)
	l, err := scanLocalization(row)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, nil
	}

	return l, err
}

// ListLocalizations returns the localizations in projID.
func (s *Store) ListLocalizations(ctx context.Context, projID, versionID string) ([]*tmodel.Localization, error) {
	rows, err := s.Query(ctx, localizationSelect+`
        WHERE t.project_id = ? AND l.version_id = ? ORDER BY l.language ASC`,
		projID, versionID)
	if err != nil {
		return nil, err
	}

	defer func() { _ = rows.Close() }()
	var out []*tmodel.Localization
	for rows.Next() {
		l, err := scanLocalization(rows)
		if err != nil {
			return nil, err
		}

		out = append(out, l)
	}

	return out, rows.Err()
}

// PutLocalization upserts by id after confirming the version chain
// belongs to the project.
func (s *Store) PutLocalization(ctx context.Context, projID string, l *tmodel.Localization) error {
	var owned int
	err := s.QueryRow(ctx, `
        SELECT COUNT(*) FROM template_versions v
        JOIN templates t ON t.id = v.template_id
        WHERE t.project_id = ? AND v.id = ?`,
		projID, l.VersionID).Scan(&owned)
	if err != nil {
		return err
	}

	if owned == 0 {
		return sql.ErrNoRows
	}

	if l.CreatedAt.IsZero() {
		l.CreatedAt = time.Now().UTC()
	}

	_, err = s.Exec(ctx, `
        INSERT INTO template_localizations (
            id, version_id, language, subject_template, html_template, text_template,
            created_at, updated_at
        ) VALUES (?, ?, ?, ?, ?, ?, ?, ?)
        ON CONFLICT(version_id, language) DO UPDATE SET
            subject_template = excluded.subject_template,
            html_template    = excluded.html_template,
            text_template    = excluded.text_template,
            updated_at       = excluded.updated_at
    `, l.ID, l.VersionID, l.Language, l.Subject, l.HTML, l.Text,
		l.CreatedAt, database.NullTime(l.UpdatedAt))

	return err
}

// ErrLastLocalization is a delete refused because it would leave the
// active version with nothing to send.
var ErrLastLocalization = errors.New("the active version would have no localization left")

// DeleteLocalization removes one localization of templateID from
// projID. The last localization of the active version is refused:
// every send of the template would fail. The version row is locked so
// two deletes of its last two localizations cannot both pass.
func (s *Store) DeleteLocalization(ctx context.Context, projID, templateID, id string) error {
	tx, err := s.DB().BeginTx(ctx, nil)
	if err != nil {
		return err
	}

	defer func() {
		if rerr := tx.Rollback(); rerr != nil && !errors.Is(rerr, sql.ErrTxDone) {
			slog.Warn("store: rollback failed", "err", rerr)
		}
	}()

	var versionID string
	var active, others bool
	err = tx.QueryRowContext(ctx, s.Q(`
        SELECT v.id,
               COALESCE(t.active_version_id = v.id, FALSE),
               EXISTS (SELECT 1 FROM template_localizations o WHERE o.version_id = v.id AND o.id <> l.id)
        FROM template_localizations l
        JOIN template_versions v ON v.id = l.version_id
        JOIN templates t ON t.id = v.template_id
        WHERE t.project_id = ? AND t.id = ? AND l.id = ?
        FOR UPDATE OF v`), projID, templateID, id).Scan(&versionID, &active, &others)
	if errors.Is(err, sql.ErrNoRows) {
		return nil
	}

	if err != nil {
		return err
	}

	if active && !others {
		return ErrLastLocalization
	}

	if _, err := tx.ExecContext(ctx, s.Q(`
        DELETE FROM template_localizations WHERE id = ? AND version_id = ?
    `), id, versionID); err != nil {
		return err
	}

	return tx.Commit()
}

func scanTemplate(r interface{ Scan(...any) error }) (*tmodel.Template, error) {
	var t tmodel.Template
	var active sql.NullString
	var updated sql.NullTime
	if err := r.Scan(&t.ID, &t.ProjectID, &t.Name, &t.Description, &t.DefaultLanguage,
		&active, &t.SampleData, &t.CreatedBy, &t.LastEditedBy, &t.CreatedAt, &updated); err != nil {
		return nil, err
	}

	if active.Valid {
		t.ActiveVersionID = new(active.String)
	}

	if updated.Valid {
		t.UpdatedAt = new(updated.Time)
	}

	return &t, nil
}

func scanVersion(r interface{ Scan(...any) error }) (*tmodel.Version, error) {
	var v tmodel.Version
	var css sql.NullString
	if err := r.Scan(&v.ID, &v.TemplateID, &v.Version, &css, &v.SampleData, &v.CreatedAt); err != nil {
		return nil, err
	}

	if css.Valid {
		v.StylesheetID = new(css.String)
	}

	return &v, nil
}

func scanLocalization(r interface{ Scan(...any) error }) (*tmodel.Localization, error) {
	var l tmodel.Localization
	var updated sql.NullTime
	if err := r.Scan(&l.ID, &l.VersionID, &l.Language, &l.Subject, &l.HTML, &l.Text,
		&l.CreatedAt, &updated); err != nil {
		return nil, err
	}

	if updated.Valid {
		l.UpdatedAt = new(updated.Time)
	}

	return &l, nil
}

// nullPtr maps a nil / empty *string to SQL NULL.
func nullPtr(s *string) any {
	if s == nil || *s == "" {
		return nil
	}

	return *s
}

const attachmentSelect = `
SELECT id, project_id, template_id, filename, content_type, size, storage_key, content, created_at, deleted_at
FROM template_attachments`

// StorageKeysForProject collects every offloaded attachment key the
// project's templates own, deleted ones included.
//
// For project DELETION, where the rows go by cascade off projects and
// nothing would name their objects afterwards.
func (s *Store) StorageKeysForProject(ctx context.Context, projID string) ([]string, error) {
	rows, err := s.Query(ctx, `
        SELECT storage_key FROM template_attachments
        WHERE project_id = ? AND storage_key <> ''`, projID)
	if err != nil {
		return nil, err
	}

	defer func() { _ = rows.Close() }()
	var keys []string
	for rows.Next() {
		var k string
		if err := rows.Scan(&k); err != nil {
			return nil, err
		}

		keys = append(keys, k)
	}

	return keys, rows.Err()
}

// PutAttachment writes one attachment, inserting it or updating the
// existing row.
func (s *Store) PutAttachment(ctx context.Context, a *tmodel.Attachment) error {
	if a.CreatedAt.IsZero() {
		a.CreatedAt = time.Now().UTC()
	}

	_, err := s.Exec(ctx, `
        INSERT INTO template_attachments (id, project_id, template_id, filename,
            content_type, size, storage_key, content, created_at)
        VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?)
    `, a.ID, a.ProjectID, a.TemplateID, a.Filename,
		a.ContentType, a.Size, a.StorageKey, a.Content, a.CreatedAt)

	return err
}

// GetAttachmentAny returns one attachment within projID by id alone,
// deleted or not, or nil when the row is gone. It is how a message
// reads the bytes it references.
func (s *Store) GetAttachmentAny(ctx context.Context, projID, id string) (*tmodel.Attachment, error) {
	a, err := scanAttachment(s.QueryRow(ctx, attachmentSelect+` WHERE project_id = ? AND id = ?`, projID, id))
	if errors.Is(err, sql.ErrNoRows) {
		return nil, nil
	}

	return a, err
}

// DeletedAttachmentsBefore lists attachments marked deleted before the
// cutoff, across every project, for the retention sweep. Content is not
// read, the sweep needs only the key, and the list is bounded by what
// people uploaded.
func (s *Store) DeletedAttachmentsBefore(ctx context.Context, before time.Time) ([]*tmodel.Attachment, error) {
	rows, err := s.Query(ctx, `
        SELECT id, project_id, template_id, filename, content_type, size, storage_key, '', created_at, deleted_at
        FROM template_attachments
        WHERE deleted_at IS NOT NULL AND deleted_at < ?
        ORDER BY deleted_at ASC`, before)
	if err != nil {
		return nil, err
	}

	defer func() { _ = rows.Close() }()
	var out []*tmodel.Attachment
	for rows.Next() {
		a, err := scanAttachment(rows)
		if err != nil {
			return nil, err
		}

		out = append(out, a)
	}

	return out, rows.Err()
}

// PurgeAttachment removes a deleted attachment row for good. A row
// that is not marked deleted is left alone.
func (s *Store) PurgeAttachment(ctx context.Context, projID, id string) error {
	_, err := s.Exec(ctx, `
        DELETE FROM template_attachments
        WHERE project_id = ? AND id = ? AND deleted_at IS NOT NULL`, projID, id)

	return err
}

// ListAttachments returns the template's attachments that are not
// deleted.
func (s *Store) ListAttachments(ctx context.Context, projID, templateID string) ([]*tmodel.Attachment, error) {
	rows, err := s.Query(ctx, attachmentSelect+`
        WHERE project_id = ? AND template_id = ? AND deleted_at IS NULL ORDER BY created_at ASC`,
		projID, templateID)
	if err != nil {
		return nil, err
	}

	defer func() { _ = rows.Close() }()
	var out []*tmodel.Attachment
	for rows.Next() {
		a, err := scanAttachment(rows)
		if err != nil {
			return nil, err
		}

		out = append(out, a)
	}

	return out, rows.Err()
}

// GetAttachment returns one attachment of the template that is not
// deleted, or nil when there is no such row.
func (s *Store) GetAttachment(ctx context.Context, projID, templateID, id string) (*tmodel.Attachment, error) {
	row := s.QueryRow(ctx, attachmentSelect+`
        WHERE project_id = ? AND template_id = ? AND id = ? AND deleted_at IS NULL`,
		projID, templateID, id)
	a, err := scanAttachment(row)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, nil
	}

	return a, err
}

// DeleteAttachment marks one attachment deleted. The row and its bytes
// stay until retention finds no message referencing them.
func (s *Store) DeleteAttachment(ctx context.Context, projID, templateID, id string) error {
	_, err := s.Exec(ctx, `
        UPDATE template_attachments SET deleted_at = now()
        WHERE project_id = ? AND template_id = ? AND id = ? AND deleted_at IS NULL`,
		projID, templateID, id)

	return err
}

func scanAttachment(r interface{ Scan(...any) error }) (*tmodel.Attachment, error) {
	var a tmodel.Attachment
	if err := r.Scan(&a.ID, &a.ProjectID, database.Str(&a.TemplateID), &a.Filename,
		&a.ContentType, &a.Size, &a.StorageKey, &a.Content, &a.CreatedAt, &a.DeletedAt); err != nil {
		return nil, err
	}

	return &a, nil
}
