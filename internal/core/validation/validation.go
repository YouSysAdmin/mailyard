// Mailyard, Copyright (c) 2021-2026 YouSysAdmin

// Package validation centralizes JSON-body parsing, normalization,
// and struct-tag validation for the API. Domain handlers declare
// their input shape with `validate:"..."` + `normalize:"..."` tags
// and call BindAndValidate - field-level errors come back through
// Humanize for response.BadRequestFields to render.
//
// Tag vocabulary used in this codebase:
//
//	validate:"required"                  field must be set/non-zero
//	validate:"min=N,max=N"               byte-length bounds for strings,
//	                                     element-count bounds for slices/maps
//	validate:"oneof=a b c"               enum-style allowed values
//	validate:"required_if=Field val"     cross-field requirement
//	validate:"dive,..."                  apply remaining rules to each
//	                                     slice/map element
//
//	normalize:"trim"                     strings.TrimSpace
//	normalize:"lower"                    strings.ToLower
//	normalize:"upper"                    strings.ToUpper
//	normalize:"normalize"                trim + lower (the common pair)
//
// Example:
//
//	Type loginRequest struct {
//	    Email    string `json:"email"    validate:"required,email" normalize:"normalize"`
//	    Password string `json:"password" validate:"required,min=1"  normalize:"trim"`
//	}
//
//	func (h Handler) Login(c fiber.Ctx) error {
//	    in, err := validation.BindAndValidate[loginRequest](c)
//	    if err != nil {
//	        return response.BadRequestFields(c, validation.Humanize(err))
//	    }
//	    // ...
//	}
package validation

import (
	"encoding/json/v2"
	"fmt"
	"reflect"
	"strings"
	"sync"

	"github.com/go-playground/validator/v10"
	"github.com/gofiber/fiber/v3"
)

var (
	v    *validator.Validate
	once sync.Once
)

// Init must be called once at app startup before any handler hits
// BindAndValidate. cli/serve.go calls this before server.New.
func Init() *validator.Validate {
	once.Do(func() {
		v = validator.New(validator.WithRequiredStructEnabled())

		// Use the json tag name in error.Field() so error responses
		// match the JSON the SPA actually sent, not the Go field name.
		v.RegisterTagNameFunc(func(fld reflect.StructField) string {
			tag := fld.Tag.Get("json")
			if tag == "" || tag == "-" {
				return fld.Name
			}

			if i := strings.IndexByte(tag, ','); i >= 0 {
				tag = tag[:i]
			}

			return tag
		})

		registerCustom(v)
	})

	return v
}

// V returns the singleton *validator.Validate, initializing it if
// the production startup path didn't (e.g. unit tests that exercise
// a handler without booting cli.serve). sync.Once makes the lazy
// init safe under concurrent access.
func V() *validator.Validate {
	if v == nil {
		Init()
	}

	return v
}

// Normalizer lets DTOs run custom normalization (cross-field
// coercion, defaulting, anything the normalize:"..." tag vocabulary
// doesn't cover) before struct-tag validation runs.
//
// Implement on a pointer receiver - BindAndValidate calls
// (&payload).Normalize(), so a value-receiver method won't fire.
type Normalizer interface {
	Normalize()
}

// fiberCtx is the slice of Fiber's fiber.Ctx that BindAndValidate
// needs. Declared as an interface so unit tests can stub it - in
// practice the only caller passes a real fiber.Ctx.
type fiberCtx interface {
	Body() []byte
}

// BindAndValidate parses the JSON body into T, runs normalization,
// then validates struct-tag rules. On any step's failure the
// (typed-zero, error) pair is returned and the caller writes the
// response.
//
// Parsing is json/v2 with its default strictness, the same policy the
// app decoder carries (response.Unmarshal): a duplicate member, invalid
// UTF-8 or a mistyped value is refused rather than absorbed, and names
// match case-sensitively - a wrong-case key is ignored as unknown, the
// way an unknown key always was.
//
// Order matters:
//  1. JSON unmarshal so the struct exists.
//  2. normalize:"..." tag passes (trim/lower/etc) so a "  Foo  "
//     becomes "foo" before required / min / oneof see it.
//  3. Normalizer.Normalize() so DTO-specific coercion (default
//     fallbacks, cross-field defaulting) sees the cleaned strings.
//  4. validator.Struct so rules consult the final shape.
func BindAndValidate[T any](c fiberCtx) (T, error) {
	var payload T
	if err := json.Unmarshal(c.Body(), &payload); err != nil {
		return payload, fmt.Errorf("decode body: %w", err)
	}

	applyNormalizeTags(reflect.ValueOf(&payload))
	if n, ok := any(&payload).(Normalizer); ok {
		n.Normalize()
	}

	if err := V().Struct(payload); err != nil {
		return payload, err
	}

	return payload, nil
}

// NormalizeAndValidate runs the same normalize-tag pass + Normalizer
// hook + validator.Struct check that BindAndValidate runs, but on a
// value that already exists in memory (i.e. wasn't decoded from a
// Fiber request body). The argument must be a non-nil pointer to a
// struct so the normalize-tag pass can mutate string fields in place.
//
// Use this when validating rows that arrived through a different
// channel than HTTP - e.g. a row nested inside a backup snapshot
// import body - so the same rules apply consistently to both paths.
func NormalizeAndValidate(payload any) error {
	applyNormalizeTags(reflect.ValueOf(payload))
	if n, ok := payload.(Normalizer); ok {
		n.Normalize()
	}

	return V().Struct(payload)
}

// applyNormalizeTags walks v and runs normalize:"..." tags on every
// settable string field (recursing into structs / slices / arrays /
// string-keyed maps). Map values are not mutated in place because
// reflect doesn't allow that for native maps - if a map value needs
// normalization, do it in Normalize().
func applyNormalizeTags(v reflect.Value) {
	if !v.IsValid() {
		return
	}

	for v.Kind() == reflect.Pointer {
		if v.IsNil() {
			return
		}

		v = v.Elem()
	}

	switch v.Kind() {
	case reflect.Struct:
		t := v.Type()
		for i := range v.NumField() {
			fv := v.Field(i)
			ft := t.Field(i)
			applyNormalizeTags(fv)
			if fv.Kind() == reflect.String && fv.CanSet() {
				if tag := ft.Tag.Get("normalize"); tag != "" {
					fv.SetString(applyStringOps(fv.String(), tag))
				}
			}
		}
	case reflect.Slice, reflect.Array:
		for i := range v.Len() {
			applyNormalizeTags(v.Index(i))
		}
	}
}

func applyStringOps(s string, tag string) string {
	for op := range strings.SplitSeq(tag, ",") {
		switch strings.ToLower(strings.TrimSpace(op)) {
		case "trim":
			s = strings.TrimSpace(s)
		case "lower":
			s = strings.ToLower(s)
		case "upper":
			s = strings.ToUpper(s)
		case "normalize":
			s = strings.TrimSpace(strings.ToLower(s))
		}
	}

	return s
}

// Compile-time guard: fiber.Ctx satisfies our minimal interface. If
// Fiber ever renames Body() the build breaks here, which is the
// right place to find out.
var _ fiberCtx = (fiber.Ctx)(nil)
