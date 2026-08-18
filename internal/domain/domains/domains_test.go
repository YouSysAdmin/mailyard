// Mailyard, Copyright (c) 2021-2026 YouSysAdmin

package domains

import (
	"context"
	"errors"
	"testing"

	dmodel "github.com/yousysadmin/mailyard/internal/models/domain"
)

func TestCheckOwnership(t *testing.T) {
	d := &dmodel.Domain{Domain: "example.com", VerificationToken: "tok123"}

	lookup := func(records []string, err error) LookupTXT {
		return func(_ context.Context, name string) ([]string, error) {
			if name != "example.com" {
				t.Errorf("lookup name = %q, want example.com", name)
			}

			return records, err
		}
	}

	if !CheckOwnership(t.Context(), lookup([]string{"v=spf1 -all", "mailyard-verification=tok123"}, nil), d) {
		t.Error("matching TXT record must verify")
	}

	if CheckOwnership(t.Context(), lookup([]string{"mailyard-verification=other"}, nil), d) {
		t.Error("wrong token must not verify")
	}

	if CheckOwnership(t.Context(), lookup(nil, errors.New("nxdomain")), d) {
		t.Error("lookup failure must not verify")
	}

	if CheckOwnership(t.Context(), lookup([]string{" mailyard-verification=tok123 "}, nil), d) != true {
		t.Error("surrounding whitespace must be tolerated")
	}
}
