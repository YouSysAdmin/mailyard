// Mailyard, Copyright (c) 2021-2026 YouSysAdmin

// Package data serves project data export and erasure.
//
// Everything here composes the existing domain stores rather than
// touching tables directly, so an export can never drift from what
// the rest of the API considers a template or a subscriber, and a
// deletion goes through the same scoped queries as anything else.
//
// Scope, stated plainly because it governs every handler below:
// these endpoints operate on one project, resolved from the request
// like every other tenant route. There is no cross-project export
// and no platform-wide erase.
package data

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/gofiber/fiber/v3"

	"github.com/yousysadmin/mailyard/internal/core/env"
	"github.com/yousysadmin/mailyard/internal/core/keyset"
	"github.com/yousysadmin/mailyard/internal/core/response"
	"github.com/yousysadmin/mailyard/internal/core/validation"
	"github.com/yousysadmin/mailyard/internal/domain"
	"github.com/yousysadmin/mailyard/internal/domain/store"
	supmodel "github.com/yousysadmin/mailyard/internal/models/suppression"
	"github.com/yousysadmin/mailyard/pkg"
)

// Handler owns /api/v1/data/*.
type Handler struct {
	Runtime *env.Runtime
}

// dropBlobs deletes offloaded attachment objects before the rows that
// name them, which is the only order that works: the storage key lives
// in the row's attachments_json and nowhere else, so deleting the row
// first strands the object permanently - no later sweep can find it,
// because nothing is left to find it from.
//
// A failure here fails the whole request rather than carrying on to the
// delete. Reporting an erasure that did not erase is the one outcome
// worse than refusing it, and retrying is safe: the rows are still
// there, so the keys can still be found.
//
// A nil Blob means attachments are inline, so there is nothing to drop
// and the rows carry the bytes away themselves.
func (h *Handler) dropBlobs(ctx context.Context, keys []string) error {
	if h.Runtime.Blob == nil || len(keys) == 0 {
		return nil
	}

	for _, k := range keys {
		if err := h.Runtime.Blob.Delete(ctx, k); err != nil {
			return fmt.Errorf("erase attachment %s: %w", k, err)
		}
	}

	return nil
}

// Export is the portable snapshot of one project.
//
// Secrets are deliberately absent. SMTP passwords, API key hashes,
// and TOTP seeds carry `json:"-"` on their models and do not appear
// here, so an export is safe to hand to the data subject who asked
// for it. That also means an import cannot restore credentials -
// see the note on Import.
type Export struct {
	MailyardVersion string    `json:"mailyard_version"`
	ExportedAt      time.Time `json:"exported_at"`
	Project         any       `json:"project"`

	Templates        []any `json:"templates"`
	Stylesheets      []any `json:"stylesheets"`
	Languages        []any `json:"languages"`
	Contacts         []any `json:"contacts"`
	Subscribers      []any `json:"subscribers"`
	SubscriberLists  []any `json:"subscriber_lists"`
	Suppressions     []any `json:"suppressions"`
	UnsubscribeLists []any `json:"unsubscribe_lists"`
	Webhooks         []any `json:"webhooks"`
	SMTPServers      []any `json:"smtp_servers"`
	Domains          []any `json:"domains"`
	Senders          []any `json:"senders"`

	// Truncated names the sections that hit their ceiling, empty when
	// nothing did.
	//
	// It was MISSING, while the comment on exportSuppressionCap said the
	// export "SAYS SO instead of ending quietly" - so a project with more
	// than exportSuppressionCap blocked addresses got a short file and no
	// indication anywhere, which is the one truncation in this document
	// that would matter to somebody. Contacts and subscribers are capped
	// the same way, for the same reason - see exportRowCap. Every other
	// section is bounded by what a person made.
	Truncated []string `json:"truncated"`
}

