// Mailyard, Copyright (c) 2021-2026 YouSysAdmin

package database

import (
	"encoding/json/jsontext"
	"encoding/json/v2"
	"fmt"
	"strconv"
	"strings"
	"time"
)

// Rebind converts the `?` placeholders stores are written with into
// Postgres's $1..$N.
//
// Stores keep writing `?` because it reads far better than counting
// positions by hand in a twenty-column INSERT, and because a reordered
// argument list then cannot silently point at the wrong placeholder.
//
// The scan is textual: it does not skip `?` inside string literals or
// comments. Keep literal question marks out of store SQL (pass them
// as bind args instead) - true for everything in this codebase.
func Rebind(query string) string {
	var b strings.Builder
	b.Grow(len(query) + 8)
	n := 0
	for _, r := range query {
		if r == '?' {
			n++
			b.WriteByte('$')
			b.WriteString(strconv.Itoa(n))
			continue
		}

		b.WriteRune(r)
	}

	return b.String()
}

// NullStr returns nil for empty strings so they land as SQL NULL,
// otherwise returns the string itself.
// Use as the arg for nullable TEXT columns.
func NullStr(s string) any {
	if s == "" {
		return nil
	}

	return s
}

// Str is the read half of NullStr: a scan target that turns SQL NULL
// into the empty string.
//
// The id columns that mean "none" are nullable UUID, because "" is not
// a uuid and a uuid column will not take it. Go keeps saying "" - the
// models are plain strings and every caller compares against "" - so
// the translation lives here, at the two edges, rather than turning
// thirty model fields into sql.NullString.
//
//	rows.Scan(&e.ID, database.Str(&e.SMTPGroupID), ...)
func Str(dst *string) any { return nullStrScanner{dst} }

type nullStrScanner struct{ dst *string }

// Scan decodes the stored JSON into v. Empty scans as the zero value
// rather than failing, since a column added later is empty on every
// existing row.
func (n nullStrScanner) Scan(v any) error {
	switch t := v.(type) {
	case nil:
		*n.dst = ""
	case string:
		*n.dst = t
	case []byte:
		*n.dst = string(t)
	default:
		return fmt.Errorf("database.Str: cannot scan %T into a string", v)
	}

	return nil
}

// NullTime returns nil when t is nil, otherwise returns the
// dereferenced value.
// Use for nullable TIMESTAMP / DATETIME columns.
func NullTime(t *time.Time) any {
	if t == nil {
		return nil
	}

	return *t
}

// EscapeLike neutralizes the LIKE metacharacters in a value that is
// being interpolated into a pattern, for use with `LIKE ? ESCAPE '\'`.
//
// A parameter placeholder stops SQL injection but not pattern
// injection: the driver passes the string through as data, and LIKE
// still reads % and _ inside it as wildcards. That is harmless in a
// search box and dangerous in a DELETE - "%@example.com" passes email
// validation and, unescaped, matches every address at that domain.
//
// The backslash is escaped first, otherwise escaping % and _ would
// produce sequences this pass then mangles.
func EscapeLike(s string) string {
	s = strings.ReplaceAll(s, `\`, `\\`)
	s = strings.ReplaceAll(s, "%", `\%`)

	return strings.ReplaceAll(s, "_", `\_`)
}

// storedJSONOptions is how a Go value becomes column text, stated once
// for the same reason MustJSON exists at all - eighteen call sites
// agreeing by hand is how the sentinel bug started.
//
// A nil slice is `[]` and a nil map is `{}`, never `null` - the v2
// default, and the columns these feed are all NOT NULL DEFAULT '[]' or
// '{}'. A round trip hides the difference, since `null` reads back as a
// nil slice, but any predicate written against the sentinel breaks: the
// retention sweeps look for an attachments_json that is neither the
// empty array nor the empty string, and `null` matches that, so they
// rewrite every settled row in the window whether it had an attachment
// or not. MustJSON used to normalize the top level by hand - the v2
// default reaches NESTED nils too, which the hand pass never did.
//
// Deterministic, because v1 sorted map keys and v2 does not: two writes
// of the same headers map must produce the same column bytes, or
// anything computed over them - a diff, a test expectation, an etag -
// flaps between runs.
//
// Invalid UTF-8 is coerced to U+FFFD rather than refused, which is what
// v1 always did. These bytes are header values and subjects taken from
// received mail, and Postgres refuses invalid UTF-8 in a TEXT column -
// so the coercion is load-bearing, not a leniency: without it one
// latin-1 byte in a header makes the write panic instead of storing the
// message.
var storedJSONOptions = json.JoinOptions(
	json.Deterministic(true),
	jsontext.AllowInvalidUTF8(true),
)

// MustJSON marshals v as JSON and panics on error. Stores serialize
// Go-native maps and slices that json.Marshal cannot fail on, so a
// panic means a programmer bug - a chan or func in a model field -
// and crashing beats writing a corrupt column. It returns []byte so
// callers can pass it straight to driver args. The exact bytes are
// storedJSONOptions' concern - see there.
func MustJSON(v any) []byte {
	b, err := json.Marshal(v, storedJSONOptions)
	if err != nil {
		panic(fmt.Sprintf("dbutil.MustJSON: %v (input: %#v)", err, v))
	}

	return b
}

// MustUnmarshalJSON is the read-side complement to MustJSON: parse
// store-emitted JSON into v. An error here means the row in the DB
// was tampered with or written by a foreign tool, both of which are
// hard failures rather than recoverable conditions. Empty input
// behaves as a no-op so callers can safely round-trip through stores
// that default empty TEXT columns to "".
func MustUnmarshalJSON(raw string, v any) {
	if raw == "" {
		return
	}

	if err := json.Unmarshal([]byte(raw), v); err != nil {
		panic(fmt.Sprintf("dbutil.MustUnmarshalJSON: %v (raw: %q)", err, raw))
	}
}
