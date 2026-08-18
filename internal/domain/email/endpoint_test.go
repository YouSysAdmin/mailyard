// Mailyard, Copyright (c) 2021-2026 YouSysAdmin

package email

import (
	"reflect"
	"slices"
	"strconv"
	"strings"
	"testing"
)

// The Limits endpoint reports maxAttachmentsPerEmail, but the value
// actually enforced lives in a struct tag, which no compiler checks
// against the constant. Reporting a cap the validator does not apply
// is worse than reporting none, so assert the two agree.
func TestAttachmentCountLimitMatchesValidateTag(t *testing.T) {
	for _, tc := range []struct {
		name  string
		typ   reflect.Type
		field string
	}{
		{"sendInput", reflect.TypeFor[sendInput](), "Attachments"},
		{"templateSendInput", reflect.TypeFor[templateSendInput](), "Attachments"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			f, ok := tc.typ.FieldByName(tc.field)
			if !ok {
				t.Fatalf("%s has no field %s", tc.name, tc.field)
			}

			want := "max=" + strconv.Itoa(maxAttachmentsPerEmail)
			tag := f.Tag.Get("validate")
			found := slices.Contains(strings.Split(tag, ","), want)
			if !found {
				t.Errorf("validate tag %q does not carry %q, so the limit reported by Limits is not the one enforced", tag, want)
			}
		})
	}
}
