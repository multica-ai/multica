package proposalkinds

import (
	"context"
	"encoding/json"
	"testing"
)

// TestGoalFilingRefusesMalformedPayloadsBeforeAnyLookup: every check that needs no
// database runs first, so these filings are refused with a nil query handle.
func TestGoalFilingRefusesMalformedPayloadsBeforeAnyLookup(t *testing.T) {
	k := NewGoalKind(nil)
	valid := func(mutate func(m map[string]any)) []byte {
		m := map[string]any{
			"title": "Ship onboarding", "period_start": "2026-10-01", "period_end": "2026-12-31",
			"key_results": []any{map[string]any{"title": "Teams", "metric_key": "runs.count", "start_value": 0, "target_value": 20}},
		}
		mutate(m)
		raw, err := json.Marshal(m)
		if err != nil {
			t.Fatal(err)
		}
		return raw
	}
	cases := map[string][]byte{
		"no title":         valid(func(m map[string]any) { m["title"] = "  " }),
		"unknown field":    valid(func(m map[string]any) { m["colour"] = "red" }),
		"not a date":       valid(func(m map[string]any) { m["period_start"] = "October" }),
		"period backwards": valid(func(m map[string]any) { m["period_end"] = "2026-09-01" }),
		"target equals start": valid(func(m map[string]any) {
			m["key_results"] = []any{map[string]any{"title": "x", "metric_key": "runs.count", "start_value": 3, "target_value": 3}}
		}),
		"weight out of range": valid(func(m map[string]any) {
			m["key_results"] = []any{map[string]any{"title": "x", "metric_key": "runs.count", "start_value": 0, "target_value": 3, "weight": 11}}
		}),
		"not an object": []byte(`[1,2]`),
	}
	for name, payload := range cases {
		if _, err := k.Validate(context.Background(), Env{}, payload); err == nil {
			t.Errorf("%s: accepted", name)
		}
	}
}

// TestGoalKindIsFiledByAgentsAndSummarises: squads file goals, the summary names
// the goal and its period, and the key result count is right.
func TestGoalKindIsFiledByAgentsAndSummarises(t *testing.T) {
	k := NewGoalKind(nil)
	if k.Key() != KeyGoal || !k.FiledByAgents() {
		t.Fatalf("goal kind: key %q, filed by agents %v", k.Key(), k.FiledByAgents())
	}
	got := k.Summarize([]byte(`{"title":"Ship","period_start":"2026-10-01","period_end":"2026-12-31","key_results":[{"title":"a","metric_key":"m","start_value":0,"target_value":1}]}`))
	if got != "Goal: Ship (1 key results, 2026-10-01 to 2026-12-31)" {
		t.Fatalf("summary = %q", got)
	}
}

// A goal may be filed with no key results yet, like a draft.
func TestGoalFilingAcceptsAGoalWithNoKeyResults(t *testing.T) {
	raw := []byte(`{"title":"Ship onboarding","period_start":"2026-10-01","period_end":"2026-12-31","key_results":[]}`)
	if _, err := NewGoalKind(nil).Validate(context.Background(), Env{}, raw); err != nil {
		t.Fatalf("a goal with no key results was refused: %v", err)
	}
}
