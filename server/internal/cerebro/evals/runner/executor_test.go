package runner

import (
	"context"
	"encoding/json"
	"strings"
	"testing"

	"github.com/multica-ai/multica/server/internal/cerebro/evals"
)

// scriptedCompleter answers a prompt by substring match, so the prompt target
// produces a deterministic answer per task without a live gateway. Each call
// reports a fixed cost so the executor's cost aggregation is exercised.
type scriptedCompleter struct {
	answers map[string]string
	cost    int64
}

func (s scriptedCompleter) Complete(_ context.Context, req CompletionRequest) (CompletionResult, error) {
	for needle, answer := range s.answers {
		if strings.Contains(req.Prompt, needle) {
			return CompletionResult{Text: answer, CostCents: s.cost}, nil
		}
	}
	return CompletionResult{Text: "", CostCents: s.cost}, nil
}

func passingEval() evals.Eval {
	return evals.Eval{
		Version:    "v1",
		Target:     json.RawMessage(`{"kind":"prompt"}`),
		Datasets:   json.RawMessage(`[{"id":"a","situation":"what is 2+2","expected":"4","critical":true},{"id":"b","situation":"capital of Denmark","expected":"Copenhagen","critical":false}]`),
		Graders:    json.RawMessage(`[{"id":"g1","type":"hard_rule","config":{"match":"iexact"}}]`),
		Thresholds: json.RawMessage(`[{"metric":"pass_rate","operator":">=","value":0.5},{"metric":"all_critical_pass","operator":"=","value":true}]`),
	}
}

// TestExecuteRunRealResultThresholdMet drives a full in-app eval through the
// engine: the prompt target gets answers from a fake completer, the hard_rule
// grader scores them deterministically, and the run's verdict is computed — not
// trusted from outside. One of two tasks passes; the pass rate meets 0.5 and the
// only critical task passed, so the run is a real "passed".
func TestExecuteRunRealResultThresholdMet(t *testing.T) {
	completer := scriptedCompleter{
		answers: map[string]string{"2+2": "4", "capital of Denmark": "Paris"},
		cost:    3,
	}
	exec := NewEvalExecutor(completer, nil)

	input, err := exec.ExecuteRun(context.Background(), passingEval())
	if err != nil {
		t.Fatalf("ExecuteRun: %v", err)
	}
	if input.Status != evals.RunStatusPassed {
		t.Fatalf("status = %q, want %q", input.Status, evals.RunStatusPassed)
	}
	if input.TargetVersion != "v1" {
		t.Fatalf("target version = %q, want v1", input.TargetVersion)
	}
	// Two target calls at 3 cents each; the hard_rule grader is free.
	if input.CostCents != 6 {
		t.Fatalf("cost = %d cents, want 6", input.CostCents)
	}
	if input.StartedAt == nil || input.CompletedAt == nil {
		t.Fatalf("timestamps not set: started=%v completed=%v", input.StartedAt, input.CompletedAt)
	}
	if input.CompletedAt.Before(*input.StartedAt) {
		t.Fatalf("completed %v before started %v", input.CompletedAt, input.StartedAt)
	}

	var report Report
	if err := json.Unmarshal(input.Results, &report); err != nil {
		t.Fatalf("results not a runner.Report: %v", err)
	}
	if len(report.Cases) != 2 {
		t.Fatalf("cases = %d, want 2", len(report.Cases))
	}
	byID := map[string]CaseReport{}
	for _, c := range report.Cases {
		byID[c.CaseID] = c
	}
	if !byID["a"].Passed {
		t.Errorf("case a should pass: %+v", byID["a"])
	}
	if byID["b"].Passed {
		t.Errorf("case b should fail: %+v", byID["b"])
	}
	// "See why" reads exactly this: the produced answer and the reason.
	if byID["b"].Produced != "Paris" {
		t.Errorf("case b produced = %q, want Paris", byID["b"].Produced)
	}
	if byID["b"].Reason == "" {
		t.Errorf("case b missing a grader reason")
	}
}

// TestExecuteRunCriticalFailureFailsClosed shows a met pass rate cannot rescue a
// failed critical task: authenticity means a critical miss sinks the whole run.
func TestExecuteRunCriticalFailureFailsClosed(t *testing.T) {
	completer := scriptedCompleter{
		answers: map[string]string{"2+2": "5", "capital of Denmark": "Copenhagen"},
		cost:    0,
	}
	exec := NewEvalExecutor(completer, nil)

	input, err := exec.ExecuteRun(context.Background(), passingEval())
	if err != nil {
		t.Fatalf("ExecuteRun: %v", err)
	}
	if input.Status != evals.RunStatusFailed {
		t.Fatalf("status = %q, want %q (critical task failed)", input.Status, evals.RunStatusFailed)
	}
}

// TestExecuteRunUnwiredTargetErrors proves a target the engine cannot run is
// rejected with an error rather than recorded as a run: nothing is ever stored
// as a pass for a target that never executed.
func TestExecuteRunUnwiredTargetErrors(t *testing.T) {
	eval := passingEval()
	eval.Target = json.RawMessage(`{"kind":"agent","ref":"some-agent"}`)
	exec := NewEvalExecutor(scriptedCompleter{}, nil)

	if _, err := exec.ExecuteRun(context.Background(), eval); err == nil {
		t.Fatal("expected an error for a not-yet-wired target kind")
	}
}

// TestExecuteRunNoTasksErrors proves an eval with nothing to test cannot produce
// a run at all.
func TestExecuteRunNoTasksErrors(t *testing.T) {
	eval := passingEval()
	eval.Datasets = json.RawMessage(`[]`)
	exec := NewEvalExecutor(scriptedCompleter{}, nil)

	if _, err := exec.ExecuteRun(context.Background(), eval); err == nil {
		t.Fatal("expected an error when the eval has no tasks")
	}
}
