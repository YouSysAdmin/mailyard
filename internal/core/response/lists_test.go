// Mailyard, Copyright (c) 2021-2026 YouSysAdmin

package response

import (
	"encoding/json"
	"testing"
)

type inner struct {
	Tags []string `json:"tags"`
}

type listBody struct {
	Templates []inner           `json:"templates"`
	Names     []string          `json:"names"`
	Optional  []string          `json:"optional,omitempty"`
	Raw       []byte            `json:"raw"`
	Nested    *inner            `json:"nested"`
	Items     []*inner          `json:"items"`
	Lookup    map[string]string `json:"lookup"`
	Count     int               `json:"count"`
}

func marshal(t *testing.T, v any) string {
	t.Helper()
	b, err := json.Marshal(withEmptyLists(v))
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}

	return string(b)
}

// The whole point: a list nobody filled is [] on the wire.
//
// Handlers pass a struct VALUE, which reflect cannot set a field
// through, so this also pins the addressable copy - without it the walk
// runs, finds every field unsettable, and changes nothing at all.
func TestAnEmptyListIsNotNull(t *testing.T) {
	got := marshal(t, listBody{})

	for _, want := range []string{`"templates":[]`, `"names":[]`, `"items":[]`} {
		if !contains(got, want) {
			t.Errorf("missing %s in %s", want, got)
		}
	}

	if contains(got, `null`) && !contains(got, `"nested":null`) && !contains(got, `"lookup":null`) {
		t.Errorf("an unexpected null survived: %s", got)
	}
}

// Nested lists too - one level down is where a response type actually
// keeps most of them.
func TestTheWalkReachesNestedLists(t *testing.T) {
	got := marshal(t, listBody{Items: []*inner{{}}, Templates: []inner{{}}})

	if !contains(got, `"items":[{"tags":[]}]`) {
		t.Errorf("a list inside a pointer element stayed null: %s", got)
	}

	if !contains(got, `"templates":[{"tags":[]}]`) {
		t.Errorf("a list inside a value element stayed null: %s", got)
	}
}

// []byte is a string on the wire. Filling it in would turn null into
// "", which is a different change and not one anybody asked for.
func TestRawBytesAreLeftAlone(t *testing.T) {
	if got := marshal(t, listBody{}); !contains(got, `"raw":null`) {
		t.Errorf("a nil []byte must stay null: %s", got)
	}
}

// omitempty omits an empty slice exactly as it omits a nil one, so
// these fields cannot move - which is what bounds the change to the
// fields the document says are always present.
func TestAnOmitemptyListStaysAbsent(t *testing.T) {
	if got := marshal(t, listBody{}); contains(got, `"optional"`) {
		t.Errorf("an omitempty list must not appear: %s", got)
	}
}

// A populated list is handed back exactly as it was.
func TestAFilledListIsUnchanged(t *testing.T) {
	got := marshal(t, listBody{Names: []string{"a", "b"}, Count: 2})

	if !contains(got, `"names":["a","b"]`) || !contains(got, `"count":2`) {
		t.Errorf("values changed: %s", got)
	}
}

// Pointers and maps are not lists and must come back as they were - a
// nil pointer is a meaningful null, not an empty object.
func TestNilPointersAndMapsAreNotTouched(t *testing.T) {
	got := marshal(t, listBody{})

	if !contains(got, `"nested":null`) {
		t.Errorf("a nil pointer must stay null: %s", got)
	}

	if !contains(got, `"lookup":null`) {
		t.Errorf("a nil map must stay null: %s", got)
	}
}

func TestNilDataSurvives(t *testing.T) {
	if withEmptyLists(nil) != nil {
		t.Error("nil in, nil out")
	}
}

func contains(haystack, needle string) bool {
	return len(haystack) >= len(needle) && indexOf(haystack, needle) >= 0
}

func indexOf(haystack, needle string) int {
	for i := 0; i+len(needle) <= len(haystack); i++ {
		if haystack[i:i+len(needle)] == needle {
			return i
		}
	}

	return -1
}
