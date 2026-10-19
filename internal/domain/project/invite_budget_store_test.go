// Mailyard, Copyright (c) 2021-2026 YouSysAdmin

package project

import (
	"context"
	"fmt"
	"log/slog"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/gofiber/fiber/v3"

	"github.com/yousysadmin/mailyard/internal/core/env"
	"github.com/yousysadmin/mailyard/internal/core/ids"
	"github.com/yousysadmin/mailyard/internal/core/safego"
	"github.com/yousysadmin/mailyard/internal/core/systemmail"
	"github.com/yousysadmin/mailyard/internal/database"
	"github.com/yousysadmin/mailyard/internal/database/dbtest"
	"github.com/yousysadmin/mailyard/internal/domain"
	"github.com/yousysadmin/mailyard/internal/domain/store"
	projmodel "github.com/yousysadmin/mailyard/internal/models/project"
	usermodel "github.com/yousysadmin/mailyard/internal/models/user"
)

// recordingProject counts the platform mails handed to the queue.
type recordingProject struct {
	mu   sync.Mutex
	sent []systemmail.Message
}

func (r *recordingProject) SendSystem(_ context.Context, _ string, m systemmail.Message) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.sent = append(r.sent, m)

	return nil
}

func (r *recordingProject) CheckSystem(context.Context, string, string) error { return nil }

func (r *recordingProject) count() int {
	r.mu.Lock()
	defer r.mu.Unlock()

	return len(r.sent)
}

type inviteRig struct {
	st     *Store
	proj   string
	app    *fiber.App
	mail   *recordingProject
	queued *safego.Group
}

func newInviteRig(t *testing.T) *inviteRig {
	t.Helper()
	st, proj, _ := roleFixture(t)
	owner := addRoleMember(t, st, proj, true)

	log := slog.New(slog.DiscardHandler)
	rec := &recordingProject{}
	sender := systemmail.New(func() systemmail.Address {
		return systemmail.Address{From: "no-reply@platform.example", Project: ids.New()}
	}, log)
	sender.UseProject(rec)
	sender.Background = &safego.Group{}

	cfg := &env.Config{}
	cfg.Server.PublicURL = "https://mail.example"
	h := &Handler{Runtime: &env.Runtime{
		Store:      &store.Store{Project: st},
		Config:     cfg,
		SystemMail: sender,
	}}

	app := fiber.New()
	app.Use(func(c fiber.Ctx) error {
		c.Locals(domain.ContextKey, &domain.RequestContext{User: &usermodel.User{ID: owner}})

		return c.Next()
	})
	app.Post("/projects/:id/invitations", h.CreateInvitation)

	return &inviteRig{st: st, proj: proj, app: app, mail: rec, queued: sender.Background}
}

