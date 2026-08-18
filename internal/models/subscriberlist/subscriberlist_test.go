package subscriberlist

import (
	"testing"

	"github.com/yousysadmin/mailyard/internal/models/subscriber"
)

func sub() *subscriber.Subscriber {
	return &subscriber.Subscriber{
		Email:  "ada@example.com",
		Name:   "Ada",
		Status: subscriber.StatusSubscribed,
		CustomFields: map[string]any{
			"plan":   "pro",
			"seats":  float64(12),
			"city":   "Berlin",
			"beta":   true,
			"budget": "250",
		},
	}
}

func TestMatchRules(t *testing.T) {
	cases := []struct {
		name  string
		rules []FilterRule
		want  bool
	}{
		{"empty rules match", nil, true},
		{"eq direct field", []FilterRule{{Field: "status", Operator: OpEq, Value: "subscribed"}}, true},
		{"eq case insensitive", []FilterRule{{Field: "name", Operator: OpEq, Value: "ADA"}}, true},
		{"eq custom field", []FilterRule{{Field: "custom_fields.plan", Operator: OpEq, Value: "pro"}}, true},
		{"eq number vs string", []FilterRule{{Field: "custom_fields.seats", Operator: OpEq, Value: "12"}}, true},
		{"neq mismatch", []FilterRule{{Field: "custom_fields.plan", Operator: OpNeq, Value: "free"}}, true},
		{"neq missing field is true", []FilterRule{{Field: "custom_fields.ghost", Operator: OpNeq, Value: "x"}}, true},
		{"contains", []FilterRule{{Field: "email", Operator: OpContains, Value: "@example"}}, true},
		{"starts_with", []FilterRule{{Field: "custom_fields.city", Operator: OpStartsWith, Value: "ber"}}, true},
		{"ends_with", []FilterRule{{Field: "email", Operator: OpEndsWith, Value: ".com"}}, true},
		{"gt number", []FilterRule{{Field: "custom_fields.seats", Operator: OpGt, Value: 10}}, true},
		{"gt string number", []FilterRule{{Field: "custom_fields.budget", Operator: OpGt, Value: 100}}, true},
		{"lt fails", []FilterRule{{Field: "custom_fields.seats", Operator: OpLt, Value: 10}}, false},
		{"exists", []FilterRule{{Field: "custom_fields.beta", Operator: OpExists}}, true},
		{"exists missing", []FilterRule{{Field: "custom_fields.ghost", Operator: OpExists}}, false},
		{"and semantics all must match", []FilterRule{
			{Field: "custom_fields.plan", Operator: OpEq, Value: "pro"},
			{Field: "custom_fields.city", Operator: OpEq, Value: "paris"},
		}, false},
		{"unknown operator never matches", []FilterRule{{Field: "email", Operator: "regex", Value: ".*"}}, false},
		{"unknown field eq fails", []FilterRule{{Field: "shoe_size", Operator: OpEq, Value: "42"}}, false},
	}
	for _, tc := range cases {
		if got := MatchRules(sub(), tc.rules); got != tc.want {
			t.Errorf("%s: MatchRules = %v, want %v", tc.name, got, tc.want)
		}
	}
}
