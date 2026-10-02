// Mailyard, Copyright (c) 2021-2026 YouSysAdmin

package render

import (
	"cmp"
	"errors"
	"fmt"
	"math"
	"reflect"
)

// Template data arrives as JSON, and the builtin comparisons refuse to
// compare a float with an int, so `{{ if lt price 10 }}` would fail on
// a price of 9.5. The comparisons below stand in for the builtins and
// compare any two numbers by value. Everything else behaves as the
// builtins do.

var (
	errBadComparison = errors.New("incompatible types for comparison")
	errNoComparison  = errors.New("missing argument for comparison")
)

type kind int

const (
	otherKind kind = iota
	boolKind
	intKind
	uintKind
	floatKind
	stringKind
)

func classify(v reflect.Value) kind {
	switch v.Kind() {
	case reflect.Bool:
		return boolKind
	case reflect.Int, reflect.Int8, reflect.Int16, reflect.Int32, reflect.Int64:
		return intKind
	case reflect.Uint, reflect.Uint8, reflect.Uint16, reflect.Uint32, reflect.Uint64, reflect.Uintptr:
		return uintKind
	case reflect.Float32, reflect.Float64:
		return floatKind
	case reflect.String:
		return stringKind
	default:
		return otherKind
	}
}

func isNumber(k kind) bool {
	return k == intKind || k == uintKind || k == floatKind
}

// order compares two values that can be ordered: numbers of any kind
// against each other, strings against strings.
func order(a, b reflect.Value) (int, error) {
	ka, kb := classify(a), classify(b)
	switch {
	case ka == stringKind && kb == stringKind:
		return cmp.Compare(a.String(), b.String()), nil
	case !isNumber(ka) || !isNumber(kb):
		return 0, errBadComparison
	case ka == intKind && kb == intKind:
		return cmp.Compare(a.Int(), b.Int()), nil
	case ka == uintKind && kb == uintKind:
		return cmp.Compare(a.Uint(), b.Uint()), nil
	case ka == intKind && kb == uintKind:
		if a.Int() < 0 {
			return -1, nil
		}

		return cmp.Compare(uint64(a.Int()), b.Uint()), nil
	case ka == uintKind && kb == intKind:
		if b.Int() < 0 {
			return 1, nil
		}

		return cmp.Compare(a.Uint(), uint64(b.Int())), nil
	default:
		return cmp.Compare(asFloat(a), asFloat(b)), nil
	}
}

func asFloat(v reflect.Value) float64 {
	switch classify(v) {
	case intKind:
		return float64(v.Int())
	case uintKind:
		return float64(v.Uint())
	default:
		return v.Float()
	}
}

func indirect(v any) reflect.Value {
	rv := reflect.ValueOf(v)
	for rv.Kind() == reflect.Interface {
		rv = rv.Elem()
	}

	return rv
}

func equal(a, b reflect.Value) (bool, error) {
	ka, kb := classify(a), classify(b)
	switch {
	case !a.IsValid() || !b.IsValid():
		return a.IsValid() == b.IsValid(), nil
	case isNumber(ka) || isNumber(kb) || ka == stringKind || kb == stringKind:
		c, err := order(a, b)

		return c == 0, err
	case ka == boolKind && kb == boolKind:
		return a.Bool() == b.Bool(), nil
	case ka != kb:
		return false, errBadComparison
	case isNil(a) || isNil(b):
		return isNil(a) == isNil(b), nil
	case !a.Type().Comparable() || !b.Type().Comparable():
		return false, errBadComparison
	default:
		return a.Interface() == b.Interface(), nil
	}
}

func isNil(v reflect.Value) bool {
	switch v.Kind() {
	case reflect.Chan, reflect.Func, reflect.Interface, reflect.Map, reflect.Pointer, reflect.Slice:
		return v.IsNil()
	default:
		return false
	}
}

// eqFunc is true when the first argument equals any of the others.
func eqFunc(a any, bs ...any) (bool, error) {
	if len(bs) == 0 {
		return false, errNoComparison
	}

	for _, b := range bs {
		ok, err := equal(indirect(a), indirect(b))
		if err != nil || ok {
			return ok, err
		}
	}

	return false, nil
}

func neFunc(a, b any) (bool, error) {
	ok, err := equal(indirect(a), indirect(b))

	return !ok, err
}

func orderFunc(want func(int) bool) func(a, b any) (bool, error) {
	return func(a, b any) (bool, error) {
		c, err := order(indirect(a), indirect(b))
		if err != nil {
			return false, err
		}

		return want(c), nil
	}
}

var comparisons = map[string]any{
	"eq":    eqFunc,
	"ne":    neFunc,
	"lt":    orderFunc(func(c int) bool { return c < 0 }),
	"le":    orderFunc(func(c int) bool { return c <= 0 }),
	"gt":    orderFunc(func(c int) bool { return c > 0 }),
	"ge":    orderFunc(func(c int) bool { return c >= 0 }),
	"slice": sliceFunc,
}

// sliceFunc is the builtin slice, reading its indexes through an
// interface. A value looked up in map[string]any is an interface, and
// the builtin refuses one as an index.
func sliceFunc(item any, idx ...any) (any, error) {
	v := indirect(item)
	if !v.IsValid() {
		return nil, errors.New("slice of untyped nil")
	}

	if len(idx) > 3 {
		return nil, fmt.Errorf("too many slice indexes: %d", len(idx))
	}

	var limit int
	switch v.Kind() {
	case reflect.String:
		if len(idx) == 3 {
			return nil, errors.New("cannot 3-index slice a string")
		}

		limit = v.Len()
	case reflect.Slice:
		limit = v.Cap()
	default:
		return nil, fmt.Errorf("can't slice item of type %s", v.Type())
	}

	at := []int{0, v.Len()}
	for i, x := range idx {
		n := indirect(x)
		var k int64
		switch classify(n) {
		case intKind:
			k = n.Int()
		case uintKind:
			k = int64(n.Uint())
		default:
			return nil, fmt.Errorf("cannot index slice/array with type %s", n.Type())
		}

		if k < 0 || k > int64(limit) {
			return nil, fmt.Errorf("index out of range: %d", k)
		}

		if i < 2 {
			at[i] = int(k)
		} else {
			at = append(at, int(k))
		}
	}

	if at[0] > at[1] {
		return nil, fmt.Errorf("invalid slice index: %d > %d", at[0], at[1])
	}

	if len(at) == 3 {
		if at[1] > at[2] {
			return nil, fmt.Errorf("invalid slice index: %d > %d", at[1], at[2])
		}

		return v.Slice3(at[0], at[1], at[2]).Interface(), nil
	}

	return v.Slice(at[0], at[1]).Interface(), nil
}

// wholeNumbers answers a copy of data with every float64 holding a
// whole number turned into an int64, through nested maps and lists.
// A JSON number is always a float64, and range over a number, index,
// slice and %d all want an integer.
func wholeNumbers(data map[string]any) map[string]any {
	if data == nil {
		return nil
	}

	out := make(map[string]any, len(data))
	for k, v := range data {
		out[k] = wholeNumber(v)
	}

	return out
}

func wholeNumber(v any) any {
	switch v := v.(type) {
	case float64:
		if v == math.Trunc(v) && v >= math.MinInt64 && v < math.MaxInt64 {
			return int64(v)
		}

		return v
	case map[string]any:
		return wholeNumbers(v)
	case []any:
		out := make([]any, len(v))
		for i, e := range v {
			out[i] = wholeNumber(e)
		}

		return out
	default:
		return v
	}
}
