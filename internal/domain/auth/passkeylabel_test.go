// Mailyard, Copyright (c) 2021-2026 YouSysAdmin

package auth

import (
	"strings"
	"testing"
	"unicode/utf8"
)

// The label is cut at characters, never inside one, so the database
// is never handed an invalid byte sequence.
func TestAPasskeyLabelIsCutAtCharacters(t *testing.T) {
	long := strings.Repeat("ключ", 30)
	got := passkeyLabel(long)
	if !utf8.ValidString(got) || utf8.RuneCountInString(got) != maxPasskeyName {
		t.Fatalf("label %q: valid %v, %d characters", got, utf8.ValidString(got), utf8.RuneCountInString(got))
	}

	if got := passkeyLabel("bad \xff byte"); !utf8.ValidString(got) {
		t.Fatalf("invalid input came back invalid: %q", got)
	}

	if got := passkeyLabel("   "); got != "Passkey" {
		t.Fatalf("blank label is %q, want the default", got)
	}
}
