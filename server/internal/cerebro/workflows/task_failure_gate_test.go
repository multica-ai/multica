package workflows

import (
	"context"
	"testing"

	"github.com/jackc/pgx/v5/pgtype"
	"github.com/multica-ai/multica/server/internal/service"
	db "github.com/multica-ai/multica/server/pkg/db/generated"
)

type fakeTaskFailureStore struct {
	enabled bool
	context TaskCompletionContext
}

func (s fakeTaskFailureStore) WorkflowHooksEnabledForTask(context.Context, pgtype.UUID) (bool, error) {
	return s.enabled, nil
}

func (s fakeTaskFailureStore) LoadTaskFailureContext(context.Context, pgtype.UUID) (TaskCompletionContext, error) {
	return s.context, nil
}

type recordingHookEvaluator struct {
	event  HookEvent
	result HookResult
}

func (e *recordingHookEvaluator) Evaluate(_ context.Context, event HookEvent) (HookResult, error) {
	e.event = event
	return e.result, nil
}

func TestTaskFailureGateReturnsWorkflowDecision(t *testing.T) {
	evaluator := &recordingHookEvaluator{result: HookResult{
		Evaluated: true,
		Decision:  HookModify,
		Modifications: map[string]any{
			"failure_action": "retry",
			"fresh_session":  true,
			"retry_limit":    float64(1),
			"user_message":   "Retrying on a fresh session.",
		},
	}}
	gate := NewTaskFailureGate(fakeTaskFailureStore{
		enabled: true,
		context: TaskCompletionContext{
			TaskID: "task-1", WorkspaceID: "workspace-1", ProjectID: "project-1",
			WorkflowID: "workflow-1", AgentID: "agent-1", IssueID: "issue-1",
			SessionID: "session-1", Model: "model-1",
		},
	}, evaluator, true)

	decision, err := gate.EvaluateTaskFailure(context.Background(), db.AgentTaskQueue{
		Attempt: 2, MaxAttempts: 3, Status: "failed",
	}, "runtime_offline", "runtime disconnected")
	if err != nil {
		t.Fatal(err)
	}
	if decision.Action != service.TaskFailureRetry || !decision.FreshSession || decision.RetryLimit != 1 || !decision.Evaluated {
		t.Fatalf("decision = %+v", decision)
	}
	if evaluator.event.Type != HookOnTaskFailure || evaluator.event.WorkspaceID != "workspace-1" {
		t.Fatalf("event = %+v", evaluator.event)
	}
	failure := evaluator.event.Context["failure"].(map[string]any)
	if failure["reason"] != "runtime_offline" || failure["message"] != "runtime disconnected" {
		t.Fatalf("failure context = %+v", failure)
	}
}

func TestTaskFailureGateSkipsWhenWorkspaceFeatureIsOff(t *testing.T) {
	evaluator := &recordingHookEvaluator{}
	gate := NewTaskFailureGate(fakeTaskFailureStore{enabled: false}, evaluator, true)

	decision, err := gate.EvaluateTaskFailure(context.Background(), db.AgentTaskQueue{}, "timeout", "timed out")
	if err != nil {
		t.Fatal(err)
	}
	if decision.Evaluated || evaluator.event.Type != "" {
		t.Fatalf("unexpected evaluation: decision=%+v event=%+v", decision, evaluator.event)
	}
}
