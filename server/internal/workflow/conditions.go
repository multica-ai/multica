package workflow

import (
	"encoding/json"
	"fmt"
	"reflect"
	"strconv"
	"strings"
)

func chooseConditionEdgeForRun(node Node, edges []Edge, run Run, states map[string]*NodeRun) (Edge, error) {
	if node.Config == nil {
		return chooseConditionEdge(node, edges), nil
	}
	if port, ok := node.Config["selected_port"].(string); ok && port != "" {
		for _, edge := range edges {
			if normalFlowEdge(edge) && edge.Source == node.ID && edge.SourcePort == port {
				return edge, nil
			}
		}
		return Edge{}, &Error{Status: 422, Message: "selected condition output does not exist", Code: "condition_output_missing"}
	}
	branches := conditionObjects(node.Config["branches"])
	if len(branches) == 0 {
		return chooseConditionEdge(node, edges), nil
	}
	var defaultID string
	if value, ok := node.Config["default_branch_id"].(string); ok {
		defaultID = value
	}
	for _, branch := range branches {
		id, _ := branch["id"].(string)
		if value, ok := branch["default"].(bool); ok && value {
			defaultID = id
			continue
		}
		rule, _ := branch["rule"].(map[string]any)
		matched, missing := evaluateRule(rule, run, states)
		if missing {
			return Edge{}, &Error{Status: 422, Message: fmt.Sprintf("condition input for branch %s is missing", id), Code: "condition_input_missing", Details: []ValidationDetail{{Code: "condition_input_missing", NodeID: node.ID, Field: "config.branches", Message: "condition input is required"}}}
		}
		if matched {
			return findConditionEdge(node.ID, id, branch, edges)
		}
	}
	if defaultID != "" {
		return findConditionEdge(node.ID, defaultID, map[string]any{"id": defaultID}, edges)
	}
	return chooseConditionEdge(node, edges), nil
}

func findConditionEdge(nodeID, branchID string, branch map[string]any, edges []Edge) (Edge, error) {
	port, _ := branch["port"].(string)
	if port == "" {
		port = branchID
	}
	for _, edge := range edges {
		if normalFlowEdge(edge) && edge.Source == nodeID && (edge.SourcePort == port || edge.SourcePort == branchID) {
			return edge, nil
		}
	}
	return Edge{}, &Error{Status: 422, Message: fmt.Sprintf("condition branch %s is not connected", branchID), Code: "condition_branch_unconnected"}
}

func conditionObjects(raw any) []map[string]any {
	objects := make([]map[string]any, 0)
	switch values := raw.(type) {
	case []map[string]any:
		return values
	case []any:
		for _, value := range values {
			if object, ok := value.(map[string]any); ok {
				objects = append(objects, object)
			}
		}
	default:
		return objects
	}
	return objects
}

func evaluateRule(rule map[string]any, run Run, states map[string]*NodeRun) (matched, missing bool) {
	if rule == nil {
		return false, false
	}
	operator, _ := rule["operator"].(string)
	if operator == "" {
		operator = "and"
	}
	clauses := conditionObjects(rule["clauses"])
	if len(clauses) == 0 {
		return false, false
	}
	if operator == "or" {
		matched = false
	} else {
		matched = true
	}
	for _, clause := range clauses {
		left, leftPresent := conditionOperand(clause["left"], run, states)
		right, rightPresent := conditionOperand(clause["right"], run, states)
		if !leftPresent || !rightPresent {
			missing = true
			continue
		}
		comparison, _ := clause["comparison"].(string)
		value := compareCondition(left, right, comparison)
		if operator == "or" {
			matched = matched || value
		} else {
			matched = matched && value
		}
	}
	return matched, missing
}

func conditionOperand(raw any, run Run, states map[string]*NodeRun) (any, bool) {
	object, ok := raw.(map[string]any)
	if !ok {
		return raw, raw != nil
	}
	kind, _ := object["kind"].(string)
	switch kind {
	case "literal":
		return object["value"], true
	case "input":
		var values map[string]any
		if json.Unmarshal([]byte(run.Input), &values) != nil {
			return nil, false
		}
		field, _ := object["field"].(string)
		return lookupPath(values, field)
	case "output":
		nodeID, _ := object["node_id"].(string)
		state := states[nodeID]
		if state == nil {
			return nil, false
		}
		var values map[string]any
		if json.Unmarshal([]byte(state.Output), &values) == nil {
			field, _ := object["field"].(string)
			return lookupPath(values, field)
		}
		field, _ := object["field"].(string)
		if field == "" || field == "text" {
			return state.Output, state.Output != ""
		}
		return nil, false
	default:
		return nil, false
	}
}

func lookupPath(values map[string]any, path string) (any, bool) {
	if path == "" {
		return values, true
	}
	var current any = values
	for _, part := range strings.Split(path, ".") {
		object, ok := current.(map[string]any)
		if !ok {
			return nil, false
		}
		current, ok = object[part]
		if !ok {
			return nil, false
		}
	}
	return current, true
}

func compareCondition(left, right any, comparison string) bool {
	switch comparison {
	case "equals", "equal", "==":
		return reflect.DeepEqual(left, right) || fmt.Sprint(left) == fmt.Sprint(right)
	case "not_equals", "not_equal", "!=":
		return !compareCondition(left, right, "equals")
	case "contains":
		return strings.Contains(fmt.Sprint(left), fmt.Sprint(right))
	case "greater_than", ">":
		return numericCondition(left) > numericCondition(right)
	case "greater_or_equal", ">=":
		return numericCondition(left) >= numericCondition(right)
	case "less_than", "<":
		return numericCondition(left) < numericCondition(right)
	case "less_or_equal", "<=":
		return numericCondition(left) <= numericCondition(right)
	case "is_empty":
		return left == nil || strings.TrimSpace(fmt.Sprint(left)) == ""
	default:
		return false
	}
}

func numericCondition(value any) float64 {
	switch number := value.(type) {
	case float64:
		return number
	case float32:
		return float64(number)
	case int:
		return float64(number)
	case int64:
		return float64(number)
	case json.Number:
		parsed, _ := number.Float64()
		return parsed
	default:
		parsed, _ := strconv.ParseFloat(fmt.Sprint(value), 64)
		return parsed
	}
}
