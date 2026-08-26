// Mailyard, Copyright (c) 2021-2026 YouSysAdmin

package plan

import (
	"reflect"
	"testing"
	"time"

	"github.com/yousysadmin/mailyard/internal/core/ids"
	"github.com/yousysadmin/mailyard/internal/database/dbtest"
	pmodel "github.com/yousysadmin/mailyard/internal/models/plan"
)

// A limit a caller sends has to reach the row, and come back.
//
// This exists because the sandbox limits went in and DID NOTHING. The
// migration had the columns, the model had the fields, the store's INSERT
// named them, the console had inputs for them - and `apply`, the four
// lines that copy the request onto the model, did not. So a plan saved
// with a sandbox cap of 3 stored 0, and the sandbox it was supposed to
// bound kept everything. Nothing failed anywhere: the request answered
// 200 with the numbers the caller sent, because the response is rendered
// from the input.
//
// It is the same shape as TestEmailSurvivesARoundTrip and for the same
// reason - a field has to be written in three lists to work, and missing
// one is silent. Reflection rather than a hand-kept list of fields, so
// the NEXT limit is covered by this test the day it is added.
func TestEveryLimitSurvivesTheRoundTrip(t *testing.T) {
	db := dbtest.Open(t)
	dbtest.Migrate(t, db)
	store := NewStore(db)
	ctx := t.Context()

	// Every numeric field of the request, set to a DISTINCT value: a
	// mapping that copies the wrong field passes an all-ones test.
	//
	// The limits are *int, not int, because 0 MEANS unlimited and a PATCH
	// has to be able to leave one alone - so this allocates each pointer
	// and sets the pointee. It found zero fields the moment they became
	// pointers and refused to pass, which is the anti-vacuity guard below
	// doing its job.
	in := upsertInput{Name: "round trip"}
	v := reflect.ValueOf(&in).Elem()
	next := 11
	numeric := 0
	for _, field := range v.Fields() {
		if field.Kind() != reflect.Pointer || field.Type().Elem().Kind() != reflect.Int {
			continue
		}

		field.Set(reflect.New(field.Type().Elem()))
		field.Elem().SetInt(int64(next))
		next += 7
		numeric++
	}

	if numeric < 6 {
		t.Fatalf("only found %d numeric limits on the request - this test would prove little", numeric)
	}

	p := &pmodel.Plan{ID: ids.New(), CreatedAt: time.Now().UTC()}
	apply(p, in)
	if err := store.Put(ctx, p); err != nil {
		t.Fatalf("put: %v", err)
	}

	got, err := store.Get(ctx, p.ID)
	if err != nil {
		t.Fatalf("get: %v", err)
	}

	if got == nil {
		t.Fatal("the plan did not come back")
	}

	// Compared by name: the request and the model use the same one for
	// every limit, which is what lets this find a field nobody mapped.
	inType := v.Type()
	out := reflect.ValueOf(got).Elem()
	for i := range v.NumField() {
		f := v.Field(i)
		if f.Kind() != reflect.Pointer || f.Type().Elem().Kind() != reflect.Int || f.IsNil() {
			continue
		}

		name := inType.Field(i).Name
		field := out.FieldByName(name)
		if !field.IsValid() {
			t.Errorf("the request has %s and the model has no such field - one of them is "+
				"named wrongly, and a limit nobody can store is a limit nobody enforces", name)
			continue
		}

		if field.Int() != f.Elem().Int() {
			t.Errorf("%s: sent %d, stored and read back %d.\n\n"+
				"Three places have to know about a limit: apply() copies the request onto "+
				"the model, the INSERT names the column, and the scan reads it. A limit that "+
				"answers 200 and stores nothing is what this test is here to catch",
				name, f.Elem().Int(), field.Int())
		}
	}
}

// A PATCH that names only some fields must leave the rest alone.
//
// The limits were plain ints, so an absent field arrived as 0 - and 0 is
// not "unset" here, it MEANS unlimited. So `PATCH {"name":"Starter"}`,
// which validation accepts because only name is required, removed every
// limit on the plan: quota.CheckSend and CheckResource both return nil at
// 0. is_default went false with it, and if that had been the default plan
// then every project with no explicit assignment became unlimited too.
// Nothing in the response said so.
func TestAPartialUpdateKeepsTheLimitsItDoesNotName(t *testing.T) {
	stored := &pmodel.Plan{
		ID:                      ids.New(),
		Name:                    "Starter",
		IsDefault:               true,
		HourlyEmailLimit:        100,
		DailyEmailLimit:         1000,
		MaxAPIKeys:              5,
		MaxSMTPServers:          2,
		MaxDomains:              3,
		MaxSubscribers:          10000,
		MaxSandboxMessages:      50,
		MaxSandboxRetentionDays: 7,
	}

	// The body an operator sends to fix a typo in the name.
	apply(stored, upsertInput{Name: "Starter plan"})

	if stored.Name != "Starter plan" {
		t.Errorf("name = %q, want the edit applied", stored.Name)
	}

	for _, tc := range []struct {
		what string
		got  int
		want int
	}{
		{"hourly_email_limit", stored.HourlyEmailLimit, 100},
		{"daily_email_limit", stored.DailyEmailLimit, 1000},
		{"max_api_keys", stored.MaxAPIKeys, 5},
		{"max_smtp_servers", stored.MaxSMTPServers, 2},
		{"max_domains", stored.MaxDomains, 3},
		{"max_subscribers", stored.MaxSubscribers, 10000},
		{"max_sandbox_messages", stored.MaxSandboxMessages, 50},
		{"max_sandbox_retention_days", stored.MaxSandboxRetentionDays, 7},
	} {
		if tc.got != tc.want {
			t.Errorf("%s = %d after a name-only PATCH, want %d - 0 here means UNLIMITED",
				tc.what, tc.got, tc.want)
		}
	}

	if !stored.IsDefault {
		t.Error("is_default was cleared by a name-only PATCH, which makes every project " +
			"with no explicit plan unlimited")
	}

	// And a field that IS named still changes, or the fix would have
	// replaced a wipe with a no-op.
	zero, hundred := 0, 100
	apply(stored, upsertInput{Name: "Starter plan", HourlyEmailLimit: &zero, MaxAPIKeys: &hundred})
	if stored.HourlyEmailLimit != 0 || stored.MaxAPIKeys != 100 {
		t.Errorf("named fields did not apply: hourly=%d api_keys=%d",
			stored.HourlyEmailLimit, stored.MaxAPIKeys)
	}

	if stored.DailyEmailLimit != 1000 {
		t.Error("an unnamed field changed while another was being set")
	}
}
