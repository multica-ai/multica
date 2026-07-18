package runner

import (
	"context"
	"fmt"
	"time"

	"github.com/multica-ai/multica/server/internal/cerebro/evals"
)

// executor.go bridges the merged runner engine (FIR-3496 Fase 2) to the live
// eval-run store: given an eval authored in the app, it builds the target and
// grader from the eval's own specs, executes every task through the engine, and
// returns the run to persist. It lives in the runner package because it depends
// on both the engine and the typed eval assets; the eval HTTP handler depends
// only on the evals.RunExecutor interface, so wiring stays behind the
// default-OFF "Run now" flag (a nil executor keeps the existing live path
// unchanged).

// EvalExecutor runs an in-app eval for real against the model gateway.
type EvalExecutor struct {
	completer Completer
	now       func() time.Time
}

// NewEvalExecutor builds the executor. completer is the live GatewayCompleter
// (or a fake in tests); now may be nil, in which case time.Now is used.
func NewEvalExecutor(completer Completer, now func() time.Time) *EvalExecutor {
	return &EvalExecutor{completer: completer, now: now}
}

// ExecuteRun builds the target and grader from the eval's specs, runs the
// engine, and returns the EvalRunInput to store. Any setup error — no target,
// no tasks, no usable grader, or a target kind not yet wired — is returned
// rather than persisted, so a run that could not really execute is never
// recorded as a pass. Only the first grader is used: the engine scores each
// task with a single grader, matching runner.New's contract.
func (e *EvalExecutor) ExecuteRun(ctx context.Context, eval evals.Eval) (evals.EvalRunInput, error) {
	spec, err := evals.ParseTarget(eval.Target)
	if err != nil {
		return evals.EvalRunInput{}, err
	}
	tasks, err := evals.ParseTasks(eval.Datasets)
	if err != nil {
		return evals.EvalRunInput{}, err
	}
	graders, err := evals.ParseGraders(eval.Graders)
	if err != nil {
		return evals.EvalRunInput{}, err
	}
	policy := evals.ParseThresholdPolicy(eval.Thresholds)

	target, err := NewTarget(e.completer, spec)
	if err != nil {
		return evals.EvalRunInput{}, err
	}
	grader, err := NewGrader(e.completer, graders[0])
	if err != nil {
		return evals.EvalRunInput{}, err
	}

	nowFn := e.now
	if nowFn == nil {
		nowFn = time.Now
	}
	startedAt := nowFn()
	report := New(target, grader, e.now).Execute(ctx, tasks, policy)
	completedAt := startedAt.Add(time.Duration(report.LatencyMS) * time.Millisecond)

	results, err := report.ResultsJSON()
	if err != nil {
		return evals.EvalRunInput{}, fmt.Errorf("serialize run report: %w", err)
	}

	return evals.EvalRunInput{
		TargetVersion: eval.Version,
		Status:        report.Outcome.Status,
		Results:       results,
		CostCents:     report.CostCents,
		LatencyMS:     report.LatencyMS,
		StartedAt:     &startedAt,
		CompletedAt:   &completedAt,
	}, nil
}
