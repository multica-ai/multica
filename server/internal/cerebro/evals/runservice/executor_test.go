package runservice

import (
	"context"
	"encoding/json"
	"fmt"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/multica-ai/multica/server/internal/cerebro/evals"
	evalrunner "github.com/multica-ai/multica/server/internal/cerebro/evals/runner"
)

type scriptedCompleter struct {
	replies []string
	calls   []evalrunner.CompletionRequest
}

func (s *scriptedCompleter) Complete(_ context.Context, request evalrunner.CompletionRequest) (evalrunner.CompletionResult, error) {
	s.calls = append(s.calls, request)
	if len(s.calls) > len(s.replies) {
		return evalrunner.CompletionResult{}, fmt.Errorf("unexpected completion call %d", len(s.calls))
	}
	reply := s.replies[len(s.calls)-1]
	return evalrunner.CompletionResult{Text: reply}, nil
}

func TestExecutorRunsStoredAssetsAndThresholds(t *testing.T) {
	completer := &scriptedCompleter{replies: []string{"yes", "wrong"}}
	var resolvedModel string
	executor := NewWithResolver(func(_ context.Context, _ uuid.UUID, model string) (evalrunner.Completer, error) {
		resolvedModel = model
		return completer, nil
	}, func() time.Time { return time.Unix(100, 0) })

	execution, err := executor.Execute(context.Background(), evals.Eval{
		WorkspaceID: uuid.New(),
		Version:     "2.3.4",
		Target:      json.RawMessage(`{"kind":"prompt","ref":"prompt-v7"}`),
		Datasets: json.RawMessage(`[
			{"id":"a","situation":"first","expected":"yes","critical":true},
			{"id":"b","situation":"second","expected":"no"}
		]`),
		Graders:    json.RawMessage(`[{"id":"g","type":"hard_rule","config":{"match":"exact"}}]`),
		Thresholds: json.RawMessage(`[{"metric":"pass_rate","operator":"gte","value":0.75}]`),
		Runner:     json.RawMessage(`{"model":"runner-model"}`),
	})
	if err != nil {
		t.Fatalf("execute: %v", err)
	}
	if resolvedModel != "runner-model" {
		t.Fatalf("stored runner model was not resolved: %q", resolvedModel)
	}
	if execution.Status != evals.RunStatusFailed || execution.TargetVersion != "prompt-v7" {
		t.Fatalf("execution verdict/version = %q/%q", execution.Status, execution.TargetVersion)
	}
	var report evalrunner.Report
	if err := json.Unmarshal(execution.Results, &report); err != nil {
		t.Fatalf("decode report: %v", err)
	}
	if len(report.Cases) != 2 || report.Cases[0].Produced != "yes" || report.Cases[1].Produced != "wrong" {
		t.Fatalf("server runner did not produce the case reports: %+v", report.Cases)
	}
	if report.Outcome.PassRate != 0.5 || report.Outcome.Status != evals.RunStatusFailed {
		t.Fatalf("stored thresholds were not applied: %+v", report.Outcome)
	}
	if len(completer.calls) != 2 {
		t.Fatalf("target calls=%d, want 2", len(completer.calls))
	}
}
