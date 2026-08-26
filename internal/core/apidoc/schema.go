// Mailyard, Copyright (c) 2021-2026 YouSysAdmin

// Package apidoc builds the OpenAPI description of the machine API
// from the Go types the handlers actually use.
//
// Shapes are reflected rather than transcribed. A handler declares a
// response struct, the route metadata names it, and this package walks
// it, so changing a field changes the document with no test to enforce
// it. A hand-written document drifts silently - checking its paths
// against the router says nothing about the bodies, and a spec that
// lies about what comes back is worse than none, since clients are
// generated from it.
//
// Reflection cannot know why a field matters, so summaries and
// descriptions stay hand-written in the route metadata beside each
// endpoint.
package apidoc

import (
	"fmt"
	"maps"
	"reflect"
	"strconv"
	"strings"
	"time"
)

// Schema is one OpenAPI schema object. A map rather than a struct
// because the shape is open-ended and it serializes straight to
// JSON or YAML.
type Schema map[string]any

// registry accumulates named component schemas while types are
// walked, so a type used by ten endpoints is defined once and
// referenced ten times.
type registry struct {
	defs map[string]Schema

	// seen guards recursion: a type that refers to itself must not
	// walk forever. The name is reserved before the fields are
	// walked, so a self-reference finds it already present.
	seen map[reflect.Type]string
}

var timeType = reflect.TypeFor[time.Time]()

// SchemaFor returns the schema for v's type, registering any named
// struct types it meets. Pass a zero value (or a nil pointer of the
// type) - only the type is read.
func (r *registry) SchemaFor(t reflect.Type) Schema {
	return r.walk(t, "")
}

func (r *registry) walk(t reflect.Type, validate string) Schema {
	pre, item := splitDive(validate)

	if t.Kind() == reflect.Pointer {
		s := r.walk(t.Elem(), validate)
		// A pointer field is how this codebase says "absent is
		// different from the zero value", which is exactly OpenAPI's
		// nullable.
		out := Schema{}
		maps.Copy(out, s)
		out["nullable"] = true

		return out
	}

	if t == timeType {
		return Schema{"type": "string", "format": "date-time"}
	}

	switch t.Kind() {
	case reflect.String:
		s := Schema{"type": "string"}
		applyStringRules(s, pre)

		return s

	case reflect.Bool:
		return Schema{"type": "boolean"}

	case reflect.Int, reflect.Int8, reflect.Int16, reflect.Int32, reflect.Int64,
		reflect.Uint, reflect.Uint8, reflect.Uint16, reflect.Uint32, reflect.Uint64:
		s := Schema{"type": "integer"}
		applyNumberRules(s, pre)

		return s

	case reflect.Float32, reflect.Float64:
		s := Schema{"type": "number"}
		applyNumberRules(s, pre)

		return s

	case reflect.Slice, reflect.Array:
		// []byte is base64 in JSON, not an array of numbers.
		if t.Kind() == reflect.Slice && t.Elem().Kind() == reflect.Uint8 {
			return Schema{"type": "string", "format": "byte"}
		}

		s := Schema{"type": "array", "items": r.walk(t.Elem(), item)}
		applyArrayRules(s, pre)

		return s

	case reflect.Map:
		return Schema{"type": "object", "additionalProperties": r.walk(t.Elem(), item)}

	case reflect.Interface:
		// any: deliberately free-form. Template variables and custom
		// subscriber fields are defined by the caller, not by us, and
		// inventing a shape for them would be a lie.
		return Schema{}

	case reflect.Struct:
		return r.structSchema(t)
	default:
		return Schema{}
	}
}

// structSchema registers t under its type name and returns a $ref.
// Anonymous structs are inlined instead, since they have no name to
// reference.
func (r *registry) structSchema(t reflect.Type) Schema {
	if name, ok := r.seen[t]; ok {
		return Schema{"$ref": "#/components/schemas/" + name}
	}

	if t.Name() == "" {
		return r.objectSchema(t)
	}

	name := r.reserve(t)
	// Reserve before walking, so a self-referential type finds its
	// own name instead of recursing.
	r.defs[name] = Schema{}
	r.defs[name] = r.objectSchema(t)

	return Schema{"$ref": "#/components/schemas/" + name}
}

// reserve picks a unique component name for t, qualifying with the
// package when two packages export the same type name.
func (r *registry) reserve(t reflect.Type) string {
	name := t.Name()
	if taken, exists := r.nameTaken(name); exists && taken != t {
		pkg := t.PkgPath()
		if _, base, ok := strings.CutLast(pkg, "/"); ok {
			pkg = base
		}

		name = strings.ToUpper(pkg[:1]) + pkg[1:] + name
	}

	r.seen[t] = name

	return name
}

func (r *registry) nameTaken(name string) (reflect.Type, bool) {
	for t, n := range r.seen {
		if n == name {
			return t, true
		}
	}

	return nil, false
}

