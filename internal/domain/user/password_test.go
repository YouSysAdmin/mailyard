// Mailyard, Copyright (c) 2021-2026 YouSysAdmin

package user

import (
	"strings"
	"testing"

	"github.com/yousysadmin/mailyard/internal/core/validation"
)

// A password of nothing but spaces was trimmed to empty, read as "no
// password", and created an account nobody could sign in to locally.
func TestAWhitespacePasswordIsRefusedNotDropped(t *testing.T) {
	blank := strings.Repeat(" ", 16)

	if err := validation.NormalizeAndValidate(&createInput{Email: "a@x.test", Password: blank}); err == nil {
		t.Error("create accepted a whitespace-only password")
	}

	if err := validation.NormalizeAndValidate(&updateInput{Password: blank}); err == nil {
		t.Error("update accepted a whitespace-only password")
	}

	in := &createInput{Email: "a@x.test"}
	if err := validation.NormalizeAndValidate(in); err != nil {
		t.Errorf("an absent password is an account without one: %v", err)
	}

	in = &createInput{Email: "a@x.test", Password: "  correct-horse-battery  "}
	if err := validation.NormalizeAndValidate(in); err != nil || in.Password != "correct-horse-battery" {
		t.Errorf("a real password is trimmed and kept: %q %v", in.Password, err)
	}
}
