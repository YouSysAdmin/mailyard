// Mailyard, Copyright (c) 2021-2026 YouSysAdmin

// Package subscriberlist is the campaign audience: a static member
// list or a dynamic segment described by filter rules. Rules are
// evaluated in Go (not SQL) so both database engines behave
// identically.
package subscriberlist

import (
	"fmt"
	"strconv"
	"strings"
	"time"

	"github.com/yousysadmin/mailyard/internal/models/subscriber"
)

// List types.
const (
	TypeStatic  = "static"
	TypeDynamic = "dynamic"
)

// Filter rule operators.
const (
	OpEq         = "eq"
	OpNeq        = "neq"
	OpContains   = "contains"
	OpStartsWith = "starts_with"
	OpEndsWith   = "ends_with"
	OpGt         = "gt"
	OpLt         = "lt"
	OpExists     = "exists"
)

// ValidOperators enumerates rule operators for input validation.
var ValidOperators = map[string]struct{}{
	OpEq: {}, OpNeq: {}, OpContains: {}, OpStartsWith: {},
	OpEndsWith: {}, OpGt: {}, OpLt: {}, OpExists: {},
}

// FilterRule is one predicate of a dynamic segment. Field addresses
// email, name, status, timezone, language, or custom_fields.<key>.
// All rules must match (AND semantics, mirroring the old platform).
type FilterRule struct {
	Field    string `json:"field"    validate:"required,max=100"`
	Operator string `json:"operator" validate:"required"`
	Value    any    `json:"value"`
}

// List is one audience. FilterRules apply to dynamic lists only and
// are evaluated at send / preview time.
type List struct {
	ID          string       `json:"id"`
	ProjectID   string       `json:"project_id"`
	Name        string       `json:"name"`
	Description string       `json:"description,omitempty"`
	Type        string       `json:"type"`
	FilterRules []FilterRule `json:"filter_rules"`
	CreatedAt   time.Time    `json:"created_at"`
	UpdatedAt   *time.Time   `json:"updated_at,omitempty"`
}

// MatchRules reports whether the subscriber satisfies every rule.
func MatchRules(sub *subscriber.Subscriber, rules []FilterRule) bool {
	for _, r := range rules {
		if !matchRule(sub, r) {
			return false
		}
	}

	return true
}

func matchRule(sub *subscriber.Subscriber, r FilterRule) bool {
	val, ok := fieldValue(sub, r.Field)
	switch r.Operator {
	case OpExists:
		return ok && val != nil
	case OpEq:
		return ok && looseEqual(val, r.Value)
	case OpNeq:
		return !ok || !looseEqual(val, r.Value)
	case OpContains:
		return ok && strings.Contains(lowerStr(val), lowerStr(r.Value))
	case OpStartsWith:
		return ok && strings.HasPrefix(lowerStr(val), lowerStr(r.Value))
	case OpEndsWith:
		return ok && strings.HasSuffix(lowerStr(val), lowerStr(r.Value))
	case OpGt:
		a, aok := toFloat(val)
		b, bok := toFloat(r.Value)

		return ok && aok && bok && a > b
	case OpLt:
		a, aok := toFloat(val)
		b, bok := toFloat(r.Value)

		return ok && aok && bok && a < b
	default:
		return false
	}
}

// fieldValue resolves a rule field against the subscriber. The bool
// reports whether the field exists at all.
func fieldValue(sub *subscriber.Subscriber, field string) (any, bool) {
	if key, found := strings.CutPrefix(field, "custom_fields."); found {
		if sub.CustomFields == nil {
			return nil, false
		}

		v, ok := sub.CustomFields[key]

		return v, ok
	}

	switch field {
	case "email":
		return sub.Email, true
	case "name":
		return sub.Name, true
	case "status":
		return sub.Status, true
	case "timezone":
		return sub.Timezone, true
	case "language":
		return sub.Language, true
	default:
		return nil, false
	}
}

// looseEqual compares string-ly (case-insensitive) or numerically so
// JSON's number-vs-string fuzziness does not surprise rule authors.
func looseEqual(a, b any) bool {
	if af, aok := toFloat(a); aok {
		if bf, bok := toFloat(b); bok {
			return af == bf
		}
	}

	return strings.EqualFold(lowerStr(a), lowerStr(b))
}

func lowerStr(v any) string {
	switch t := v.(type) {
	case string:
		return strings.ToLower(t)
	case nil:
		return ""
	default:
		return strings.ToLower(fmt.Sprintf("%v", t))
	}
}

func toFloat(v any) (float64, bool) {
	switch t := v.(type) {
	case float64:
		return t, true
	case float32:
		return float64(t), true
	case int:
		return float64(t), true
	case int64:
		return float64(t), true
	case string:
		f, err := strconv.ParseFloat(t, 64)

		return f, err == nil
	default:
		return 0, false
	}
}
