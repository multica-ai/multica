package runner

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/multica-ai/multica/server/internal/cerebro/evals"
)

// fakeTarget echoes a scripted answer per task id and reports a fixed cost.
type fakeTarget struct {
	answers map[string]string
	cost    int64
	err     error
}

func (f fakeTarget) Run(_ context.Context, task evals.TaskCase) (string, int64, error) {
	if f.err != nil {
		return "", 0, f.err
	}
	return f.answers[task.ID], f.cost, nil
}

// fakeGrader passes when the produced answer equals the task's expected value.
type fakeGrader struct {
	cost int64
	err  error
}

func (f fakeGrader) Grade(_ context.Context, task evals.TaskCase, answer string) (bool, string, int64, error) {
	if f.err != nil {
		return false, "", 0, f.err
	}
	if answer == task.Expected {
		return true, "matched expected", f.cost, nil
	}
	return false, "did not match expected", f.cost, nil
}

func tasks() []evals.TaskCase {
	return []evals.TaskCase{
		{ID: "a", Situation: "s1", Expected: "yes", Critical: true},
		{ID: "b", Situation: "s2", Expected: "no", Critical: false},
	}
}

// fixedClock advances by a fixed step on each call so latency is deterministic.
func fixedClock(step time.Duration) func() time.Time {
	base := time.Unix(0, 0)
	n := 0
	return func() time.Time {
		t := base.Add(time.Duration(n) * step)
		n++
		return t
	}
}

func TestExecuteProducesTrustworthyPass(t *testing.T) {
	r := New(
		fakeTarget{answers: map[string]string{"a": "yes", "b": "no"}, cost: 3},
		fakeGrader{cost: 2},
		fixedClock(100*time.Millisecond),
	)
	report := r.Execute(context.Background(), tasks(), evals.ThresholdPolicy{MinPassRate: 1.0, RequireAllCritical: true})

	if report.Outcome.Status != evals.RunStatusPassed {
		t.Fatalf("expected passed, got %s", report.Outcome.Status)
	}
	if len(report.Cases) != 2 || !report.Cases[0].Passed || report.Cases[0].Produced != "yes" {
		t.Fatalf("unexpected case records: %+v", report.Cases)
	}
	// 2 tasks * (target 3 + grader 2) = 10 cents.
	if report.CostCents != 10 {
		t.Fatalf("expected cost 10, got %d", report.CostCents)
	}
	if report.LatencyMS <= 0 {
		t.Fatalf("expected positive latency, got %d", report.LatencyMS)
	}
}

func TestExecuteFailsRunWhenCriticalTaskWrong(t *testing.T) {
	r := New(
		// task "a" is critical and gets a wrong answer.
		fakeTarget{answers: map[string]string{"a": "WRONG", "b": "no"}},
		fakeGrader{},
		fixedClock(time.Millisecond),
	)
	report := r.Execute(context.Background(), tasks(), evals.ThresholdPolicy{MinPassRate: 0.5, RequireAllCritical: true})

	if report.Outcome.Status != evals.RunStatusFailed {
		t.Fatalf("expected failed on critical, got %s", report.Outcome.Status)
	}
	if report.Cases[0].Passed {
		t.Fatal("critical task should be recorded as failed")
	}
}

func TestExecuteTargetErrorFailsClosedWithoutAbort(t *testing.T) {
	r := New(
		fakeTarget{err: errors.New("boom")},
		fakeGrader{},
		fixedClock(time.Millisecond),
	)
	report := r.Execute(context.Background(), tasks(), evals.ThresholdPolicy{MinPassRate: 0.5})

	if len(report.Cases) != 2 {
		t.Fatalf("run must not abort on target error, got %d cases", len(report.Cases))
	}
	if report.Cases[0].Error == "" || report.Cases[0].Passed {
		t.Fatalf("errored task must be failed with error set: %+v", report.Cases[0])
	}
	if report.Outcome.Status != evals.RunStatusFailed {
		t.Fatalf("expected failed run, got %s", report.Outcome.Status)
	}
}

func TestExecuteGraderErrorRecordedPerCase(t *testing.T) {
	r := New(
		fakeTarget{answers: map[string]string{"a": "yes", "b": "no"}},
		fakeGrader{err: errors.New("judge unavailable")},
		fixedClock(time.Millisecond),
	)
	report := r.Execute(context.Background(), tasks(), evals.ThresholdPolicy{MinPassRate: 0.5})
	if report.Cases[0].Error == "" || report.Cases[0].Passed {
		t.Fatalf("grader error must fail the case: %+v", report.Cases[0])
	}
}

func TestResultsJSONRoundTrips(t *testing.T) {
	r := New(
		fakeTarget{answers: map[string]string{"a": "yes", "b": "no"}},
		fakeGrader{},
		fixedClock(time.Millisecond),
	)
	report := r.Execute(context.Background(), tasks(), evals.ThresholdPolicy{MinPassRate: 1.0})
	raw, err := report.ResultsJSON()
	if err != nil {
		t.Fatalf("ResultsJSON: %v", err)
	}
	if len(raw) == 0 {
		t.Fatal("expected non-empty results JSON")
	}
}