// ExportData collects a project snapshot.
func (h *Handler) ExportData(c fiber.Ctx) error {
	rc := domain.GetRequestContext(c)
	ctx := c.Context()
	projID := rc.Project.ID
	st := h.Runtime.Store

	out := &Export{
		MailyardVersion: pkg.Version,
		ExportedAt:      time.Now().UTC(),
		Project:         rc.Project,
	}

	// Each section is collected the same way, so a store error names
	// the section that failed rather than surfacing a bare SQL error.
	sections := []struct {
		name    string
		collect func() ([]any, error)
	}{
		{"templates", func() ([]any, error) { return boxed(st.Template.List(ctx, projID)) }},
		{"stylesheets", func() ([]any, error) { return boxed(st.Stylesheet.List(ctx, projID)) }},
		{"languages", func() ([]any, error) { return boxed(st.Language.List(ctx, projID)) }},
		{"subscriber lists", func() ([]any, error) { return boxed(st.SubscriberList.List(ctx, projID)) }},
		{"suppressions", func() ([]any, error) {
			rows, cut, err := allSuppressions(ctx, st, projID)
			if cut {
				out.Truncated = append(out.Truncated, "suppressions")
			}

			return boxed(rows, err)
		}},
		{"unsubscribe lists", func() ([]any, error) { return boxed(st.UnsubscribeList.List(ctx, projID)) }},
		{"webhooks", func() ([]any, error) { return boxed(st.Webhook.List(ctx, projID)) }},
		{"smtp servers", func() ([]any, error) { return boxed(st.SMTPServer.List(ctx, projID)) }},
		{"domains", func() ([]any, error) { return boxed(st.Domain.List(ctx, projID)) }},
		{"senders", func() ([]any, error) { return boxed(st.Sender.List(ctx, projID)) }},
	}
	results := make(map[string][]any, len(sections))
	for _, sec := range sections {
		rows, err := sec.collect()
		if err != nil {
			return response.Internal(c, fmt.Errorf("export %s: %w", sec.name, err))
		}

		results[sec.name] = rows
	}

	out.Templates = results["templates"]
	out.Stylesheets = results["stylesheets"]
	out.Languages = results["languages"]
	out.SubscriberLists = results["subscriber lists"]
	out.Suppressions = results["suppressions"]
	out.UnsubscribeLists = results["unsubscribe lists"]
	out.Webhooks = results["webhooks"]
	out.SMTPServers = results["smtp servers"]
	out.Domains = results["domains"]
	out.Senders = results["senders"]

	// Contacts and subscribers are paged in the API, so they are
	// walked here rather than pulled in one unbounded query.
	contacts, cut, err := collectContacts(ctx, st, projID)
	if err != nil {
		return response.Internal(c, fmt.Errorf("export contacts: %w", err))
	}

	if cut {
		out.Truncated = append(out.Truncated, "contacts")
	}

	out.Contacts = contacts

	subs, cut, err := collectSubscribers(ctx, st, projID)
	if err != nil {
		return response.Internal(c, fmt.Errorf("export subscribers: %w", err))
	}

	if cut {
		out.Truncated = append(out.Truncated, "subscribers")
	}

	out.Subscribers = subs

	c.Set(fiber.HeaderContentDisposition,
		fmt.Sprintf(`attachment; filename="mailyard-export-%s.json"`, time.Now().UTC().Format("20060102")))

	return response.Success(c, ExportResponse{Export: out})
}

// exportPageSize bounds one page while walking a paged store.
const exportPageSize = 500

// exportRowCap bounds the contacts and the subscribers sections, the
// two that grow per recipient rather than per thing a person made.
// Suppressions had a ceiling, these were walked to the end into one
// in-memory slice and marshalled as one document - a second full copy -
// so a project of a few million contacts was a request costing
// gigabytes, repeatable at the api rate limit. Reported in Truncated
// like the suppressions are, never passed over.
const exportRowCap = 50000

func collectContacts(ctx context.Context, st *store.Store, projID string) ([]any, bool, error) {
	return walkPages(func(offset int) ([]any, error) {
		return boxed(st.Contact.List(ctx, projID, "", exportPageSize, offset))
	})
}

func collectSubscribers(ctx context.Context, st *store.Store, projID string) ([]any, bool, error) {
	return walkPages(func(offset int) ([]any, error) {
		return boxed(st.Subscriber.ListPage(ctx, projID, exportPageSize, offset))
	})
}

// walkPages pages a store to its end or to exportRowCap, and says
// which. A short page is the end. Reaching the cap on a full page
// reports truncation, which at an exact multiple of the page size can
// over-report by one page - the safe direction, as allSuppressions
// says.
func walkPages(page func(offset int) ([]any, error)) ([]any, bool, error) {
	var out []any
	for offset := 0; ; offset += exportPageSize {
		rows, err := page(offset)
		if err != nil {
			return nil, false, err
		}

		out = append(out, rows...)
		if len(rows) < exportPageSize {
			return orEmpty(out), false, nil
		}

		if len(out) >= exportRowCap {
			return out[:exportRowCap], true, nil
		}
	}
}

// boxed adapts a typed slice result to []any so the sections above
// can share one shape.
func boxed[T any](rows []T, err error) ([]any, error) {
	if err != nil {
		return nil, err
	}

	out := make([]any, 0, len(rows))
	for _, r := range rows {
		out = append(out, r)
	}

	return out, nil
}

func orEmpty(in []any) []any {
	if in == nil {
		return []any{}
	}

	return in
}

// DeleteContacts erases contact records, and the suppressions for
// that address.
//
// Note what this does not do: it leaves the email log alone. Removing
// the record that a message was sent is a separate decision with
// separate consequences (billing, deliverability disputes), so it lives behind DeleteEmailLogs.
func (h *Handler) DeleteContacts(c fiber.Ctx) error {
	rc := domain.GetRequestContext(c)
	in, resp, ok := validation.Bind[deleteContactsInput](c)
	if !ok {
		return resp
	}

	ctx := c.Context()
	projID := rc.Project.ID

	if in.Email == "" {
		if !in.ConfirmAll {
			return response.BadRequest(c,
				"pass an email to erase one contact, or confirm_all: true to erase every contact in this project")
		}

		n, err := h.Runtime.Store.Contact.PurgeAll(ctx, projID)
		if err != nil {
			return response.Internal(c, err)
		}

		return response.Success(c, ErasureResponse{
			Deleted: n,
			Message: fmt.Sprintf("Deleted %d contacts in this project.", n),
		})
	}

	email := strings.ToLower(in.Email)
	deleted, err := h.Runtime.Store.Contact.PurgeForEmail(ctx, projID, email)
	if err != nil {
		return response.Internal(c, err)
	}

	// Suppressions are keyed by address and are personal data too.
	// Every scope of them: Delete lifts the global block alone, which
	// would leave this person's list opt-outs behind as a record of
	// somebody we were asked to forget.
	if _, err := h.Runtime.Store.Suppression.PurgeForAddress(ctx, projID, email); err != nil {
		return response.Internal(c, err)
	}

	return response.Success(c, ErasureResponse{
		Deleted: deleted,
		Message: fmt.Sprintf("Erased %s from contacts and the suppression list.", email),
	})
}

