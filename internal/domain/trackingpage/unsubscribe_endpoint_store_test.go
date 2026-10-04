// Mailyard, Copyright (c) 2021-2026 YouSysAdmin

package trackingpage

import (
	"context"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/gofiber/fiber/v3"

	"github.com/yousysadmin/mailyard/internal/core/env"
	"github.com/yousysadmin/mailyard/internal/core/ids"
	"github.com/yousysadmin/mailyard/internal/core/tracking"
	"github.com/yousysadmin/mailyard/internal/database/dbtest"
	"github.com/yousysadmin/mailyard/internal/domain/campaign"
	"github.com/yousysadmin/mailyard/internal/domain/store"
	"github.com/yousysadmin/mailyard/internal/domain/subscriber"
	"github.com/yousysadmin/mailyard/internal/domain/subscriberlist"
	"github.com/yousysadmin/mailyard/internal/domain/suppression"
	"github.com/yousysadmin/mailyard/internal/domain/unsubscribelist"
	cmodel "github.com/yousysadmin/mailyard/internal/models/campaign"
	submodel "github.com/yousysadmin/mailyard/internal/models/subscriber"
	ulmodel "github.com/yousysadmin/mailyard/internal/models/unsubscribelist"
)

// unsubscribeFixture is one campaign message to one subscriber on one
// list, with the real stores behind the handler.
type unsubscribeFixture struct {
	t       *testing.T
	ctx     context.Context
	app     *fiber.App
	signer  *tracking.Signer
	st      *store.Store
	project string
	list    string
	sub     *submodel.Subscriber
	msg     *cmodel.Message
}

func newUnsubscribeFixture(t *testing.T, status string) *unsubscribeFixture {
	t.Helper()
	db := dbtest.Open(t)
	dbtest.Migrate(t, db)
	ctx := t.Context()

	subs := subscriber.NewStore(db)
	lists := subscriberlist.NewStore(db)
	camps := campaign.NewStore(db)
	st := &store.Store{
		Subscriber:      subs,
		SubscriberList:  lists,
		Campaign:        camps,
		UnsubscribeList: unsubscribelist.NewStore(db),
		Suppression:     suppression.NewStore(db),
	}

	project, list := ids.New(), ids.New()
	if _, err := db.ExecContext(ctx, `
		INSERT INTO projects (id, name, slug, created_at) VALUES ($1, 'p', $2, now())`, project, project); err != nil {
		t.Fatalf("seed project: %v", err)
	}

	if _, err := db.ExecContext(ctx, `
		INSERT INTO subscriber_lists (id, project_id, name, type, created_at)
		VALUES ($1, $2, 'Newsletter', 'static', now())`, list, project); err != nil {
		t.Fatalf("seed list: %v", err)
	}

	sub := &submodel.Subscriber{ProjectID: project, Email: "reader@example.test", Name: "Reader", Status: status}
	if err := subs.Put(ctx, sub); err != nil {
		t.Fatalf("seed subscriber: %v", err)
	}

	if err := lists.AddMember(ctx, project, list, sub.ID); err != nil {
		t.Fatalf("seed member: %v", err)
	}

	cam := &cmodel.Campaign{
		ID: ids.New(), ProjectID: project, Name: "Issue 1", FromEmail: "news@example.test",
		Status: cmodel.StatusSent, TemplateID: ids.New(), ListID: list,
	}
	if err := camps.Put(ctx, cam); err != nil {
		t.Fatalf("seed campaign: %v", err)
	}

	msg := &cmodel.Message{ID: ids.New(), CampaignID: cam.ID, SubscriberID: sub.ID, Status: cmodel.MsgSent}
	if err := camps.BulkCreateMessages(ctx, []*cmodel.Message{msg}); err != nil {
		t.Fatalf("seed message: %v", err)
	}

	// The handler logs every step. Not into the test output.
	prev := slog.Default()
	slog.SetDefault(slog.New(slog.NewTextHandler(io.Discard, nil)))
	t.Cleanup(func() { slog.SetDefault(prev) })

	signer := tracking.NewSigner("https://mail.example.test", "test-secret-test-secret-test-secret")
	h := &Handler{Runtime: &env.Runtime{Store: st, Tracking: signer}}
	app := fiber.New()
	app.Get("/tracking/unsubscribe/:token", h.UnsubscribePage)
	app.Post("/tracking/unsubscribe/:token", h.UnsubscribeConfirm)

	return &unsubscribeFixture{t: t, ctx: ctx, app: app, signer: signer, st: st,
		project: project, list: list, sub: sub, msg: msg}
}

