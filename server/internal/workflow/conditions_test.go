package workflow

import "testing"

func TestConditionSelectsFirstMatchingBranchAndKeepsDefaultFallback(t *testing.T) {
	node := Node{ID: "decision", Type: "condition", Config: map[string]any{
		"branches": []any{
			map[string]any{"id": "review", "rule": map[string]any{"operator": "and", "clauses": []any{map[string]any{
				"left": map[string]any{"kind": "input", "field": "needs_review"}, "comparison": "equals", "right": map[string]any{"kind": "literal", "value": true},
			}}}},
			map[string]any{"id": "default", "default": true},
		},
	}}
	edges := []Edge{{ID: "review", Source: "decision", Target: "human", SourcePort: "review"}, {ID: "default", Source: "decision", Target: "end", SourcePort: "default"}}
	chosen, err := chooseConditionEdgeForRun(node, edges, Run{Input: `{"needs_review":true}`}, map[string]*NodeRun{})
	if err != nil || chosen.Target != "human" {
		t.Fatalf("chosen = %#v, err = %v", chosen, err)
	}
	chosen, err = chooseConditionEdgeForRun(node, edges, Run{Input: `{"needs_review":false}`}, map[string]*NodeRun{})
	if err != nil || chosen.Target != "end" {
		t.Fatalf("default chosen = %#v, err = %v", chosen, err)
	}
}

func TestConditionMissingInputDoesNotSilentlyUseDefault(t *testing.T) {
	node := Node{ID: "decision", Type: "condition", Config: map[string]any{
		"branches": []any{
			map[string]any{"id": "review", "rule": map[string]any{"operator": "and", "clauses": []any{map[string]any{
				"left": map[string]any{"kind": "input", "field": "missing"}, "comparison": "equals", "right": map[string]any{"kind": "literal", "value": true},
			}}}},
			map[string]any{"id": "default", "default": true},
		},
	}}
	_, err := chooseConditionEdgeForRun(node, []Edge{{ID: "default", Source: "decision", Target: "end", SourcePort: "default"}}, Run{Input: `{}`}, map[string]*NodeRun{})
	if err == nil {
		t.Fatal("missing condition input should be an error")
	}
}

func TestReworkLimitIsEnforced(t *testing.T) {
	run := Run{Graph: Graph{Scopes: []Scope{{ID: "scope", Type: "rework", ExitNodeID: "review", MaxReworks: 1}}, Edges: []Edge{{Source: "draft", Target: "review"}}}}
	if err := applySimpleRework(&run, "review", "first"); err != nil {
		t.Fatalf("first rework: %v", err)
	}
	if err := applySimpleRework(&run, "review", "second"); err == nil {
		t.Fatal("second rework should be rejected")
	}
}

func TestReworkCreatesNewGenerationAndActivation(t *testing.T) {
	run := Run{
		Graph: Graph{
			Nodes:  []Node{{ID: "draft", Type: "agent"}, {ID: "review", Type: "human_review"}},
			Edges:  []Edge{{Source: "draft", Target: "review"}},
			Scopes: []Scope{{ID: "scope", Type: "rework", EntryNodeID: "draft", ExitNodeID: "review", MaxReworks: 2}},
		},
		Nodes: []NodeRun{
			{NodeID: "draft", Status: "succeeded", Generation: 1, ActivationID: "old-draft", TaskID: "old-task"},
			{NodeID: "review", Status: "waiting_human", Generation: 1, ActivationID: "old-review", WorkItemID: "old-item"},
		},
	}
	if err := applySimpleRework(&run, "review", "add evidence"); err != nil {
		t.Fatalf("rework failed: %v", err)
	}
	for _, node := range run.Nodes {
		if node.Status != "pending" || node.Generation != 2 || node.ActivationID == "" || node.ActivationID == "old-draft" || node.ActivationID == "old-review" {
			t.Fatalf("node was not renewed: %#v", node)
		}
	}
}
