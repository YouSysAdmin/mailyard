// Mailyard, Copyright (c) 2021-2026 YouSysAdmin

package project

import (
	"context"
	"testing"

	"github.com/yousysadmin/mailyard/internal/core/env"
	"github.com/yousysadmin/mailyard/internal/core/settings"
	"github.com/yousysadmin/mailyard/internal/domain"
	settingmodel "github.com/yousysadmin/mailyard/internal/models/setting"
	usermodel "github.com/yousysadmin/mailyard/internal/models/user"
)

// storedSettings is a settings.Loader over rows held in memory, so the
// decision can be tested against a setting that is actually ON without
// a database.
type storedSettings []*settingmodel.Setting

func (s storedSettings) All(context.Context) ([]*settingmodel.Setting, error) {
	return s, nil
}

// runtimeWith builds the two things MayCreate reads and nothing else.
func runtimeWith(t *testing.T, userCreation bool, authDisabled bool) *env.Runtime {
	t.Helper()

	var rows storedSettings
	if userCreation {
		rows = storedSettings{{
			Key:   settingmodel.KeyUserProjectCreation,
			Value: "true",
			Type:  settingmodel.TypeBool,
		}}
	}

	svc := settings.New(rows)
	if err := svc.Reload(t.Context()); err != nil {
		t.Fatalf("reload settings: %v", err)
	}

	cfg := &env.Config{}
	cfg.Auth.Disabled = authDisabled

	return &env.Runtime{Settings: svc, Config: cfg}
}

func member(admin bool) *domain.RequestContext {
	return &domain.RequestContext{User: &usermodel.User{ID: "u1", Admin: admin}}
}

// CREATING A PROJECT IS CLOSED UNTIL AN ADMIN OPENS IT.
//
// The default is the whole point of the setting: an installation that
// nobody has configured is one where projects are made by an
// administrator and joined by invitation. Getting this backwards is not
// a visible bug - every account simply gains the ability to mint
// tenants, and the tenants it mints have mail and API keys in them.
func TestOnlyAnAdminCreatesProjectsByDefault(t *testing.T) {
	rt := runtimeWith(t, false, false)

	if MayCreate(rt, member(false)) {
		t.Error("an ordinary account may create a project with the setting off")
	}

	if !MayCreate(rt, member(true)) {
		t.Error("a platform admin was refused - somebody has to be able to make the first project")
	}
}

func TestTheSettingOpensItToEverybody(t *testing.T) {
	rt := runtimeWith(t, true, false)

	if !MayCreate(rt, member(false)) {
		t.Error("an ordinary account was refused with the setting on")
	}

	if !MayCreate(rt, member(true)) {
		t.Error("a platform admin was refused with the setting on")
	}
}

// The three ways of being nobody. A caller with no user reaches this
// through machineAuth holding an API key, and Create refuses that
// anyway - but the answer has to be no here too, or the list endpoint
// tells a key-authenticated caller it may create one.
func TestNobodyIsNotSomebody(t *testing.T) {
	rt := runtimeWith(t, true, false)

	cases := map[string]*domain.RequestContext{
		"no request context": nil,
		"no user on it":      {},
	}
	for name, rc := range cases {
		if MayCreate(rt, rc) {
			t.Errorf("%s: may create a project", name)
		}
	}

	if MayCreate(nil, member(true)) {
		t.Error("a nil runtime answered yes")
	}
}

// Auth disabled is an installation with no accounts to tell apart, and
// requireAdmin makes the same exemption. Refusing here would leave such
// an install with no way to create its first project at all.
func TestAuthDisabledCreatesFreely(t *testing.T) {
	rt := runtimeWith(t, false, true)

	if !MayCreate(rt, nil) {
		t.Error("auth disabled must not gate project creation")
	}
}
