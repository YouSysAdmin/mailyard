// Mailyard, Copyright (c) 2021-2026 YouSysAdmin

package setting

import "testing"

// Ref is a closed set the console switches on. A kind nobody renders
// would fall through to a text box and show an id.
func TestEveryRefIsAKnownKind(t *testing.T) {
	for _, d := range Registry {
		switch d.Ref {
		case "", RefProject:
		default:
			t.Errorf("%s: ref %q is not a kind the console renders", d.Key, d.Ref)
		}

		if d.Ref != "" && d.Type != TypeString {
			t.Errorf("%s: a ref setting holds an id and must be a string, not %s", d.Key, d.Type)
		}
	}
}

// An int setting with no ceiling takes any int64, and a day count that
// large wraps the retention cutoff into the future.
func TestEveryIntSettingDeclaresACeiling(t *testing.T) {
	for _, d := range Registry {
		if d.Type == TypeInt && d.Max <= 0 {
			t.Errorf("%s: an int setting must declare Max", d.Key)
		}

		if d.Type != TypeInt && d.Max != 0 {
			t.Errorf("%s: Max applies to int settings only", d.Key)
		}
	}
}