// path is the hosted unsubscribe path for the fixture's message, as the
// mail carries it.
func (f *unsubscribeFixture) path() string {
	return strings.TrimPrefix(f.signer.UnsubscribeURL(f.msg.ID), "https://mail.example.test")
}

func (f *unsubscribeFixture) do(method, path, body string, form bool) (int, string) {
	f.t.Helper()
	var reader io.Reader
	if body != "" {
		reader = strings.NewReader(body)
	}

	req := httptest.NewRequest(method, path, reader)
	if form {
		req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	}

	res, err := f.app.Test(req)
	if err != nil {
		f.t.Fatalf("%s %s: %v", method, path, err)
	}

	defer func() { _ = res.Body.Close() }()
	out, _ := io.ReadAll(res.Body)

	return res.StatusCode, string(out)
}

func (f *unsubscribeFixture) status() string {
	f.t.Helper()
	s, err := f.st.Subscriber.Get(f.ctx, f.project, f.sub.ID)
	if err != nil || s == nil {
		f.t.Fatalf("read subscriber: %v", err)
	}

	return s.Status
}

func (f *unsubscribeFixture) optedOut() bool {
	f.t.Helper()
	ids, err := f.st.SubscriberList.UnsubscribedIDs(f.ctx, f.project, f.list)
	if err != nil {
		f.t.Fatalf("read opt-outs: %v", err)
	}

	_, ok := ids[f.sub.ID]

	return ok
}

func (f *unsubscribeFixture) stamped() *time.Time {
	f.t.Helper()
	m, err := f.st.Campaign.GetMessageAny(f.ctx, f.msg.ID)
	if err != nil || m == nil {
		f.t.Fatalf("read message: %v", err)
	}

	return m.UnsubscribedAt
}

func (f *unsubscribeFixture) events() int {
	f.t.Helper()
	var n int
	if err := f.st.Campaign.(*campaign.Store).DB().QueryRowContext(f.ctx,
		`SELECT COUNT(*) FROM tracking_events WHERE campaign_message_id = $1 AND event_type = 'unsubscribe'`,
		f.msg.ID).Scan(&n); err != nil {
		f.t.Fatalf("count events: %v", err)
	}

	return n
}

// The one-click POST a mail client sends - a body of
// List-Unsubscribe=One-Click and nothing else - is a global opt-out: the
// subscriber leaves every campaign of the project, the list records the
// opt-out, the message is stamped, one event is written, and a second
// POST changes nothing.
func TestOneClickUnsubscribesFromEveryCampaign(t *testing.T) {
	f := newUnsubscribeFixture(t, submodel.StatusSubscribed)

	code, body := f.do(http.MethodPost, f.path(), "List-Unsubscribe=One-Click", true)
	if code != http.StatusOK || !strings.Contains(body, "Unsubscribed") {
		t.Fatalf("one-click: %d %q", code, body)
	}

	if got := f.status(); got != submodel.StatusUnsubscribed {
		t.Errorf("status = %q, want unsubscribed", got)
	}

	if !f.optedOut() {
		t.Error("the per-list opt-out was not recorded")
	}

	first := f.stamped()
	if first == nil {
		t.Fatal("the message was not stamped")
	}

	if n := f.events(); n != 1 {
		t.Errorf("events = %d, want 1", n)
	}

	if _, _, unsubscribed, err := f.st.Campaign.EngagementStats(f.ctx, f.msg.CampaignID); err != nil || unsubscribed != 1 {
		t.Errorf("engagement unsubscribed = %d, err %v, want 1", unsubscribed, err)
	}

	// A retry from the mail client.
	if code, _ := f.do(http.MethodPost, f.path(), "List-Unsubscribe=One-Click", true); code != http.StatusOK {
		t.Fatalf("second one-click: %d", code)
	}

	if again := f.stamped(); again == nil || !again.Equal(*first) {
		t.Errorf("second POST moved the stamp: %v -> %v", first, again)
	}

	if n := f.events(); n != 1 {
		t.Errorf("events after the retry = %d, want still 1", n)
	}
}

// A POST with no body at all is a one-click too: the scope is decided by
// the ABSENCE of the page's own field, never by the body being there.
func TestAnEmptyPostIsAOneClick(t *testing.T) {
	f := newUnsubscribeFixture(t, submodel.StatusSubscribed)
	if code, _ := f.do(http.MethodPost, f.path(), "", false); code != http.StatusOK {
		t.Fatalf("empty POST: %d", code)
	}

	if got := f.status(); got != submodel.StatusUnsubscribed {
		t.Errorf("status = %q, want unsubscribed", got)
	}
}

