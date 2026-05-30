package orchestration

import (
	"reflect"
	"testing"
)

func ids(children []ChildState) []string {
	out := make([]string, len(children))
	for i, c := range children {
		out[i] = c.ID
	}
	return out
}

func TestReadyToStart_NoBlockers(t *testing.T) {
	children := []ChildState{
		{ID: "a", Number: 1, Status: "backlog"},
		{ID: "b", Number: 2, Status: "todo"},
	}
	got := ids(ReadyToStart(children, nil))
	want := []string{"a", "b"}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("ready = %v, want %v", got, want)
	}
}

func TestReadyToStart_BlockedUntilTerminal(t *testing.T) {
	children := []ChildState{
		{ID: "a", Number: 1, Status: "in_progress"},
		{ID: "b", Number: 2, Status: "backlog"},
	}
	blockers := map[string][]BlockerState{
		"b": {{ID: "a", Status: "in_progress"}},
	}
	// a still running -> b not ready.
	if got := ReadyToStart(children, blockers); len(got) != 0 {
		t.Fatalf("expected nothing ready while blocker runs, got %v", ids(got))
	}
	// a done -> b ready.
	children[0].Status = "done"
	blockers["b"][0].Status = "done"
	got := ids(ReadyToStart(children, blockers))
	if !reflect.DeepEqual(got, []string{"b"}) {
		t.Fatalf("expected b ready after blocker done, got %v", got)
	}
}

func TestReadyToStart_SkipsStartedAndTerminal(t *testing.T) {
	children := []ChildState{
		{ID: "a", Number: 1, Status: "in_review"},
		{ID: "b", Number: 2, Status: "done"},
		{ID: "c", Number: 3, Status: "cancelled"},
		{ID: "d", Number: 4, Status: "todo"},
	}
	got := ids(ReadyToStart(children, nil))
	if !reflect.DeepEqual(got, []string{"d"}) {
		t.Fatalf("expected only d ready, got %v", got)
	}
}

func TestReadyToStart_CrossTreeBlockerHonored(t *testing.T) {
	children := []ChildState{{ID: "b", Number: 2, Status: "backlog"}}
	// "x" is not a child of this parent but still blocks b.
	blockers := map[string][]BlockerState{"b": {{ID: "x", Status: "in_progress"}}}
	if got := ReadyToStart(children, blockers); len(got) != 0 {
		t.Fatalf("expected b held by external blocker, got %v", ids(got))
	}
}

func TestReadyToStart_OrderedByNumber(t *testing.T) {
	children := []ChildState{
		{ID: "z", Number: 9, Status: "todo"},
		{ID: "a", Number: 3, Status: "backlog"},
		{ID: "m", Number: 5, Status: "todo"},
	}
	got := ids(ReadyToStart(children, nil))
	if !reflect.DeepEqual(got, []string{"a", "m", "z"}) {
		t.Fatalf("expected number order, got %v", got)
	}
}

func TestPlanFromChildren_KeepsOnlyInTreeDeps(t *testing.T) {
	children := []ChildState{
		{ID: "a", Number: 1, Status: "todo"},
		{ID: "b", Number: 2, Status: "backlog"},
	}
	blockers := map[string][]BlockerState{
		"b": {{ID: "a", Status: "todo"}, {ID: "x", Status: "todo"}},
	}
	plan := PlanFromChildren("parent", children, blockers)
	if len(plan.Nodes) != 2 {
		t.Fatalf("expected 2 nodes, got %d", len(plan.Nodes))
	}
	var bNode Node
	for _, n := range plan.Nodes {
		if n.ID == "b" {
			bNode = n
		}
	}
	if !reflect.DeepEqual(bNode.DependsOn, []string{"a"}) {
		t.Fatalf("expected b depends on [a] (x dropped), got %v", bNode.DependsOn)
	}
	// The derived plan must be valid for the engine.
	if issues := ValidatePlan(plan); len(issues) != 0 {
		t.Fatalf("derived plan should validate, got %v", issues)
	}
}

func TestRenderWaves_Numbers(t *testing.T) {
	children := []ChildState{
		{ID: "a", Number: 11, Status: "todo"},
		{ID: "b", Number: 12, Status: "backlog"},
		{ID: "c", Number: 13, Status: "backlog"},
	}
	blockers := map[string][]BlockerState{
		"b": {{ID: "a", Status: "todo"}},
		"c": {{ID: "a", Status: "todo"}},
	}
	plan := PlanFromChildren("p", children, blockers)
	got := RenderWaves(children, plan)
	want := []string{"Wave 1: #11", "Wave 2: #12, #13"}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("waves = %v, want %v", got, want)
	}
}

func TestPlanFromChildren_CycleDetected(t *testing.T) {
	children := []ChildState{
		{ID: "a", Number: 1, Status: "backlog"},
		{ID: "b", Number: 2, Status: "backlog"},
	}
	blockers := map[string][]BlockerState{
		"a": {{ID: "b", Status: "todo"}},
		"b": {{ID: "a", Status: "todo"}},
	}
	plan := PlanFromChildren("p", children, blockers)
	if !DetectCycle(plan.Nodes) {
		t.Fatal("expected cycle in mutually-blocking children")
	}
}
