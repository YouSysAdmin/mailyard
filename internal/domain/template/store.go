// Mailyard, Copyright (c) 2021-2026 YouSysAdmin

package template

import (
	"context"
	"database/sql"
	"errors"
	"time"

	"github.com/yousysadmin/mailyard/internal/database"
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
// there is no such row.
func (s *Store) GetByName(ctx context.Context, projID, name string) (*tmodel.Template, error) {
	row := s.QueryRow(ctx, templateSelect+` WHERE project_id = ? AND name = ?`, projID, name)
	t, err := scanTemplate(row)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, nil
	}

	return t, err
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
// exists.
func (s *Store) Put(ctx context.Context, t *tmodel.Template) error {
	if t.CreatedAt.IsZero() {
		t.CreatedAt = time.Now().UTC()
	}

	_, err := s.Exec(ctx, `
        INSERT INTO templates (
            id, project_id, name, description, default_language, active_version_id,
            sample_data, created_by, last_edited_by, created_at, updated_at
        ) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
        ON CONFLICT(id) DO UPDATE SET
            name              = excluded.name,
            description       = excluded.description,
            default_language  = excluded.default_language,
            active_version_id = excluded.active_version_id,
            sample_data       = excluded.sample_data,
            last_edited_by    = excluded.last_edited_by,
            updated_at        = excluded.updated_at
    `,
		t.ID, t.ProjectID, t.Name, t.Description, t.DefaultLanguage,
		nullPtr(t.ActiveVersionID), t.SampleData, t.CreatedBy, t.LastEditedBy,
		t.CreatedAt, database.NullTime(t.UpdatedAt),
	)

	return err
}

// Delete removes one template from projID.
func (s *Store) Delete(ctx context.Context, projID, id string) error {
	_, err := s.Exec(ctx, `DELETE FROM templates WHERE project_id = ? AND id = ?`, projID, id)

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
// next number for the template. The project check runs first so a
// guessed template id in another tenant is a no-op.
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
		if err := s.QueryRow(ctx, `SELECT COALESCE(MAX(version), 0) + 1 FROM template_versions WHERE template_id = ?`,
			v.TemplateID).Scan(&v.Version); err != nil {
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

// DeleteVersion removes one version from projID.
func (s *Store) DeleteVersion(ctx context.Context, projID, templateID, versionID string) error {
	_, err := s.Exec(ctx, `
        DELETE FROM template_versions
        WHERE id = ? AND template_id = ?
          AND EXISTS (SELECT 1 FROM templates t WHERE t.id = ? AND t.project_id = ?)
    `, versionID, templateID, templateID, projID)

	return err
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

// GetLocalizationByID returns one localization by id within projID, or
// nil when there is no such row.
func (s *Store) GetLocalizationByID(ctx context.Context, projID, id string) (*tmodel.Localization, error) {
	row := s.QueryRow(ctx, localizationSelect+`
        WHERE t.project_id = ? AND l.id = ?`, projID, id)
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

// DeleteLocalization removes one localization from projID.
func (s *Store) DeleteLocalization(ctx context.Context, projID, id string) error {
	_, err := s.Exec(ctx, `
        DELETE FROM template_localizations
        WHERE id = ? AND version_id IN (
            SELECT v.id FROM template_versions v
            JOIN templates t ON t.id = v.template_id
            WHERE t.project_id = ?
        )
    `, id, projID)

	return err
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
SELECT id, project_id, template_id, filename, content_type, size, storage_key, content, created_at
FROM template_attachments`

// StorageKeysForProject collects every offloaded attachment key the
// project's templates own.
//
// For project DELETION and template deletion alike, where the rows go by
// cascade (template_attachments cascades off templates, which cascades
// off projects). Neither path collected them, so every offloaded template
// attachment became an object with nothing naming it - and unlike email
// attachments, retention never looks at this table at all, so no later
// pass could ever find them.
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

// StorageKeysForTemplate is the same for one template, for the delete
// that takes its attachments with it by cascade.
func (s *Store) StorageKeysForTemplate(ctx context.Context, projID, templateID string) ([]string, error) {
	rows, err := s.Query(ctx, `
        SELECT storage_key FROM template_attachments
        WHERE project_id = ? AND template_id = ? AND storage_key <> ''`, projID, templateID)
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

// ListAttachments returns the attachments in projID.
func (s *Store) ListAttachments(ctx context.Context, projID, templateID string) ([]*tmodel.Attachment, error) {
	rows, err := s.Query(ctx, attachmentSelect+` WHERE project_id = ? AND template_id = ? ORDER BY created_at ASC`,
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

// GetAttachment returns one attachment within projID, or nil when
// there is no such row.
func (s *Store) GetAttachment(ctx context.Context, projID, templateID, id string) (*tmodel.Attachment, error) {
	row := s.QueryRow(ctx, attachmentSelect+` WHERE project_id = ? AND template_id = ? AND id = ?`,
		projID, templateID, id)
	a, err := scanAttachment(row)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, nil
	}

	return a, err
}

// DeleteAttachment removes one attachment from projID.
func (s *Store) DeleteAttachment(ctx context.Context, projID, templateID, id string) error {
	_, err := s.Exec(ctx, `DELETE FROM template_attachments WHERE project_id = ? AND template_id = ? AND id = ?`,
		projID, templateID, id)

	return err
}

func scanAttachment(r interface{ Scan(...any) error }) (*tmodel.Attachment, error) {
	var a tmodel.Attachment
	if err := r.Scan(&a.ID, &a.ProjectID, &a.TemplateID, &a.Filename,
		&a.ContentType, &a.Size, &a.StorageKey, &a.Content, &a.CreatedAt); err != nil {
		return nil, err
	}

	return &a, nil
}
