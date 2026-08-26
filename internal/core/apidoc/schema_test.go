// Mailyard, Copyright (c) 2021-2026 YouSysAdmin

package apidoc

import (
	"encoding/json/v2"
	"reflect"
	"strings"
	"testing"
	"time"
)

type inner struct {
	Label string `json:"label"`
}

type sample struct {
	ID       string            `json:"id"       validate:"required,uuid"`
	Email    string            `json:"email"    validate:"required,email,max=320"`
	Kind     string            `json:"kind"     validate:"omitempty,oneof=hard soft complaint"`
	Count    int               `json:"count"    validate:"min=0,max=100"`
	Ratio    float64           `json:"ratio"`
	Enabled  bool              `json:"enabled"`
	To       []string          `json:"to"       validate:"required,min=1,dive,max=320"`
	Headers  map[string]string `json:"headers,omitempty"`
	Free     map[string]any    `json:"free,omitempty"`
	Nested   inner             `json:"nested"`
	Optional *inner            `json:"optional,omitempty"`
	When     time.Time         `json:"when"`
	Maybe    *time.Time        `json:"maybe,omitempty"`
	Secret   string            `json:"-"`

	// Its PRESENCE is the test: the reflector must skip an unexported
	// field. Nothing reads it, and nothing should.
	unseen string //nolint:unused // the fixture exists to be skipped
	Raw    []byte `json:"raw,omitempty"`
}

func build(t *testing.T, v any) (Schema, map[string]Schema) {
	t.Helper()
	r := &registry{defs: map[string]Schema{}, seen: map[reflect.Type]string{}}
	s := r.SchemaFor(reflect.TypeOf(v))

	return s, r.defs
}

// The whole point of reflecting instead of transcribing: what the
// document says a field is comes from the field itself. This pins the
// translation of every kind the API actually uses.
func TestSchemaMirrorsTheStruct(t *testing.T) {
	top, defs := build(t, sample{})

	ref, _ := top["$ref"].(string)
	if ref != "#/components/schemas/sample" {
		t.Fatalf("a named struct must become a $ref, got %v", top)
	}

	def := defs["sample"]
	props, _ := def["properties"].(map[string]any)
	if props == nil {
		t.Fatal("no properties recorded")
	}

	// json:"-" and unexported fields must not leak into a public
	// document - that is how a secret ends up in a generated client.
	if _, ok := props["Secret"]; ok {
		t.Error(`a json:"-" field appeared in the schema`)
	}

	if _, ok := props["unseen"]; ok {
		t.Error("an unexported field appeared in the schema")
	}

	cases := []struct {
		field string
		key   string
		want  any
	}{
		{"id", "format", "uuid"},
		{"email", "format", "email"},
		{"email", "maxLength", 320},
		{"count", "type", "integer"},
		{"count", "maximum", 100},
		{"ratio", "type", "number"},
		{"enabled", "type", "boolean"},
		{"to", "type", "array"},
		{"to", "minItems", 1},
		{"when", "format", "date-time"},
		{"raw", "format", "byte"},
	}
	for _, c := range cases {
		f, _ := props[c.field].(Schema)
		if f == nil {
			t.Errorf("%s: missing", c.field)
			continue
		}

		if got := f[c.key]; got != c.want {
			t.Errorf("%s.%s = %v, want %v", c.field, c.key, got, c.want)
		}
	}

	// dive: the max applies to the ELEMENTS, not to the slice.
	to, _ := props["to"].(Schema)
	items, _ := to["items"].(Schema)
	if items["maxLength"] != 320 {
		t.Errorf("dive rules must land on the items, got %v", items)
	}

	if to["maxLength"] != nil {
		t.Error("an element rule leaked onto the array itself")
	}

	// oneof becomes an enum, so a generator produces a closed type.
	kind, _ := props["kind"].(Schema)
	enum, _ := kind["enum"].([]string)
	if len(enum) != 3 || enum[0] != "hard" {
		t.Errorf("oneof did not become an enum: %v", kind)
	}

	// A pointer is this codebase's way of saying absent differs from
	// the zero value, which is exactly nullable.
	opt, _ := props["optional"].(Schema)
	if opt["nullable"] != true {
		t.Errorf("a pointer field must be nullable: %v", opt)
	}

	// A free-form map stays free-form. Inventing a shape for template
	// variables would be a lie.
	free, _ := props["free"].(Schema)
	ap, _ := free["additionalProperties"].(Schema)
	if len(ap) != 0 {
		t.Errorf("map[string]any must stay open, got %v", ap)
	}

	// required comes from the validator, and omitempty excludes a
	// field from it.
	req, _ := def["required"].([]string)
	found := map[string]bool{}
	for _, r := range req {
		found[r] = true
	}

	if !found["id"] || !found["email"] || !found["to"] {
		t.Errorf("required is %v, want id, email and to", req)
	}

	if found["kind"] || found["headers"] {
		t.Errorf("optional fields must not be required: %v", req)
	}

	// A nested named struct is registered once and referenced.
	if _, ok := defs["inner"]; !ok {
		t.Error("a nested struct was not registered as a component")
	}
}

