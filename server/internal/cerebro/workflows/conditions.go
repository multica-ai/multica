package workflows

import (
	"encoding/json"
	"fmt"
)

// Condition is a single conjunctive filter applied after the trigger fires.
// The conditions column in cerebro_workflow stores an array of these — all
// must evaluate true for the action to execute. An empty array means
// "always".
//
//	{ "field": "issue.priority", "op": "in", "values": ["urgent","high"] }
//	{ "field": "issue.project_id", "op": "eq", "value": "<uuid>" }
//	{ "field": "issue.assignee_id", "op": "is_not_null" }
type Condition struct {
	Field  string `json:"field"`
	Op     string `json:"op"`
	Value  any    `json:"value,omitempty"`
	Values []any  `json:"values,omitempty"`
}

func parseConditions(raw []byte) ([]Condition, error) {
	if len(raw) == 0 {
		return nil, nil
	}
	var out []Condition
	if err := json.Unmarshal(raw, &out); err != nil {
		return nil, fmt.Errorf("conditions: %w", err)
	}
	return out, nil
}

// evaluate returns true when every condition matches the given context.
// Unknown fields and unknown operators fail closed (return false) — better
// to skip a workflow than fire one with an unexpected interpretation.
func evaluate(conds []Condition, ctx map[string]any) bool {
	for _, c := range conds {
		if !match(c, ctx) {
			return false
		}
	}
	return true
}

func match(c Condition, ctx map[string]any) bool {
	got, ok := lookup(c.Field, ctx)
	switch c.Op {
	case "eq":
		return ok && fmt.Sprint(got) == fmt.Sprint(c.Value)
	case "ne":
		return ok && fmt.Sprint(got) != fmt.Sprint(c.Value)
	case "in":
		if !ok {
			return false
		}
		for _, v := range c.Values {
			if fmt.Sprint(got) == fmt.Sprint(v) {
				return true
			}
		}
		return false
	case "is_null":
		return !ok || got == nil
	case "is_not_null":
		return ok && got != nil
	default:
		return false
	}
}

// lookup walks dotted field paths like "issue.priority" against the supplied
// context map. Missing keys return (nil, false) so callers can distinguish
// "field absent" from "field is JSON null".
func lookup(path string, ctx map[string]any) (any, bool) {
	if path == "" {
		return nil, false
	}
	cur := any(ctx)
	for _, seg := range splitDot(path) {
		m, ok := cur.(map[string]any)
		if !ok {
			return nil, false
		}
		v, exists := m[seg]
		if !exists {
			return nil, false
		}
		cur = v
	}
	return cur, true
}

func splitDot(s string) []string {
	out := make([]string, 0, 2)
	start := 0
	for i := 0; i < len(s); i++ {
		if s[i] == '.' {
			out = append(out, s[start:i])
			start = i + 1
		}
	}
	out = append(out, s[start:])
	return out
}
