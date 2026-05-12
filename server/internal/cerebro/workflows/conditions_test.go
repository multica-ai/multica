package workflows

import "testing"

func TestEvaluate_EmptyConditionsAlwaysMatch(t *testing.T) {
	if !evaluate(nil, map[string]any{}) {
		t.Fatal("empty conditions must match")
	}
}

func TestEvaluate_DottedFieldLookup(t *testing.T) {
	ctx := map[string]any{
		"issue": map[string]any{
			"priority": "high",
			"project_id": "p-1",
		},
	}
	conds := []Condition{{Field: "issue.priority", Op: "eq", Value: "high"}}
	if !evaluate(conds, ctx) {
		t.Fatal("eq high should match")
	}
	conds[0].Value = "low"
	if evaluate(conds, ctx) {
		t.Fatal("eq low should not match")
	}
}

func TestEvaluate_InOperator(t *testing.T) {
	ctx := map[string]any{"issue": map[string]any{"priority": "urgent"}}
	conds := []Condition{{
		Field:  "issue.priority",
		Op:     "in",
		Values: []any{"urgent", "high"},
	}}
	if !evaluate(conds, ctx) {
		t.Fatal("'urgent' should be in [urgent, high]")
	}
	conds[0].Values = []any{"low", "medium"}
	if evaluate(conds, ctx) {
		t.Fatal("'urgent' should not be in [low, medium]")
	}
}

func TestEvaluate_NullChecks(t *testing.T) {
	ctx := map[string]any{"issue": map[string]any{"assignee_id": nil}}
	isNull := []Condition{{Field: "issue.assignee_id", Op: "is_null"}}
	if !evaluate(isNull, ctx) {
		t.Fatal("explicit nil should satisfy is_null")
	}
	isNotNull := []Condition{{Field: "issue.assignee_id", Op: "is_not_null"}}
	if evaluate(isNotNull, ctx) {
		t.Fatal("explicit nil should fail is_not_null")
	}
}

func TestEvaluate_UnknownOperatorFailsClosed(t *testing.T) {
	ctx := map[string]any{"x": "y"}
	conds := []Condition{{Field: "x", Op: "regex", Value: ".*"}}
	if evaluate(conds, ctx) {
		t.Fatal("unknown operator must fail closed")
	}
}

func TestEvaluate_AllMustHold(t *testing.T) {
	ctx := map[string]any{"issue": map[string]any{"priority": "high", "status": "todo"}}
	conds := []Condition{
		{Field: "issue.priority", Op: "eq", Value: "high"},
		{Field: "issue.status", Op: "eq", Value: "in_progress"},
	}
	if evaluate(conds, ctx) {
		t.Fatal("AND semantics: second condition must fail the whole match")
	}
}

func TestEvaluate_MissingFieldFailsEquality(t *testing.T) {
	ctx := map[string]any{"issue": map[string]any{}}
	conds := []Condition{{Field: "issue.priority", Op: "eq", Value: "high"}}
	if evaluate(conds, ctx) {
		t.Fatal("missing field must not satisfy eq")
	}
}
