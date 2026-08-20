// Mailyard, Copyright (c) 2021-2026 YouSysAdmin

package response

import "reflect"

// maxListDepth bounds the walk below. Response types are trees a few
// levels deep - this is a backstop against a shape nobody expected, not
// a limit anything legitimate reaches.
const maxListDepth = 12

// withEmptyLists returns data with every nil slice replaced by an empty
// one, so a list that happens to be empty marshals as [] and never as
// null.
//
// AN EMPTY LIST WAS TWO SHAPES ON THE WIRE. `out := []T{}` marshals to
// [], `var out []T` marshals to null, and the tree did both - seven
// stores built the empty slice and fifty-eight accumulators did not, so
// GET /templates, /webhooks, /contacts, /plans and /domains answered
// {"templates": null} on an empty project. The published document
// declares 142 array properties and marks 4 of them nullable, so for the
// other 138 it was simply wrong.
//
// Go absorbs it - null unmarshals into a nil slice - but the generated
// Python and Ruby clients do not: `for t in resp["templates"]` raises
// where it should iterate zero times. The console only survives because
// every consumer writes `?? []`.
//
// DONE HERE rather than at each accumulator, for the reason
// Server.Normalize is done in the store: fifty-eight edits are fifty-
// eight chances to miss one, and the fifty-ninth accumulator would
// arrive nil having been told nothing. A response goes out through
// Success or Created or it does not go out.
//
// The change is confined by construction. A field carrying omitempty is
// omitted for a nil slice AND for an empty one, so those are untouched -
// what moves is exactly the fields the document promises are there. And
// []byte is skipped: nil marshals to null but empty marshals to "",
// which would be a second, unwanted change.
func withEmptyLists(data any) any {
	if data == nil {
		return data
	}

	v := reflect.ValueOf(data)
	// An addressable copy. Handlers hand us a struct VALUE, and reflect
	// cannot set a field through one - the walk would silently do
	// nothing, which is the failure mode this whole function exists to
	// remove.
	box := reflect.New(v.Type()).Elem()
	box.Set(v)
	fillEmptyLists(box, 0)

	return box.Interface()
}

// fillEmptyLists is the walk. Only ever turns nil into empty, never the
// other way and never anything else.
func fillEmptyLists(v reflect.Value, depth int) {
	if depth > maxListDepth || !v.IsValid() {
		return
	}

	switch v.Kind() {
	case reflect.Pointer, reflect.Interface:
		if !v.IsNil() {
			fillEmptyLists(v.Elem(), depth+1)
		}

	case reflect.Struct:
		for i := range v.NumField() {
			if f := v.Field(i); f.CanSet() {
				fillEmptyLists(f, depth+1)
			}
		}

	case reflect.Slice:
		// []byte is a string on the wire, not a list. Empty would
		// marshal as "" where nil marshals as null, so filling it in
		// would change a shape this is not about.
		if v.Type().Elem().Kind() == reflect.Uint8 {
			return
		}

		if v.IsNil() {
			if v.CanSet() {
				v.Set(reflect.MakeSlice(v.Type(), 0, 0))
			}

			return
		}

		for i := range v.Len() {
			fillEmptyLists(v.Index(i), depth+1)
		}

	case reflect.Array:
		for i := range v.Len() {
			fillEmptyLists(v.Index(i), depth+1)
		}
	}
}
