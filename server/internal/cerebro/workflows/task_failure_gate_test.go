package workflows

import (
	"context"
	"errors"
	"testing"

	"github.com/jackc/pgx/v5/pgtype"

	"github.com/multica-ai/multica/server/internal/events"
	"github.com/multica-ai/multica/server/internal/util"
	"github.com/multica-ai/multica/server/pkg/protocol"
)

type fakeTaskFailureStore struct {
	failure TaskFailureContext
	err     error
	loaded  int
}

func (s *fakeTaskFailureStore) LoadTaskFailureContext(_ context.Context, _ pgtype.UUID) (TaskFailureContext, error) {
	s.loaded++
	return s.failure, s.err
}

type recordingEvaluator struct {
	events []HookEvent
}

func (e *recordingEvaluator) Evaluate(_ context.Context, event HookEvent) (HookResult, error) {
	e.events = append(e.events, event)
	return HookResult{Decision: HookAllow}, nil
}

const testFailureTaskID = "5f9d0a34-8a11-4a5a-9d5f-1c2b3a4d5e6f"

func failedTaskEvent(taskID string) events.Event {
	return events.Event{
		Type:        protocol.EventTaskFailed,
		WorkspaceID: "ws-1",
		ActorType:   "system",
		Payload: map[string]any{
			"task_id":        taskID,
			"agent_id":       "agent-1",
			"issue_id":       "issue-1",
			"status":         "failed",
			"failure_reason": "timeout",
		},
	}
}

func newTestFailureContext() TaskFailureContext {
	return TaskFailureContext{
		HooksEnabled: true, TaskID: testFailureTaskID,
		WorkspaceID: "ws-1", ProjectID: "project-1", IssueID: "issue-1", IssueStatus: "in_progress",
		AgentID: "agent-1", Model: "claude", SessionID: "session-1",
		FailureReason: "timeout", ErrorText: "task timed out", Attempt: 2, MaxAttempts: 2,
		RetryPending: false,
	}
}

func TestTaskFailureGateFiresOnTaskFailureHook(t *testing.T) {
	store := &fakeTaskFailureStore{failure: newTestFailureContext()}
	evaluator := &recordingEvaluator{}
	bus := events.New()
	NewTaskFailureGate(store, evaluator).Attach(bus)

	bus.Publish(failedTaskEvent(testFailureTaskID))

	if len(evaluator.events) != 1 {
		t.Fatalf("evaluated %d events, want 1", len(evaluator.events))
	}
	event := evaluator.events[0]
	if event.Type != HookOnTaskFailure {
		t.Fatalf("event type = %q, want %q", event.Type, HookOnTaskFailure)
	}
	if event.EventID != "task-failure:"+testFailureTaskID {
		t.Fatalf("event id = %q", event.EventID)
	}
	if event.WorkspaceID != "ws-1" || event.IssueID != "issue-1" || event.AgentID != "agent-1" || event.Attempt != 2 {
		t.Fatalf("event scope = %#v", event)
	}
	task, _ := event.Context["task"].(map[string]any)
	if task["failure_reason"] != "timeout" || task["attempt"] != 2 || task["max_attempts"] != 2 {
		t.Fatalf("task context = %#v", task)
	}
	retry, _ := event.Context["retry"].(map[string]any)
	if retry["pending"] != false {
		t.Fatalf("retry context = %#v", retry)
	}
	issue, _ := event.Context["issue"].(map[string]any)
	if issue["status"] != "in_progress" {
		t.Fatalf("issue context = %#v", issue)
	}
}

func TestTaskFailureGateSkipsNonFailureBroadcasts(t *testing.T) {
	cases := []struct {
		name  string
		event events.Event
	}{
		{"cancelled task", func() events.Event {
			e := failedTaskEvent(testFailureTaskID)
			e.Payload.(map[string]any)["status"] = "cancelled"
			return e
		}()},
		{"invalid task id", failedTaskEvent("not-a-uuid")},
		{"non-map payload", events.Event{Type: protocol.EventTaskFailed, Payload: "boom"}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			store := &fakeTaskFailureStore{failure: newTestFailureContext()}
			evaluator := &recordingEvaluator{}
			bus := events.New()
			NewTaskFailureGate(store, evaluator).Attach(bus)
			bus.Publish(tc.event)
			if len(evaluator.events) != 0 {
				t.Fatalf("evaluated %d events, want 0", len(evaluator.events))
			}
		})
	}
}

func TestTaskFailureGateSkipsWhenContextUnavailableOrDisabled(t *testing.T) {
	cases := []struct {
		name  string
		store *fakeTaskFailureStore
	}{
		{"store error (chat task)", &fakeTaskFailureStore{err: errors.New("no rows")}},
		{"workspace flag off", &fakeTaskFailureStore{failure: func() TaskFailureContext {
			f := newTestFailureContext()
			f.HooksEnabled = false
			return f
		}()}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			evaluator := &recordingEvaluator{}
			bus := events.New()
			NewTaskFailureGate(tc.store, evaluator).Attach(bus)
			bus.Publish(failedTaskEvent(testFailureTaskID))
			if len(evaluator.events) != 0 {
				t.Fatalf("evaluated %d events, want 0", len(evaluator.events))
			}
		})
	}
}

