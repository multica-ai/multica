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

func TestHasAcceptanceCriteria(t *testing.T) {
	cases := []struct {
		name string
		desc string
		want bool
	}{
		{"empty", "", false},
		{"prose only", "Build the login page and ship it.", false},
		{"unchecked item", "Done means:\n- [ ] login works", true},
		{"checked item", "- [x] tests green", true},
		{"checked upper", "- [X] deployed", true},
		{"asterisk bullet", "* [ ] api returns 200", true},
		{"plus bullet", "+ [ ] migration applied", true},
		{"indented item", "Criteria:\n    - [ ] nested but valid", true},
		{"bullet without checkbox is not criteria", "- just a bullet point", false},
		{"empty checkbox line too short", "- [ ]", true},
	}
	for _, tc := range cases {
		if got := HasAcceptanceCriteria(tc.desc); got != tc.want {
			t.Errorf("%s: HasAcceptanceCriteria(%q) = %v, want %v", tc.name, tc.desc, got, tc.want)
		}
	}
}

func TestBlockerSatisfied_VerificationGate(t *testing.T) {
	cases := []struct {
		name string
		b    BlockerState
		want bool
	}{
		{"running never satisfies", BlockerState{Status: "in_progress"}, false},
		{"in_review never satisfies", BlockerState{Status: "in_review"}, false},
		{"cancelled always satisfies", BlockerState{Status: "cancelled"}, true},
		{"cancelled satisfies even when gated", BlockerState{Status: "cancelled", RequiresVerification: true}, true},
		{"plain done satisfies (non-child)", BlockerState{Status: "done"}, true},
		{"gated done unverified holds", BlockerState{Status: "done", RequiresVerification: true, Verified: false}, false},
		{"gated done verified satisfies", BlockerState{Status: "done", RequiresVerification: true, Verified: true}, true},
		{"verified flag ignored when not gated", BlockerState{Status: "done", RequiresVerification: false, Verified: false}, true},
	}
	for _, tc := range cases {
		if got := BlockerSatisfied(tc.b); got != tc.want {
			t.Errorf("%s: BlockerSatisfied(%+v) = %v, want %v", tc.name, tc.b, got, tc.want)
		}
	}
}

func TestReadyToStart_VerificationGateHoldsDependents(t *testing.T) {
	children := []ChildState{
		{ID: "a", Number: 1, Status: "done"},
		{ID: "b", Number: 2, Status: "backlog"},
	}
	// b waits on a; a is an orchestrated child so it must be verified, not
	// just done, before b is released.
	blockers := map[string][]BlockerState{
		"b": {{ID: "a", Status: "done", RequiresVerification: true, Verified: false}},
	}
	if got := ReadyToStart(children, blockers); len(got) != 0 {
		t.Fatalf("expected b held while blocker a is done-but-unverified, got %v", ids(got))
	}
	// a verified -> b released.
	blockers["b"][0].Verified = true
	got := ids(ReadyToStart(children, blockers))
	if !reflect.DeepEqual(got, []string{"b"}) {
		t.Fatalf("expected b ready after blocker a verified, got %v", got)
	}
}

func TestReadyToStart_CancelledGatedBlockerNeedsNoVerification(t *testing.T) {
	children := []ChildState{{ID: "b", Number: 2, Status: "backlog"}}
	blockers := map[string][]BlockerState{
		"b": {{ID: "a", Status: "cancelled", RequiresVerification: true, Verified: false}},
	}
	got := ids(ReadyToStart(children, blockers))
	if !reflect.DeepEqual(got, []string{"b"}) {
		t.Fatalf("cancelled blocker should release b without verification, got %v", got)
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
