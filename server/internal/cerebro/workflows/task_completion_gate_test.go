package workflows

import (
	"context"
	"encoding/json"
	"errors"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgtype"
)

func TestTaskCompletionGateAllowsTerminalIssueWithoutEvaluation(t *testing.T) {
	store := &fakeCompletionContextStore{context: TaskCompletionContext{IssueStatus: "done"}}
	evaluator := &fakeHookEvaluator{result: HookResult{Decision: HookBlock}}
	gate := NewTaskCompletionGate(store, evaluator)

	got, err := gate.BeforeComplete(context.Background(), pgtype.UUID{Valid: true}, []byte(`{"output":"done"}`))
	if err != nil {
		t.Fatal(err)
	}
	if evaluator.calls != 0 || string(got) != `{"output":"done"}` {
		t.Fatalf("terminal completion evaluated=%d result=%s", evaluator.calls, got)
	}
}

func TestTaskCompletionGateRequiresActualContinuation(t *testing.T) {
	store := &fakeCompletionContextStore{context: nonTerminalCompletionContext()}
	evaluator := &fakeHookEvaluator{result: HookResult{Decision: HookRequire, Requirements: []string{"Create a wakeup or handoff"}}}
	gate := NewTaskCompletionGate(store, evaluator)

	_, err := gate.BeforeComplete(context.Background(), pgtype.UUID{Bytes: [16]byte{1}, Valid: true}, []byte(`{"output":"I will continue later"}`))
	if !errors.Is(err, ErrTaskContinuationRequired) {
		t.Fatalf("error = %v", err)
	}

	store.continuation = &TaskContinuation{Kind: ContinuationWakeup, EvidenceID: "wakeup-1"}
	got, err := gate.BeforeComplete(context.Background(), pgtype.UUID{Bytes: [16]byte{2}, Valid: true}, []byte(`{"output":"done"}`))
	if err != nil {
		t.Fatal(err)
	}
	var payload struct {
		Continuation TaskContinuation `json:"continuation"`
	}
	if err := json.Unmarshal(got, &payload); err != nil {
		t.Fatal(err)
	}
	if payload.Continuation.Kind != ContinuationWakeup || payload.Continuation.EvidenceID != "wakeup-1" {
		t.Fatalf("continuation = %#v", payload.Continuation)
	}
}

func TestTaskCompletionGateDryRunAuditsWithoutBlocking(t *testing.T) {
	store := &fakeCompletionContextStore{context: nonTerminalCompletionContext()}
	evaluator := &fakeHookEvaluator{result: HookResult{Decision: HookAllow, WouldDecision: HookBlock}}
	gate := NewTaskCompletionGate(store, evaluator)
	if _, err := gate.BeforeComplete(context.Background(), pgtype.UUID{Bytes: [16]byte{3}, Valid: true}, nil); err != nil {
		t.Fatalf("dry-run blocked completion: %v", err)
	}
}

func TestTaskCompletionGateFailsClosedWhenContextCannotBeLoaded(t *testing.T) {
	store := &fakeCompletionContextStore{loadErr: errors.New("agent lookup failed")}
	evaluator := &fakeHookEvaluator{result: HookResult{Decision: HookBlock}}
	gate := NewTaskCompletionGate(store, evaluator)
	input := []byte(`{"output":"done"}`)

	got, err := gate.BeforeComplete(context.Background(), pgtype.UUID{Bytes: [16]byte{6}, Valid: true}, input)
	if !errors.Is(err, ErrTaskCompletionContextUnavailable) {
		t.Fatalf("context lookup error = %v", err)
	}
	if evaluator.calls != 0 || string(got) != string(input) {
		t.Fatalf("lookup failure evaluated=%d result=%s", evaluator.calls, got)
	}
}

func TestTaskCompletionGateFailsClosedWhenFeatureLookupFails(t *testing.T) {
	store := &fakeCompletionContextStore{featureErr: errors.New("workspace lookup failed")}
	evaluator := &fakeHookEvaluator{result: HookResult{Decision: HookAllow}}
	gate := NewTaskCompletionGate(store, evaluator)

	_, err := gate.BeforeComplete(context.Background(), pgtype.UUID{Bytes: [16]byte{9}, Valid: true}, nil)
	if !errors.Is(err, ErrTaskCompletionContextUnavailable) {
		t.Fatalf("feature lookup error = %v", err)
	}
	if store.loadCalls != 0 || evaluator.calls != 0 {
		t.Fatalf("feature failure loaded context=%d evaluated=%d", store.loadCalls, evaluator.calls)
	}
}

func TestTaskCompletionGateChecksWorkspaceFeatureBeforeLoadingAgentContext(t *testing.T) {
	store := &fakeCompletionContextStore{
		featureDisabled: true,
		loadErr:         errors.New("agent lookup must not run"),
	}
	evaluator := &fakeHookEvaluator{result: HookResult{Decision: HookBlock}}
	gate := NewTaskCompletionGate(store, evaluator)
	input := []byte(`{"output":"done"}`)

	got, err := gate.BeforeComplete(context.Background(), pgtype.UUID{Bytes: [16]byte{7}, Valid: true}, input)
	if err != nil {
		t.Fatalf("disabled workspace was blocked: %v", err)
	}
	if store.loadCalls != 0 || evaluator.calls != 0 || string(got) != string(input) {
		t.Fatalf("disabled workspace loaded context=%d evaluated=%d result=%s", store.loadCalls, evaluator.calls, got)
	}
}

