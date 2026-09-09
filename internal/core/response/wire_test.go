// Mailyard, Copyright (c) 2021-2026 YouSysAdmin

package response

import (
	"strings"
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
	b, err := Marshal(v)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}

	return string(b)
}

// A list nobody filled is [] on the wire. This pins that the encoder
// the server installs actually formats a nil slice that way.
func TestAnEmptyListIsNotNull(t *testing.T) {
	got := marshal(t, listBody{})

	for _, want := range []string{`"templates":[]`, `"names":[]`, `"items":[]`} {
		if !strings.Contains(got, want) {
			t.Errorf("missing %s in %s", want, got)
		}
	}
}

// omitempty keeps omitting: nil and empty both encode as the empty
// array, so a field that was absent stays absent - the document's
// optional fields cannot start appearing.
func TestAnOmittedListStaysOmitted(t *testing.T) {
	got := marshal(t, listBody{})
	if strings.Contains(got, "optional") {
		t.Errorf("omitempty list appeared: %s", got)
	}
}

// A nil map stays null - the shape the document promises. The v2
// default would move it to {}, which FormatNilMapAsNull holds back.
func TestANilMapStaysNull(t *testing.T) {
	got := marshal(t, listBody{})
	if !strings.Contains(got, `"lookup":null`) {
		t.Errorf(`want "lookup":null in %s`, got)
	}

	if !strings.Contains(got, `"nested":null`) {
		t.Errorf(`want "nested":null in %s`, got)
	}
}

// Response strings come from received mail, and one latin-1 byte in a
// subject must not turn the whole response into a 500. The bad byte is
// coerced to U+FFFD.
func TestABadByteDoesNotFailTheResponse(t *testing.T) {
	got := marshal(t, map[string]string{"subject": "caf\xe9"})
	if !strings.Contains(got, "caf�") {
		t.Errorf("invalid UTF-8 was not coerced: %q", got)
	}
}

// Requests are parsed strictly: a duplicate member and invalid UTF-8
// are refused, and names match case-sensitively - a wrong-case key is
// an unknown member, not a match.
func TestARequestIsParsedStrictly(t *testing.T) {
	var u struct {
		Name string `json:"name"`
	}

	if err := Unmarshal([]byte(`{"name":"a","name":"b"}`), &u); err == nil {
		t.Error("a duplicate member was absorbed")
	}

	if err := Unmarshal([]byte("{\"name\":\"\xff\"}"), &u); err == nil {
		t.Error("invalid UTF-8 was absorbed")
	}

	u.Name = ""
	if err := Unmarshal([]byte(`{"NAME":"x"}`), &u); err != nil {
		t.Errorf("an unknown member should be ignored, got %v", err)
	}

	if u.Name != "" {
		t.Errorf("a wrong-case key matched: %q", u.Name)
	}
}
