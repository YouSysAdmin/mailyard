// Mailyard, Copyright (c) 2021-2026 YouSysAdmin

package subscriberlist

import (
	"context"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/gofiber/fiber/v3"

	"github.com/yousysadmin/mailyard/internal/core/env"
	"github.com/yousysadmin/mailyard/internal/domain"
	"github.com/yousysadmin/mailyard/internal/domain/store"
	pmodel "github.com/yousysadmin/mailyard/internal/models/plan"
	projmodel "github.com/yousysadmin/mailyard/internal/models/project"
	submodel "github.com/yousysadmin/mailyard/internal/models/subscriber"
	slmodel "github.com/yousysadmin/mailyard/internal/models/subscriberlist"
)

const quotaListID = "0192f000-0000-7000-8000-000000000001"

type quotaProjects struct{ store.ProjectStore }

func (quotaProjects) Get(_ context.Context, id string) (*projmodel.Project, error) {
	return &projmodel.Project{ID: id}, nil
}

type quotaPlans struct{ store.PlanStore }

func (quotaPlans) GetDefault(context.Context) (*pmodel.Plan, error) {
	return &pmodel.Plan{Name: "tiny", MaxSubscribers: 1}, nil
}

type quotaLists struct{ store.SubscriberListStore }

func (quotaLists) Get(_ context.Context, projID, id string) (*slmodel.List, error) {
	return &slmodel.List{ID: id, ProjectID: projID, Type: slmodel.TypeStatic}, nil
}

func (quotaLists) AddMember(context.Context, string, string, string) error { return nil }

func (quotaLists) Resubscribe(context.Context, string, string, string) error { return nil }

// quotaSubscribers holds one subscriber, which fills a plan of one.
type quotaSubscribers struct {
	store.SubscriberStore
	held map[string]*submodel.Subscriber
	puts int
}

func (s *quotaSubscribers) GetByEmail(_ context.Context, _, email string) (*submodel.Subscriber, error) {
	return s.held[email], nil
}

func (s *quotaSubscribers) Count(context.Context, string) (int, error) { return len(s.held), nil }

func (s *quotaSubscribers) Put(context.Context, *submodel.Subscriber) error {
	s.puts++

	return nil
}

// A subscribe call adding a new address is a subscriber create and
// is refused at the plan cap, while an address already held is not.
func TestSubscribeRespectsTheSubscriberCap(t *testing.T) {
	subs := &quotaSubscribers{held: map[string]*submodel.Subscriber{
		"old@example.invalid": {ID: "s1", ProjectID: "p1", Email: "old@example.invalid"},
	}}
	h := &Handler{Runtime: &env.Runtime{Store: &store.Store{
		Project:        quotaProjects{},
		Plan:           quotaPlans{},
		Subscriber:     subs,
		SubscriberList: quotaLists{},
	}}}

	app := fiber.New()
	app.Post("/", func(c fiber.Ctx) error {
		c.Locals(domain.ContextKey, &domain.RequestContext{Project: &projmodel.Project{ID: "p1"}})

		return h.Subscribe(c)
	})

	subscribe := func(email string) int {
		req := httptest.NewRequest("POST", "/",
			strings.NewReader(`{"list_id":"`+quotaListID+`","email":"`+email+`"}`))
		req.Header.Set("Content-Type", "application/json")

		resp, err := app.Test(req)
		if err != nil {
			t.Fatal(err)
		}

		_ = resp.Body.Close()

		return resp.StatusCode
	}

	if code := subscribe("new@example.invalid"); code != fiber.StatusTooManyRequests {
		t.Fatalf("a new address at the cap answered %d, want 429", code)
	}

	if subs.puts != 0 {
		t.Fatalf("a refused subscribe wrote %d subscribers", subs.puts)
	}

	if code := subscribe("old@example.invalid"); code != fiber.StatusCreated {
		t.Fatalf("an address already held answered %d, want 201", code)
	}
}