func TestTaskCompletionGateBypassesAllLookupsWhenServerFeatureIsDisabled(t *testing.T) {
	store := &fakeCompletionContextStore{loadErr: errors.New("must not run")}
	evaluator := &fakeHookEvaluator{result: HookResult{Decision: HookBlock}}
	gate := NewTaskCompletionGate(store, evaluator, false)

	if _, err := gate.BeforeComplete(context.Background(), pgtype.UUID{Bytes: [16]byte{8}, Valid: true}, nil); err != nil {
		t.Fatalf("disabled server feature blocked completion: %v", err)
	}
	if store.featureCalls != 0 || store.loadCalls != 0 || evaluator.calls != 0 {
		t.Fatalf("disabled server feature called feature=%d context=%d evaluator=%d", store.featureCalls, store.loadCalls, evaluator.calls)
	}
}

func TestTaskCompletionGateEmitsAgentStopAndTaskCompleteThroughTheSameAttemptCeiling(t *testing.T) {
	store := &fakeCompletionContextStore{context: nonTerminalCompletionContext()}
	evaluator := &fakeHookEvaluator{result: HookResult{Decision: HookAllow}}
	gate := NewTaskCompletionGate(store, evaluator)
	if _, err := gate.BeforeComplete(context.Background(), pgtype.UUID{Bytes: [16]byte{5}, Valid: true}, nil); err != nil {
		t.Fatal(err)
	}
	if len(evaluator.events) != 2 || evaluator.events[0].Type != HookBeforeAgentStop || evaluator.events[1].Type != HookBeforeTaskComplete {
		t.Fatalf("completion events = %#v", evaluator.events)
	}
	if evaluator.events[0].Attempt != 1 || evaluator.events[1].Attempt != 1 {
		t.Fatalf("completion events do not share the attempt ceiling: %#v", evaluator.events)
	}
}

func TestTaskCompletionGateNeverTurnsFailedRemediationIntoCompletion(t *testing.T) {
	store := &fakeCompletionContextStore{context: nonTerminalCompletionContext()}
	evaluator := &fakeHookEvaluator{result: HookResult{Decision: HookBlock}}
	gate := NewTaskCompletionGate(store, evaluator)
	taskID := pgtype.UUID{Bytes: [16]byte{4}, Valid: true}
	for attempt := 1; attempt <= 4; attempt++ {
		if _, err := gate.BeforeComplete(context.Background(), taskID, nil); !errors.Is(err, ErrTaskContinuationRequired) {
			t.Fatalf("attempt %d error = %v", attempt, err)
		}
	}
}

// FIR-4643: a run that called no tool wrote nothing to the issue, so there is
// nothing for it to continue. It must stop without the gate turning it into a
// failed task.
func TestTaskCompletionGateAllowsSilentRunWithoutEvaluation(t *testing.T) {
	completion := nonTerminalCompletionContext()
	completion.SilentRun = true
	store := &fakeCompletionContextStore{context: completion}
	evaluator := &fakeHookEvaluator{result: HookResult{Decision: HookRequire, Requirements: []string{"Create a wakeup or handoff"}}}
	gate := NewTaskCompletionGate(store, evaluator)

	got, err := gate.BeforeComplete(context.Background(), pgtype.UUID{Bytes: [16]byte{7}, Valid: true}, []byte(`{"output":"nothing to report"}`))
	if err != nil {
		t.Fatalf("silent run blocked: %v", err)
	}
	if evaluator.calls != 0 {
		t.Fatalf("silent run evaluated %d hooks", evaluator.calls)
	}
	if string(got) != `{"output":"nothing to report"}` {
		t.Fatalf("result = %s", got)
	}
}

// The same run still blocks when the runtime reported a tool call, so the
// exemption cannot be reached by a run that actually did something.
func TestTaskCompletionGateStillBlocksRunThatUsedTools(t *testing.T) {
	store := &fakeCompletionContextStore{context: nonTerminalCompletionContext()}
	evaluator := &fakeHookEvaluator{result: HookResult{Decision: HookRequire, Requirements: []string{"Create a wakeup or handoff"}}}
	gate := NewTaskCompletionGate(store, evaluator)

	_, err := gate.BeforeComplete(context.Background(), pgtype.UUID{Bytes: [16]byte{8}, Valid: true}, []byte(`{"output":"nothing to report"}`))
	if !errors.Is(err, ErrTaskContinuationRequired) {
		t.Fatalf("error = %v", err)
	}
}

type fakeCompletionContextStore struct {
	context         TaskCompletionContext
	continuation    *TaskContinuation
	loadErr         error
	loadCalls       int
	featureCalls    int
	featureDisabled bool
	featureErr      error
}

func (s *fakeCompletionContextStore) LoadTaskCompletionContext(context.Context, pgtype.UUID) (TaskCompletionContext, error) {
	s.loadCalls++
	return s.context, s.loadErr
}

func (s *fakeCompletionContextStore) WorkflowHooksEnabled(context.Context, pgtype.UUID) (bool, error) {
	s.featureCalls++
	return !s.featureDisabled, s.featureErr
}

func (s *fakeCompletionContextStore) ResolveContinuation(context.Context, TaskCompletionContext) (*TaskContinuation, error) {
	return s.continuation, nil
}

type fakeHookEvaluator struct {
	result HookResult
	calls  int
	events []HookEvent
}

func (e *fakeHookEvaluator) Evaluate(_ context.Context, event HookEvent) (HookResult, error) {
	e.calls++
	e.events = append(e.events, event)
	return e.result, nil
}

func nonTerminalCompletionContext() TaskCompletionContext {
	return TaskCompletionContext{
		TaskID: "task-1", WorkspaceID: "workspace-1", IssueID: "issue-1",
		IssueStatus: "in_progress", AgentID: "agent-1", StartedAt: time.Now().Add(-time.Minute),
	}
}
