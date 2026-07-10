package workflows

// Unit test for how the engine routes an OpCheckPasses condition: it must
// defer the verdict to the injected GateEvaluator, and fail closed (never
// advance) when no evaluator is wired. No DB needed — the evaluator is a stub.

import (
	"context"
	"encoding/json"
	"errors"
	"testing"
)

// stubGate is a GateEvaluator that returns canned values and records the args
// it was called with.
type stubGate struct {
	advance     bool
	err         error
	calls       int
	gotIssueID  string
	gotGate     string
	gotValueLen int
}

func (g *stubGate) EvaluateCheckGate(_ context.Context, issueID, gate string, value any) (bool, error) {
	g.calls++
	g.gotIssueID = issueID
	g.gotGate = gate
	// Value arrives as the generic JSON shape (a map), exactly as it would from
	// a stored workflow row — re-marshal it into the config to read the checks,
	// the same parse the real loops adapter does.
	if raw, err := json.Marshal(value); err == nil {
		var cfg CheckGateValue
		if json.Unmarshal(raw, &cfg) == nil {
			g.gotValueLen = len(cfg.Checks)
		}
	}
	return g.advance, g.err
}

// CheckGateValue is a minimal local mirror of the config shape, only so the
// stub can read it back; the engine treats Condition.Value opaquely.
type CheckGateValue struct {
	Checks [][]string `json:"checks"`
}

func checkPassesWorkflow(t *testing.T) workflow {
	t.Helper()
	conds, err := json.Marshal([]Condition{{
		Field: "loop.checks",
		Op:    OpCheckPasses,
		Value: CheckGateValue{Checks: [][]string{{"go", "test", "./..."}}},
	}})
	if err != nil {
		t.Fatalf("marshal conditions: %v", err)
	}
	return workflow{conditions: conds}
}

func TestConditionsHold_CheckPasses_FailsClosedWithoutEvaluator(t *testing.T) {
	s := &Service{} // no gateEval wired
	if s.conditionsHold(context.Background(), checkPassesWorkflow(t), TriggerEvent{IssueID: "00000000-0000-0000-0000-000000000001"}) {
		t.Fatal("check_passes must fail closed when no evaluator is wired")
	}
}

func TestConditionsHold_CheckPasses_DefersToEvaluator(t *testing.T) {
	gate := &stubGate{advance: true}
	s := (&Service{}).WithGateEvaluator(gate)
	te := TriggerEvent{IssueID: "00000000-0000-0000-0000-000000000002"}

	if !s.conditionsHold(context.Background(), checkPassesWorkflow(t), te) {
		t.Fatal("conditionsHold should hold when the evaluator advances")
	}
	if gate.calls != 1 {
		t.Fatalf("evaluator should be called once, got %d", gate.calls)
	}
	if gate.gotIssueID != te.IssueID {
		t.Fatalf("evaluator got issue id %q, want %q", gate.gotIssueID, te.IssueID)
	}
	if gate.gotValueLen != 1 {
		t.Fatalf("evaluator got %d checks in the config value, want 1", gate.gotValueLen)
	}

	gate.advance = false
	if s.conditionsHold(context.Background(), checkPassesWorkflow(t), te) {
		t.Fatal("conditionsHold should not hold when the evaluator declines to advance")
	}
}

func TestConditionsHold_CheckPasses_FailsClosedOnError(t *testing.T) {
	gate := &stubGate{advance: true, err: errors.New("boom")}
	s := (&Service{}).WithGateEvaluator(gate)
	if s.conditionsHold(context.Background(), checkPassesWorkflow(t), TriggerEvent{IssueID: "00000000-0000-0000-0000-000000000003"}) {
		t.Fatal("check_passes must fail closed when the evaluator errors")
	}
}

// scopedCheckPassesWorkflow builds a delivery-gate rule the way the issue-loop
// compiler does: an `issue.id eq` scope filter FOLLOWED by the check_passes
// gate condition.
func scopedCheckPassesWorkflow(t *testing.T, issueID string) workflow {
	t.Helper()
	conds, err := json.Marshal([]Condition{
		{Field: "issue.id", Op: "eq", Value: issueID},
		{
			Field: "loop.checks",
			Op:    OpCheckPasses,
			Value: CheckGateValue{Checks: [][]string{{"make", "build"}}},
		},
	})
	if err != nil {
		t.Fatalf("marshal conditions: %v", err)
	}
	return workflow{conditions: conds}
}

// Regression (FIR-2283): EvaluateCheckGate is side-effecting — it materializes
// gate state, check runs, and human approvals for the (issue, gate) pair. The
// pure `issue.id eq` scope filter must therefore be decided BEFORE the gate
// evaluator runs; otherwise every issue-scoped delivery gate in the workspace
// opens a gate round (duplicate approvals + check tasks) on ANY issue entering
// the trigger status.
func TestConditionsHold_CheckPasses_SkippedWhenIssueScopeDoesNotMatch(t *testing.T) {
	gate := &stubGate{advance: true}
	s := (&Service{}).WithGateEvaluator(gate)

	scoped := "00000000-0000-0000-0000-00000000000a"
	other := "00000000-0000-0000-0000-00000000000b"
	te := TriggerEvent{
		IssueID: other,
		Raw:     map[string]any{"issue": map[string]any{"id": other}},
	}

	if s.conditionsHold(context.Background(), scopedCheckPassesWorkflow(t, scoped), te) {
		t.Fatal("conditions must not hold for an issue outside the rule's scope")
	}
	if gate.calls != 0 {
		t.Fatalf("gate evaluator must NOT be invoked for an out-of-scope issue (side effects!), got %d calls", gate.calls)
	}
}

func TestConditionsHold_CheckPasses_RunsWhenIssueScopeMatches(t *testing.T) {
	gate := &stubGate{advance: true}
	s := (&Service{}).WithGateEvaluator(gate)

	scoped := "00000000-0000-0000-0000-00000000000a"
	te := TriggerEvent{
		IssueID: scoped,
		Raw:     map[string]any{"issue": map[string]any{"id": scoped}},
	}

	if !s.conditionsHold(context.Background(), scopedCheckPassesWorkflow(t, scoped), te) {
		t.Fatal("conditions should hold for the scoped issue when the evaluator advances")
	}
	if gate.calls != 1 {
		t.Fatalf("gate evaluator should be called once for the scoped issue, got %d", gate.calls)
	}
}