// DeleteEmailLogs erases delivery records.
//
// In-flight rows (queued, scheduled, processing) are never touched -
// deleting one would strand work the queue is about to claim. The
// same rule the retention sweep follows.
func (h *Handler) DeleteEmailLogs(c fiber.Ctx) error {
	rc := domain.GetRequestContext(c)
	in, resp, ok := validation.Bind[deleteLogsInput](c)
	if !ok {
		return resp
	}

	ctx := c.Context()
	projID := rc.Project.ID

	if in.Email != "" {
		addr := strings.ToLower(in.Email)
		keys, err := h.Runtime.Store.Email.StorageKeysForAddress(ctx, projID, addr)
		if err != nil {
			return response.Internal(c, err)
		}

		if err := h.dropBlobs(ctx, keys); err != nil {
			return response.Internal(c, err)
		}

		n, err := h.Runtime.Store.Email.PurgeForAddress(ctx, projID, addr)
		if err != nil {
			return response.Internal(c, err)
		}

		return response.Success(c, ErasureResponse{
			Deleted: n,
			Message: fmt.Sprintf("Deleted %d email records involving %s.", n, in.Email),
		})
	}

	if in.OlderThanDays == 0 && !in.ConfirmAll {
		return response.BadRequest(c,
			"pass older_than_days, an email, or confirm_all: true to delete every email record in this project")
	}

	cutoff := time.Now().UTC()
	if in.OlderThanDays > 0 {
		cutoff = cutoff.AddDate(0, 0, -in.OlderThanDays)
	}

	keys, err := h.Runtime.Store.Email.StorageKeysForProjectOlderThan(ctx, projID, cutoff)
	if err != nil {
		return response.Internal(c, err)
	}

	if err := h.dropBlobs(ctx, keys); err != nil {
		return response.Internal(c, err)
	}

	n, err := h.Runtime.Store.Email.PurgeProjectOlderThan(ctx, projID, cutoff)
	if err != nil {
		return response.Internal(c, err)
	}

	msg := fmt.Sprintf("Deleted %d email records older than %d days.", n, in.OlderThanDays)
	if in.OlderThanDays == 0 {
		msg = fmt.Sprintf("Deleted %d email records.", n)
	}

	return response.Success(c, ErasureResponse{Deleted: n, Message: msg})
}

// exportSuppressionCap bounds one export.
//
// The suppression list is the only section here that grows per
// message rather than per thing a person made, and it is never
// pruned - a suppression is permanent by design. On a busy install it
// reaches millions of rows, and an export is built in memory and sent
// as one JSON document.
//
// So there is a ceiling, and hitting it is reported in the export rather
// than passed over. Truncating this section quietly would leave somebody
// with a data export missing everything older than a day and nothing
// anywhere to say so.
const exportSuppressionCap = 50000

// allSuppressions pages the whole list rather than taking one page, and
// says whether it stopped at the ceiling.
//
// A short page means the end of the list, which is how "there is no
// more" is known without a COUNT. Reaching the cap on a FULL page
// reports truncation - at an exact multiple of the page size that can
// over-report by one page, and that is the safe direction: a file
// wrongly said to be complete is the failure worth avoiding.
func allSuppressions(ctx context.Context, st *store.Store, projID string) ([]*supmodel.Suppression, bool, error) {
	// The page must fit inside the store's own backstop: listLimit in
	// the suppression store passes 1..201 through and turns anything
	// else into 51. It was 500 once - the store silently answered 51
	// rows, the short-page test below read that as the end of the
	// list, and an export carried at most 51 suppressions with
	// Truncated empty, the exact quiet loss this walker exists to
	// prevent.
	const page = 200
	out := []*supmodel.Suppression{} // we need empty slice here
	var cur keyset.Cursor
	for {
		rows, err := st.Suppression.List(ctx, projID, store.SuppressionFilter{Limit: page, Cursor: cur})
		if err != nil {
			return nil, false, err
		}

		out = append(out, rows...)
		if len(rows) < page {
			return out, false, nil
		}

		if len(out) >= exportSuppressionCap {
			return out[:exportSuppressionCap], true, nil
		}

		last := rows[len(rows)-1]
		cur = keyset.Cursor{CreatedAt: last.CreatedAt, ID: last.ID}
	}
}
