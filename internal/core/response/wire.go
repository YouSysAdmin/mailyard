// Mailyard, Copyright (c) 2021-2026 YouSysAdmin

package response

import (
	"encoding/json/jsontext"
	"encoding/json/v2"
)

// marshalOptions is the wire policy, stated once. Everything the
// product sends as JSON - the HTTP responses through Fiber, the event
// stream, the outgoing webhooks - marshals under these options, so what
// a shape looks like on the wire is decided here and nowhere else.
//
// AN EMPTY LIST IS []. `out := []T{}` marshals to [] and `var out []T`
// marshals to null, and the tree builds BOTH at the accumulators - so
// the encoder formats a nil slice as [], and it never reaches the wire.
// This used to be a reflective walk over every response body
// (withEmptyLists); json/v2 makes it the encoder's default, and the
// walk, its addressable copy and its depth cap are gone with it.
//
// A nil MAP STAYS null, deliberately. The v2 default would format it as
// {}, but the published document and both script SDKs were proven
// against null maps field by field, and unlike the lists nobody was
// being lied to about them - so that shape does not move.
//
// INVALID UTF-8 IS COERCED, NOT REFUSED. Response bodies carry strings
// taken from received mail - subjects, header values - and a latin-1
// byte in one of those must not turn a 200 into a 500. v1 replaced the
// bad byte with U+FFFD and this keeps that: strict output would make
// the whole response fail over one byte the sender got wrong.
//
// A nil []byte becomes "" where v1 wrote null. No wire type carries a
// []byte today, so nothing observes it - stated here so the first one
// added is a decision and not a surprise.
var marshalOptions = json.JoinOptions(
	json.FormatNilMapAsNull(true),
	jsontext.AllowInvalidUTF8(true),
)

// Marshal renders v under the product's wire policy - see
// marshalOptions for what that policy is and why.
func Marshal(v any) ([]byte, error) {
	return json.Marshal(v, marshalOptions)
}

// Unmarshal parses request JSON under json/v2's defaults, which are
// deliberately stricter than v1's: a duplicate object member, invalid
// UTF-8, or a mistyped value is refused instead of silently absorbed,
// and field names match case-sensitively. The options are the zero set
// - this function exists so the request-parsing policy has ONE name,
// not so it can carry configuration.
func Unmarshal(data []byte, v any) error {
	return json.Unmarshal(data, v)
}