func (r *registry) objectSchema(t reflect.Type) Schema {
	props := map[string]any{}
	var required []string

	var walkFields func(reflect.Type)
	walkFields = func(t reflect.Type) {
		for f := range t.Fields() {
			// Embedded structs contribute their fields directly, the
			// way encoding/json flattens them.
			if f.Anonymous && f.Type.Kind() == reflect.Struct && f.Tag.Get("json") == "" {
				walkFields(f.Type)
				continue
			}

			if !f.IsExported() {
				continue
			}

			name, omitempty, skip := jsonName(f)
			if skip {
				continue
			}

			validate := f.Tag.Get("validate")
			s := r.walk(f.Type, validate)
			if doc := f.Tag.Get("doc"); doc != "" {
				s["description"] = doc
			}

			props[name] = s
			// Required means the caller must send it: the validator
			// says so and the field is not optional in JSON terms.
			if hasRule(validate, "required") && !omitempty {
				required = append(required, name)
			}
		}
	}
	walkFields(t)

	out := Schema{"type": "object", "properties": props}
	if len(required) > 0 {
		out["required"] = required
	}

	return out
}

// jsonName reads the json tag the way encoding/json does.
func jsonName(f reflect.StructField) (name string, omitempty, skip bool) {
	tag := f.Tag.Get("json")
	if tag == "-" {
		return "", false, true
	}

	parts := strings.Split(tag, ",")
	name = parts[0]
	if name == "" {
		name = f.Name
	}

	for _, p := range parts[1:] {
		// omitzero counts: for what the document derives from this -
		// "may this member be absent" - the two options are the same
		// claim, they only differ in which Go values trigger it.
		if p == "omitempty" || p == "omitzero" {
			omitempty = true
		}
	}

	return name, omitempty, false
}

// splitDive separates rules that apply to a slice from rules that
// apply to its elements. `validate:"required,min=1,dive,max=320"`
// means the slice needs at least one entry and each entry is at most
// 320 characters.
func splitDive(v string) (outer, inner string) {
	before, after, found := strings.Cut(v, "dive")
	if !found {
		return v, ""
	}

	return strings.Trim(before, ","), strings.Trim(after, ",")
}

func hasRule(validate, name string) bool {
	for rule := range strings.SplitSeq(validate, ",") {
		if strings.TrimSpace(rule) == name {
			return true
		}
	}

	return false
}

func ruleValue(validate, name string) (string, bool) {
	for rule := range strings.SplitSeq(validate, ",") {
		k, v, found := strings.Cut(strings.TrimSpace(rule), "=")
		if found && k == name {
			return v, true
		}
	}

	return "", false
}

func applyStringRules(s Schema, validate string) {
	if v, ok := ruleValue(validate, "max"); ok {
		if n, err := strconv.Atoi(v); err == nil {
			s["maxLength"] = n
		}
	}

	if v, ok := ruleValue(validate, "min"); ok {
		if n, err := strconv.Atoi(v); err == nil {
			s["minLength"] = n
		}
	}

	if v, ok := ruleValue(validate, "len"); ok {
		if n, err := strconv.Atoi(v); err == nil {
			s["minLength"], s["maxLength"] = n, n
		}
	}

	if v, ok := ruleValue(validate, "oneof"); ok {
		s["enum"] = strings.Fields(v)
	}

	switch {
	case hasRule(validate, "email"):
		s["format"] = "email"
	case hasRule(validate, "uuid"):
		s["format"] = "uuid"
	case hasRule(validate, "url"):
		s["format"] = "uri"
	}
}

func applyNumberRules(s Schema, validate string) {
	if v, ok := ruleValue(validate, "min"); ok {
		if n, err := strconv.Atoi(v); err == nil {
			s["minimum"] = n
		}
	}

	if v, ok := ruleValue(validate, "max"); ok {
		if n, err := strconv.Atoi(v); err == nil {
			s["maximum"] = n
		}
	}

	if v, ok := ruleValue(validate, "oneof"); ok {
		s["enum"] = strings.Fields(v)
	}
}

func applyArrayRules(s Schema, validate string) {
	if v, ok := ruleValue(validate, "min"); ok {
		if n, err := strconv.Atoi(v); err == nil {
			s["minItems"] = n
		}
	}

	if v, ok := ruleValue(validate, "max"); ok {
		if n, err := strconv.Atoi(v); err == nil {
			s["maxItems"] = n
		}
	}
}

// typeOf resolves the reflect.Type of a metadata value, accepting
// either a zero value or a typed nil pointer.
func typeOf(v any) (reflect.Type, error) {
	if v == nil {
		return nil, fmt.Errorf("apidoc: nil type")
	}

	t := reflect.TypeOf(v)
	for t.Kind() == reflect.Pointer {
		t = t.Elem()
	}

	return t, nil
}
