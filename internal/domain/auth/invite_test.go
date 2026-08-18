// Mailyard, Copyright (c) 2021-2026 YouSysAdmin

package auth

import "strings"

import "testing"

// inviteToken is the whole defence on the post-sign-in redirect, so it
// is tested as one.
//
// The callback builds its destination itself and the only variable in it
// is this token, which is why there is no open redirect to reason about
// here - but that argument only holds while nothing else can get
// through. A path, a scheme, a host or a second parameter must all come
// back empty.
func TestInviteTokenAcceptsOnlyATokenShape(t *testing.T) {
	good := strings.Repeat("a1", 32) // 64 hex characters
	if got := inviteToken(good); got != good {
		t.Errorf("a real token was rejected: %q", got)
	}

	for _, bad := range []string{
		"",
		strings.Repeat("a", 63),           // too short
		strings.Repeat("a", 65),           // too long
		strings.Repeat("A", 64),           // uppercase is not what randomToken emits
		strings.Repeat("g", 64),           // not hex
		"../../../etc/passwd",             // a path
		"https://evil.example/",           // another origin
		"//evil.example",                  // protocol relative
		strings.Repeat("a", 60) + "/../x", // a path smuggled into the right length
		strings.Repeat("a", 62) + "&x",    // a second parameter
		strings.Repeat("a", 62) + "..",    // a dot, which the state serialization splits on
	} {
		if got := inviteToken(bad); got != "" {
			t.Errorf("inviteToken(%q) = %q, want empty", bad, got)
		}
	}
}

// The length check alone is not enough, so this states the property the
// state cookie depends on: whatever survives carries no dot. The
// serialization is dot-separated, so a token with one would shift every
// field after it.
func TestAnAcceptedInviteNeverContainsADot(t *testing.T) {
	for _, raw := range []string{
		strings.Repeat("a", 64),
		strings.Repeat("a", 62) + "..",
		strings.Repeat(".", 64),
		"a." + strings.Repeat("b", 62),
	} {
		if got := inviteToken(raw); strings.Contains(got, ".") {
			t.Errorf("inviteToken(%q) = %q, which would break the state serialization", raw, got)
		}
	}
}