func TestTaskFailureGateServerFeatureFlagOffIsInert(t *testing.T) {
	store := &fakeTaskFailureStore{failure: newTestFailureContext()}
	evaluator := &recordingEvaluator{}
	bus := events.New()
	NewTaskFailureGate(store, evaluator, false).Attach(bus)
	bus.Publish(failedTaskEvent(testFailureTaskID))
	if store.loaded != 0 || len(evaluator.events) != 0 {
		t.Fatalf("disabled gate did work: loads=%d evals=%d", store.loaded, len(evaluator.events))
	}
}

// TestTaskFailureGuardPolicyMatchesOnlyTerminalFailures runs the real engine:
// the no-silent-failure policy (condition retry.pending eq false) must match a
// terminal failure and stay quiet while an auto-retry is pending.
func TestTaskFailureGuardPolicyMatchesOnlyTerminalFailures(t *testing.T) {
	policy := newTestHookPolicy("no-silent-failure", HookAllow, HookModeEnforce, HookBinding{Kind: HookScopeWorkspace, ID: "ws-1"})
	policy.Events = []HookEventType{HookOnTaskFailure}
	policy.Conditions = []Condition{{Field: "retry.pending", Op: "eq", Value: false}}
	policy.Handlers[0].Actions = []HookAction{
		{Type: "issue.comment", Config: map[string]any{"body": "Run stopped: {{task.failure_reason}}"}},
		{Type: "issue.status", Config: map[string]any{"status": "blocked"}},
	}

	run := func(retryPending bool, eventID string) HookResult {
		store := NewMemoryHookStore([]HookPolicy{policy})
		executed := map[string]int{}
		actions := NewActionRegistry()
		for _, name := range []string{"issue.comment", "issue.status"} {
			name := name
			actions.Register(name, func(_ context.Context, in ActionInvocation) (map[string]any, error) {
				executed[name]++
				return map[string]any{"body": renderHookTemplate(hookConfigString(in.Action.Config, "body", ""), in.Event)}, nil
			})
		}
		failure := newTestFailureContext()
		failure.RetryPending = retryPending
		failureStore := &fakeTaskFailureStore{failure: failure}
		engine := NewHookEngine(true, store).WithActionRegistry(actions)
		bus := events.New()
		NewTaskFailureGate(failureStore, engine).Attach(bus)
		event := failedTaskEvent(testFailureTaskID)
		event.Payload.(map[string]any)["task_id"] = eventID
		failureStore.failure.TaskID = eventID
		bus.Publish(event)
		result, _ := store.GetResult(context.Background(), "task-failure:"+eventID)
		return result
	}

	terminalID := util.UUIDToString(mustTestUUID(t, "1b2c3d4e-0000-4000-8000-000000000001"))
	terminal := run(false, terminalID)
	if len(terminal.Matches) != 1 || len(terminal.ActionResults) != 2 {
		t.Fatalf("terminal failure result = %#v", terminal)
	}
	for _, actionResult := range terminal.ActionResults {
		if actionResult.Status != HookActionSuccess {
			t.Fatalf("action %s status = %s", actionResult.Type, actionResult.Status)
		}
	}
	if body, _ := terminal.ActionResults[0].Result["body"].(string); body != "Run stopped: timeout" {
		t.Fatalf("rendered comment = %q", body)
	}

	retryingID := util.UUIDToString(mustTestUUID(t, "1b2c3d4e-0000-4000-8000-000000000002"))
	retrying := run(true, retryingID)
	if len(retrying.Matches) != 0 || len(retrying.ActionResults) != 0 {
		t.Fatalf("retry-pending failure should not match: %#v", retrying)
	}
}

func TestRenderHookTemplate(t *testing.T) {
	event := HookEvent{
		Type: HookOnTaskFailure, WorkspaceID: "ws-1", IssueID: "issue-1", Attempt: 2,
		Context: map[string]any{
			"task": map[string]any{"failure_reason": "timeout", "attempt": 2, "max_attempts": 3},
		},
	}
	got := renderHookTemplate("Reason {{task.failure_reason}}, attempt {{ task.attempt }} of {{task.max_attempts}} on {{issue.id}}; keep {{unknown.path}}.", event)
	want := "Reason timeout, attempt 2 of 3 on issue-1; keep {{unknown.path}}."
	if got != want {
		t.Fatalf("rendered = %q, want %q", got, want)
	}
	if plain := renderHookTemplate("no placeholders", event); plain != "no placeholders" {
		t.Fatalf("plain = %q", plain)
	}
}

func TestValidateIssueActions(t *testing.T) {
	if err := validateTypedHookAction(HookAction{Type: "issue.comment", Config: map[string]any{}}); err == nil {
		t.Fatal("issue.comment without body must fail validation")
	}
	if err := validateTypedHookAction(HookAction{Type: "issue.comment", Config: map[string]any{"body": "x"}}); err != nil {
		t.Fatalf("issue.comment with body = %v", err)
	}
	if err := validateTypedHookAction(HookAction{Type: "issue.status", Config: map[string]any{"status": "parked"}}); err == nil {
		t.Fatal("issue.status with unknown status must fail validation")
	}
	if err := validateTypedHookAction(HookAction{Type: "issue.status", Config: map[string]any{"status": "blocked"}}); err != nil {
		t.Fatalf("issue.status blocked = %v", err)
	}
}

func mustTestUUID(t *testing.T, value string) pgtype.UUID {
	t.Helper()
	id, err := util.ParseUUID(value)
	if err != nil {
		t.Fatal(err)
	}
	return id
}
