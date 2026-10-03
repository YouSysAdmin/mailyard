// Mailyard, Copyright (c) 2021-2026 YouSysAdmin

package domain

import (
	"testing"

	akmodel "github.com/yousysadmin/mailyard/internal/models/apikey"
	usermodel "github.com/yousysadmin/mailyard/internal/models/user"
)

// A write made with a platform key has no user, and the record of who
// made it must still name the key.
func TestTheActorIsNamedForEveryCredential(t *testing.T) {
	for _, tc := range []struct {
		rc        *RequestContext
		id, label string
	}{
		{nil, "", ""},
		{&RequestContext{User: &usermodel.User{ID: "u1", Email: "a@x.test"}}, "u1", "a@x.test"},
		{&RequestContext{APIKey: &akmodel.Key{Name: "ci"}}, "", "api key ci"},
		{&RequestContext{AdminAPIKey: &akmodel.Admin{Name: "ops"}}, "", "admin api key ops"},
	} {
		id, label := tc.rc.Actor()
		if id != tc.id || label != tc.label {
			t.Errorf("Actor() = %q, %q, want %q, %q", id, label, tc.id, tc.label)
		}
	}
}
