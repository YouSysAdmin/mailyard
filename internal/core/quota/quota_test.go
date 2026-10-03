// Mailyard, Copyright (c) 2021-2026 YouSysAdmin

package quota

import (
	"context"
	"errors"
	"testing"

	"github.com/yousysadmin/mailyard/internal/domain/store"
	pmodel "github.com/yousysadmin/mailyard/internal/models/plan"
	projmodel "github.com/yousysadmin/mailyard/internal/models/project"
)

type fakeProjects struct{ store.ProjectStore }

func (fakeProjects) Get(context.Context, string) (*projmodel.Project, error) {
	return &projmodel.Project{ID: "p1"}, nil
}

type fakePlans struct {
	store.PlanStore
	plan *pmodel.Plan
}

func (f fakePlans) GetDefault(context.Context) (*pmodel.Plan, error) { return f.plan, nil }

type fakeKeys struct {
	store.APIKeyStore
	n     int
	locks *fakeLocks
}

func (f *fakeKeys) Count(context.Context, string) (int, error) {
	f.locks.countedHeld = f.locks.held

	return f.n, nil
}

type fakeLocks struct {
	held        bool
	countedHeld bool
	holds       int
}

func (f *fakeLocks) Hold(context.Context, string, string) (func(), error) {
	f.holds++
	f.held = true

	return func() { f.held = false }, nil
}

// The count is taken under the lock, the lock stays held for the
// create on success, and a refusal lets it go.
func TestHoldResourceCountsUnderTheLock(t *testing.T) {
	locks := &fakeLocks{}
	keys := &fakeKeys{n: 2, locks: locks}
	st := &store.Store{
		Locks:   locks,
		Project: fakeProjects{},
		Plan:    fakePlans{plan: &pmodel.Plan{Name: "small", MaxAPIKeys: 3}},
		APIKey:  keys,
	}

	release, err := HoldResource(t.Context(), st, "p1", ResAPIKeys, 1)
	if err != nil {
		t.Fatalf("room for one more was refused: %v", err)
	}

	if !locks.countedHeld || !locks.held {
		t.Fatal("the count must be taken, and the create made, while the lock is held")
	}

	release()

	keys.n = 3
	release, err = HoldResource(t.Context(), st, "p1", ResAPIKeys, 1)
	if _, ok := errors.AsType[*Error](err); !ok {
		t.Fatalf("a full plan answered %v, want a quota error", err)
	}

	release()

	if locks.held {
		t.Error("a refusal must release the lock")
	}

	// An unbounded resource takes no lock at all.
	st.Plan = fakePlans{plan: &pmodel.Plan{Name: "free"}}
	before := locks.holds
	release, err = HoldResource(t.Context(), st, "p1", ResAPIKeys, 1)
	if err != nil || locks.holds != before {
		t.Errorf("unbounded resource: err %v, %d new holds", err, locks.holds-before)
	}

	release()
}