type node struct {
	Name     string  `json:"name"`
	Parent   *node   `json:"parent,omitempty"`
	Children []*node `json:"children,omitempty"`
}

// A self-referential type must terminate. The name is reserved before
// the fields are walked, so the recursion finds a $ref waiting.
func TestRecursiveTypeTerminates(t *testing.T) {
	done := make(chan map[string]Schema, 1)
	go func() {
		_, defs := build(t, node{})
		done <- defs
	}()
	select {
	case defs := <-done:
		props, _ := defs["node"]["properties"].(map[string]any)
		parent, _ := props["parent"].(Schema)
		if parent["$ref"] != "#/components/schemas/node" {
			t.Errorf("self-reference should be a $ref, got %v", parent)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("reflecting a recursive type did not terminate")
	}
}

// The document has to survive being serialized - a Schema holding
// something json cannot encode would fail at request time, on the one
// endpoint whose job is describing the others.
func TestBuiltDocumentSerializes(t *testing.T) {
	doc, err := Build(Info{Title: "t", Version: "1", ServerURL: "/api/v1"}, []Route{{
		Method:     "POST",
		Path:       "/things/:id",
		Tag:        "things",
		Summary:    "Do a thing",
		Permission: "things:write",
		PathParams: []Param{{Name: "id", Type: "string", Format: "uuid"}},
		Query:      []Param{{Name: "limit", Type: "integer"}},
		Request:    sample{},
		Responses: []Response{
			{Status: 201, Description: "made", Body: sample{}},
			{Status: 204, Description: "nothing to say"},
		},
	}})
	if err != nil {
		t.Fatalf("build: %v", err)
	}

	if _, err := json.Marshal(doc); err != nil {
		t.Fatalf("document does not serialize: %v", err)
	}

	paths, _ := doc["paths"].(map[string]any)
	// Fiber's :id must be rewritten - OpenAPI does not know that form,
	// and a client generated from it would call a literal ":id".
	if _, ok := paths["/things/{id}"]; !ok {
		t.Errorf("path not rewritten to OpenAPI form: %v", paths)
	}

	op, _ := paths["/things/{id}"].(map[string]any)["post"].(map[string]any)
	if op == nil {
		t.Fatal("no post operation")
	}

	desc, _ := op["description"].(string)
	if desc == "" || !strings.Contains(desc, "`things:write` permission") {
		t.Errorf("the required permission should reach the description, got %q", desc)
	}

	resp, _ := op["responses"].(map[string]any)
	noBody, _ := resp["204"].(map[string]any)
	if _, ok := noBody["content"]; ok {
		t.Error("a 204 must not carry a content schema")
	}
}