func (r *inviteRig) invite(t *testing.T, email string) int {
	t.Helper()
	body := `{"email":"` + email + `"}`
	req := httptest.NewRequest(fiber.MethodPost, "/projects/"+r.proj+"/invitations", strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	res, err := r.app.Test(req)
	if err != nil {
		t.Fatalf("invite: %v", err)
	}

	_ = res.Body.Close()

	return res.StatusCode
}

func (r *inviteRig) mails(t *testing.T) int {
	t.Helper()
	if !r.queued.Wait(10 * time.Second) {
		t.Fatal("platform mail did not settle")
	}

	return r.mail.count()
}

// seed writes n invitations directly, created an age ago and expiring
// at expires.
func (r *inviteRig) seed(t *testing.T, n int, age time.Duration, expires time.Time) {
	t.Helper()
	for i := range n {
		inv := &projmodel.Invitation{
			ID:        ids.New(),
			ProjectID: r.proj,
			Email:     fmt.Sprintf("seeded-%d-%s@example.invalid", i, ids.New()),
			Token:     ids.New(),
			Status:    projmodel.InvitationPending,
			ExpiresAt: expires,
			CreatedAt: time.Now().UTC().Add(-age),
		}
		if err := r.st.PutInvitation(t.Context(), inv); err != nil {
			t.Fatalf("seed invitation: %v", err)
		}
	}
}

// seedSends writes n entries to the send log, an age ago, to fresh
// addresses unless email is given.
func (r *inviteRig) seedSends(t *testing.T, n int, age time.Duration, email string) {
	t.Helper()
	for i := range n {
		addr := email
		if addr == "" {
			addr = fmt.Sprintf("sent-%d-%s@example.invalid", i, ids.New())
		}

		if _, err := r.st.DB().ExecContext(t.Context(), `
			INSERT INTO project_invitation_sends (id, project_id, email, created_at)
			VALUES ($1, $2, $3, $4)`, ids.New(), r.proj, addr, time.Now().UTC().Add(-age)); err != nil {
			t.Fatalf("seed send: %v", err)
		}
	}
}

// cancelAll deletes every invitation of the project, as the console's
// cancel does.
func (r *inviteRig) cancelAll(t *testing.T) {
	t.Helper()
	list, err := r.st.ListInvitations(t.Context(), r.proj)
	if err != nil {
		t.Fatalf("list invitations: %v", err)
	}

	for _, inv := range list {
		if err := r.st.DeleteInvitation(t.Context(), r.proj, inv.ID); err != nil {
			t.Fatalf("cancel invitation: %v", err)
		}
	}
}

func (r *inviteRig) sends(t *testing.T) int {
	t.Helper()
	var n int
	if err := r.st.DB().QueryRowContext(t.Context(),
		`SELECT COUNT(*) FROM project_invitation_sends WHERE project_id = $1`, r.proj).Scan(&n); err != nil {
		t.Fatalf("count sends: %v", err)
	}

	return n
}

// Cancelling an invitation does not give its address a fresh mail.
// The address has nothing pending, so the answer is the rate limit,
// not the conflict.
func TestACancelledInvitationStillCountsAgainstItsAddress(t *testing.T) {
	r := newInviteRig(t)

	if got := r.invite(t, "victim@example.invalid"); got != fiber.StatusCreated {
		t.Fatalf("first invitation: %d, want 201", got)
	}

	r.cancelAll(t)

	if got := r.invite(t, "Victim@Example.invalid"); got != fiber.StatusTooManyRequests {
		t.Errorf("invitation after a cancel within the hour: %d, want 429", got)
	}

	if got := r.mails(t); got != 1 {
		t.Errorf("platform mails queued: %d, want 1", got)
	}
}

func TestAnAddressMayBeInvitedAgainAfterTheHour(t *testing.T) {
	r := newInviteRig(t)
	r.seedSends(t, 1, 2*time.Hour, "again@example.invalid")

	if got := r.invite(t, "again@example.invalid"); got != fiber.StatusCreated {
		t.Errorf("invitation an hour after the last one: %d, want 201", got)
	}
}

func TestCancellingDoesNotRefundTheHourlyBudget(t *testing.T) {
	r := newInviteRig(t)

	for i := range maxInvitationsPerHour {
		if got := r.invite(t, fmt.Sprintf("churn-%d@example.invalid", i)); got != fiber.StatusCreated {
			t.Fatalf("invitation %d: %d, want 201", i+1, got)
		}

		r.cancelAll(t)
	}

	if got := r.invite(t, "one-too-many@example.invalid"); got != fiber.StatusTooManyRequests {
		t.Errorf("invitation %d in an hour after cancels: %d, want 429", maxInvitationsPerHour+1, got)
	}

	if got := r.mails(t); got != maxInvitationsPerHour {
		t.Errorf("platform mails queued: %d, want %d", got, maxInvitationsPerHour)
	}
}

func TestPruneInvitationSendsDropsOnlyOldRows(t *testing.T) {
	r := newInviteRig(t)
	r.seedSends(t, 3, 4*24*time.Hour, "")
	r.seedSends(t, 2, time.Hour, "")

	n, err := r.st.PruneInvitationSendsBefore(t.Context(), time.Now().UTC().AddDate(0, 0, -3))
	if err != nil {
		t.Fatalf("prune: %v", err)
	}

	if n != 3 {
		t.Errorf("pruned %d rows, want 3", n)
	}

	if got := r.sends(t); got != 2 {
		t.Errorf("send log rows left: %d, want 2", got)
	}
}

func TestASecondInvitationToAPendingAddressIsRefused(t *testing.T) {
	r := newInviteRig(t)

	if got := r.invite(t, "victim@example.invalid"); got != fiber.StatusCreated {
		t.Fatalf("first invitation: %d, want 201", got)
	}

	if got := r.invite(t, "Victim@Example.invalid"); got != fiber.StatusConflict {
		t.Errorf("second invitation to a pending address: %d, want 409", got)
	}

	if got := r.mails(t); got != 1 {
		t.Errorf("platform mails queued: %d, want 1", got)
	}
}

// An expired invitation is no longer pending, so it neither blocks the
// address nor counts against the project.
func TestAnExpiredInvitationDoesNotBlockTheAddress(t *testing.T) {
	r := newInviteRig(t)

	expired := &projmodel.Invitation{
		ID: ids.New(), ProjectID: r.proj, Email: "late@example.invalid", Token: ids.New(),
		Status:    projmodel.InvitationPending,
		ExpiresAt: time.Now().UTC().Add(-time.Hour),
		CreatedAt: time.Now().UTC().Add(-8 * 24 * time.Hour),
	}
	if err := r.st.PutInvitation(t.Context(), expired); err != nil {
		t.Fatalf("seed: %v", err)
	}

	if got := r.invite(t, "late@example.invalid"); got != fiber.StatusCreated {
		t.Errorf("invitation after the previous one expired: %d, want 201", got)
	}
}

func TestAProjectInvitesAtMostFiftyAnHour(t *testing.T) {
	r := newInviteRig(t)

	for i := range maxInvitationsPerHour {
		if got := r.invite(t, fmt.Sprintf("hourly-%d@example.invalid", i)); got != fiber.StatusCreated {
			t.Fatalf("invitation %d: %d, want 201", i+1, got)
		}
	}

	if got := r.invite(t, "one-too-many@example.invalid"); got != fiber.StatusTooManyRequests {
		t.Errorf("invitation %d in an hour: %d, want 429", maxInvitationsPerHour+1, got)
	}

	if got := r.mails(t); got != maxInvitationsPerHour {
		t.Errorf("platform mails queued: %d, want %d", got, maxInvitationsPerHour)
	}
}

func TestAProjectHoldsAtMostTwoHundredPendingInvitations(t *testing.T) {
	r := newInviteRig(t)

	// Old enough to be outside the hourly window, still redeemable.
	r.seed(t, maxPendingInvitations, 2*time.Hour, time.Now().UTC().Add(24*time.Hour))

	// Expired ones are not pending and must not count.
	r.seed(t, 5, 2*time.Hour, time.Now().UTC().Add(-time.Hour))

	if got := r.invite(t, "pending-cap@example.invalid"); got != fiber.StatusTooManyRequests {
		t.Errorf("invitation past %d pending: %d, want 429", maxPendingInvitations, got)
	}

	if got := r.mails(t); got != 0 {
		t.Errorf("platform mails queued: %d, want 0", got)
	}
}

// The budget is read under a lock held to the insert, so requests
// arriving together, here each on a connection of its own as several
// nodes would be, cannot all pass one count.
func TestConcurrentInvitationsDoNotExceedTheHourlyBudget(t *testing.T) {
	r := newInviteRig(t)

	const head = 5
	r.seedSends(t, maxInvitationsPerHour-head, time.Minute, "")

	const racers = 12
	stores := make([]*Store, racers)
	for i := range stores {
		stores[i] = &Store{Base: database.NewBase(dbtest.Peer(t))}
	}

	var wg sync.WaitGroup
	start := make(chan struct{})
	results := make(chan bool, racers)
	for i, s := range stores {
		wg.Go(func() {
			<-start
			now := time.Now().UTC()
			inv := &projmodel.Invitation{
				ID: ids.New(), ProjectID: r.proj, Email: fmt.Sprintf("racer-%d@example.invalid", i),
				Token: ids.New(), Status: projmodel.InvitationPending,
				ExpiresAt: now.Add(invitationTTL), CreatedAt: now,
			}
			_, created, err := s.PutInvitationWithin(t.Context(), inv, now.Add(-time.Hour), refuseInvitation)
			if err != nil {
				t.Errorf("racer %d: %v", i, err)
			}

			results <- created
		})
	}

	close(start)
	wg.Wait()
	close(results)

	created := 0
	for ok := range results {
		if ok {
			created++
		}
	}

	if created != head {
		t.Errorf("concurrent invitations created: %d, want %d", created, head)
	}
}