// The hosted page offers the choice, and its list-only button records
// the per-list opt-out without touching the subscriber's status.
func TestThePageOffersTheListOnlyChoice(t *testing.T) {
	f := newUnsubscribeFixture(t, submodel.StatusSubscribed)

	code, body := f.do(http.MethodGet, f.path(), "", false)
	if code != http.StatusOK {
		t.Fatalf("GET: %d", code)
	}

	for _, want := range []string{`name="scope" value="list"`, `name="scope" value="all"`, "Newsletter"} {
		if !strings.Contains(body, want) {
			t.Errorf("page lacks %q:\n%s", want, body)
		}
	}

	if strings.Contains(body, f.sub.Email) {
		t.Error("the page names the mailbox, which a leaked link must not")
	}

	if f.optedOut() || f.stamped() != nil {
		t.Fatal("a GET wrote something")
	}

	code, body = f.do(http.MethodPost, f.path(), "scope=list", true)
	if code != http.StatusOK || !strings.Contains(body, "Newsletter") {
		t.Fatalf("list-only POST: %d %q", code, body)
	}

	if got := f.status(); got != submodel.StatusSubscribed {
		t.Errorf("status = %q, want still subscribed", got)
	}

	if !f.optedOut() {
		t.Error("the per-list opt-out was not recorded")
	}

	if f.stamped() == nil {
		t.Error("the message was not stamped")
	}
}

// A bounced address is a stronger record than an unsubscribe, and a
// one-click does not downgrade it. The list opt-out is still recorded.
func TestOneClickLeavesABouncedStatusAlone(t *testing.T) {
	f := newUnsubscribeFixture(t, submodel.StatusBounced)
	if code, _ := f.do(http.MethodPost, f.path(), "List-Unsubscribe=One-Click", true); code != http.StatusOK {
		t.Fatalf("one-click: %d", code)
	}

	if got := f.status(); got != submodel.StatusBounced {
		t.Errorf("status = %q, want bounced", got)
	}

	if !f.optedOut() {
		t.Error("the per-list opt-out was not recorded")
	}
}

// A token that does not verify is a 404 on both methods, and writes
// nothing.
func TestATamperedTokenIsRefused(t *testing.T) {
	f := newUnsubscribeFixture(t, submodel.StatusSubscribed)
	bad := f.path() + "x"
	if code, _ := f.do(http.MethodGet, bad, "", false); code != http.StatusNotFound {
		t.Errorf("GET tampered: %d, want 404", code)
	}

	if code, _ := f.do(http.MethodPost, bad, "List-Unsubscribe=One-Click", true); code != http.StatusNotFound {
		t.Errorf("POST tampered: %d, want 404", code)
	}

	if f.status() != submodel.StatusSubscribed || f.optedOut() {
		t.Error("a refused token wrote something")
	}
}

// The transactional kind is untouched by all of the above: a scoped
// token writes a suppression on its list and leaves the subscriber's
// status alone - suppressions are what block transactional mail.
func TestATransactionalOptOutStillWritesAScopedSuppression(t *testing.T) {
	f := newUnsubscribeFixture(t, submodel.StatusSubscribed)
	ul := &ulmodel.List{ID: ids.New(), ProjectID: f.project, Name: "Receipts", Active: true}
	if err := f.st.UnsubscribeList.Put(f.ctx, ul); err != nil {
		t.Fatalf("seed unsubscribe list: %v", err)
	}

	path := strings.TrimPrefix(f.signer.ListUnsubscribeURL(ul.ID, f.sub.Email), "https://mail.example.test")
	if code, _ := f.do(http.MethodPost, path, "List-Unsubscribe=One-Click", true); code != http.StatusOK {
		t.Fatalf("scoped one-click: %d", code)
	}

	var n int
	if err := f.st.Campaign.(*campaign.Store).DB().QueryRowContext(f.ctx,
		`SELECT COUNT(*) FROM suppressions WHERE project_id = $1 AND email = $2 AND unsubscribe_list_id = $3`,
		f.project, f.sub.Email, ul.ID).Scan(&n); err != nil || n != 1 {
		t.Errorf("scoped suppressions = %d, err %v, want 1", n, err)
	}

	if got := f.status(); got != submodel.StatusSubscribed {
		t.Errorf("status = %q, want subscribed - a transactional opt-out is not a campaign one", got)
	}
}
