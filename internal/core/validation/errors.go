// Mailyard, Copyright (c) 2021-2026 YouSysAdmin

package validation

import (
	"errors"
	"fmt"
	"sort"
	"strings"

	"github.com/go-playground/validator/v10"
)

// FieldError is the normalized error item the SPA renders next to
// the offending input. JSON-stable - the SPA's fields[].field matches
// the json tag the user actually sent.
type FieldError struct {
	Field   string `json:"field"`
	Rule    string `json:"rule"`
	Message string `json:"message"`
}

// Humanize converts validator.ValidationErrors into []FieldError.
// Non-validator errors (typically a JSON-decode failure from
// BindAndValidate) collapse to a single field-less entry so the
// caller can still render something.
func Humanize(err error) []FieldError {
	var ve validator.ValidationErrors
	if !errors.As(err, &ve) {
		return []FieldError{{Message: err.Error()}}
	}

	out := make([]FieldError, 0, len(ve))
	for _, fe := range ve {
		out = append(out, FieldError{
			Field:   fe.Field(),
			Rule:    fe.Tag(),
			Message: defaultMessage(fe),
		})
	}

	sort.Slice(out, func(i, j int) bool { return out[i].Field < out[j].Field })

	return out
}

// Summary collapses []FieldError into one human line, suitable for
// the response envelope's "error" string when the SPA isn't yet
// consuming the structured "fields" array.
func Summary(fes []FieldError) string {
	if len(fes) == 0 {
		return "validation failed"
	}

	if len(fes) == 1 {
		return fes[0].Message
	}

	parts := make([]string, 0, len(fes))
	for _, fe := range fes {
		parts = append(parts, fe.Message)
	}

	return "validation failed: " + strings.Join(parts, "; ")
}

func defaultMessage(fe validator.FieldError) string {
	field := friendlyField(fe.Field())
	switch fe.Tag() {
	case "required":
		return field + " is required"
	case "provider":
		return field + " is not a mail provider this build supports"
	case "required_if", "required_unless", "required_with", "required_without":
		return field + " is required when other fields change"
	case "email":
		return field + " must be a valid email"
	case "ipcidr":
		return field + " must be an IP address or CIDR block"
	case "alpha":
		return field + " must be letters only"
	case "certname":
		return field + " may contain letters, digits, dot, dash and underscore"
	case "bcryptlen":
		return field + " must be at most 72 bytes (a long passphrase in a non-latin script hits this sooner than 72 characters)"
	case "uuid", "uuid4", "uuid5":
		return field + " must be a valid UUID"
	case "min":
		// Numeric vs string min/max are surfaced with the same tag
		// in validator/v10 - the parameter is unitless. Keep the
		// message neutral ("at least N") so it reads both for
		// counts ("at least 1") and lengths ("at least 1 character").
		return fmt.Sprintf("%s must be at least %s", field, fe.Param())
	case "max":
		return fmt.Sprintf("%s must be at most %s", field, fe.Param())
	case "oneof":
		return fmt.Sprintf("%s must be one of: %s", field, strings.ReplaceAll(fe.Param(), " ", ", "))
	case "len":
		return fmt.Sprintf("%s must be exactly %s", field, fe.Param())
	case "url":
		return field + " must be a URL"
	case "fqdn":
		return field + " must be a domain name"
	case "hostname_rfc1123":
		return field + " must be a hostname"
	case "json":
		return field + " must be valid JSON"
	case "numeric":
		return field + " must be a number"
	case "startswith":
		return fmt.Sprintf("%s must start with %s", field, fe.Param())
	case "eqfield":
		return fmt.Sprintf("%s must match %s", field, friendlyField(fe.Param()))
	default:
		// Fallback: validator's library message with quotes stripped.
		return strings.ReplaceAll(fe.Error(), "\"", "")
	}
}

// friendlyLabels overrides the auto-titlecase for json tags that
// expand to unidiomatic English (e.g. "smtp_host" -> "SMTP host").
// Add an entry here when a new field's auto-rendered form reads
// awkwardly in error messages.
var friendlyLabels = map[string]string{
	"id": "ID",
}

// friendlyField converts a json tag name to a human-readable label
// suitable for an end-user-facing error message. Known cases come
// from the friendlyLabels map - everything else falls through to a
// sentence-cased version with underscores replaced by spaces
// ("max_concurrent_runners" -> "Max concurrent runners").
//
// The structured FieldError envelope keeps the original json tag in
// FieldError.Field so the SPA can still route the error to the
// matching input - only the Message is humanized.
func friendlyField(jsonField string) string {
	if jsonField == "" {
		return "This field"
	}

	if v, ok := friendlyLabels[jsonField]; ok {
		return v
	}

	parts := strings.Split(jsonField, "_")
	for i, p := range parts {
		if i == 0 && len(p) > 0 {
			parts[i] = strings.ToUpper(p[:1]) + p[1:]
		}
	}

	return strings.Join(parts, " ")
}
