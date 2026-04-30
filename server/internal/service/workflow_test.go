package service

import (
	"context"
	"errors"
	"testing"

	"github.com/jackc/pgx/v5"
	"github.com/multica-ai/multica/server/internal/events"
)

// ---------------------------------------------------------------------------
// Test fakes
// ---------------------------------------------------------------------------

// fakeAgentStore is the in-memory agentWorkflowStore implementation used
// by the agent-path tests. It records every call so assertions can verify
// that the engine dispatched the right step / parked the run / marked it
// done.
type fakeAgentStore struct {
	// run is what FindRun returns. nil + findErr controls the not-found path.
	run     *agentWorkflowRun
	findErr error

	// task id returned by CreateAgentTask.
	taskIDToReturn string

	// recorded calls
	createCalls   []agentCreateCall
	updateCalls   []agentUpdateCall
	doneCalls     []string
	failedCalls   []agentFailedCall
	setTaskCalls  []agentTaskOrIssueCall
	setIssueCalls []agentTaskOrIssueCall
}

type agentTaskOrIssueCall struct {
	runID string
	id    string
}

type agentCreateCall struct {
	runID        string
	step         string
	instructions string
}

type agentUpdateCall struct {
	runID  string
	status string
	step   string
	state  map[string]any
}

type agentFailedCall struct {
	runID  string
	reason string
}

func (s *fakeAgentStore) FindRun(_ context.Context, _, _, _ string) (*agentWorkflowRun, error) {
	if s.findErr != nil {
		return nil, s.findErr
	}
	return s.run, nil
}

func (s *fakeAgentStore) CreateAgentTask(_ context.Context, run *agentWorkflowRun, instructions string) (string, error) {
	s.createCalls = append(s.createCalls, agentCreateCall{
		runID:        run.ID,
		step:         run.CurrentStep,
		instructions: instructions,
	})
	if s.taskIDToReturn == "" {
		return "task-new", nil
	}
	return s.taskIDToReturn, nil
}

func (s *fakeAgentStore) UpdateRun(_ context.Context, runID, status, step string, state map[string]any) error {
	// Defensive copy so later mutations to the engine's state map don't
	// retroactively mutate the recorded call.
	stateCopy := make(map[string]any, len(state))
	for k, v := range state {
		stateCopy[k] = v
	}
	s.updateCalls = append(s.updateCalls, agentUpdateCall{
		runID:  runID,
		status: status,
		step:   step,
		state:  stateCopy,
	})
	return nil
}

func (s *fakeAgentStore) MarkDone(_ context.Context, runID string) error {
	s.doneCalls = append(s.doneCalls, runID)
	return nil
}

func (s *fakeAgentStore) MarkFailed(_ context.Context, runID, reason string) error {
	s.failedCalls = append(s.failedCalls, agentFailedCall{runID: runID, reason: reason})
	return nil
}

func (s *fakeAgentStore) SetRunCurrentTask(_ context.Context, runID, taskID string) error {
	s.setTaskCalls = append(s.setTaskCalls, agentTaskOrIssueCall{runID: runID, id: taskID})
	return nil
}

func (s *fakeAgentStore) SetRunCurrentIssue(_ context.Context, runID, issueID string) error {
	s.setIssueCalls = append(s.setIssueCalls, agentTaskOrIssueCall{runID: runID, id: issueID})
	return nil
}

// sampleAgentDefinition mirrors a typical six-step approval-gated
// workflow: ingest → compute → draft → review (human) → file → archive.
func sampleAgentDefinition() map[string]any {
	return map[string]any{
		"steps": []any{
			map[string]any{"id": "ingest", "actor": "agent", "tool": "service.fetch", "next": "compute"},
			map[string]any{"id": "compute", "actor": "agent", "skill": "by_kind", "next": "draft"},
			map[string]any{"id": "draft", "actor": "agent", "tool": "service.draft", "next": "review"},
			map[string]any{
				"id": "review", "actor": "human", "gate": "issue_approval",
				"next_on_approve": "file",
				"next_on_reject":  "draft",
			},
			map[string]any{"id": "file", "actor": "agent", "tool": "service.file", "next": "archive"},
			map[string]any{"id": "archive", "actor": "agent", "tool": "service.archive"},
		},
	}
}

func newEngineWithStore(s *fakeAgentStore) *WorkflowEngine {
	e := NewWorkflowEngine(nil, nil, nil, nil)
	e.SetAgentStore(s)
	return e
}

// ---------------------------------------------------------------------------
// Agent path
// ---------------------------------------------------------------------------

// task:completed advances to the next agent step.
func TestWorkflowEngine_Agent_TaskCompleted_AdvancesToNextAgentStep(t *testing.T) {
	store := &fakeAgentStore{
		run: &agentWorkflowRun{
			ID:          "run-1",
			WorkflowID:  "wf-1",
			WorkspaceID: "ws-1",
			AgentID:     "agent-1",
			Status:      "running",
			CurrentStep: "ingest",
			State: map[string]any{
				"period":    "2024-09",
				"client_id": "client-1",
			},
			Definition: sampleAgentDefinition(),
		},
		taskIDToReturn: "task-2",
	}
	e := newEngineWithStore(store)

	e.handleTaskCompleted(taskCompletedEvent("task-1"))

	if len(store.createCalls) != 1 {
		t.Fatalf("expected 1 create call, got %d", len(store.createCalls))
	}
	if got := store.createCalls[0].step; got != "ingest" {
		// Engine still has run.CurrentStep = "ingest" when CreateAgentTask
		// is invoked — the run is updated AFTER the task is created.
		t.Fatalf("create call step = %q, want %q", got, "ingest")
	}
	if len(store.updateCalls) != 1 {
		t.Fatalf("expected 1 update call, got %d", len(store.updateCalls))
	}
	upd := store.updateCalls[0]
	if upd.status != "running" || upd.step != "compute" {
		t.Fatalf("update = (%s, %s), want (running, compute)", upd.status, upd.step)
	}
	if len(store.setTaskCalls) != 1 || store.setTaskCalls[0].id != "task-2" {
		t.Fatalf("expected SetRunCurrentTask(_, task-2), got %+v", store.setTaskCalls)
	}
	if len(store.doneCalls) != 0 {
		t.Fatalf("did not expect MarkDone, got %d calls", len(store.doneCalls))
	}
	if len(store.failedCalls) != 0 {
		t.Fatalf("did not expect MarkFailed, got %v", store.failedCalls)
	}
}

// Task completion at a step whose `next` points at a human-actor step
// parks the run in awaiting_approval and does NOT enqueue a new task.
func TestWorkflowEngine_Agent_NextStepIsHumanGate_ParksRun(t *testing.T) {
	store := &fakeAgentStore{
		run: &agentWorkflowRun{
			ID:          "run-park",
			WorkflowID:  "wf-1",
			WorkspaceID: "ws-1",
			AgentID:     "agent-1",
			Status:      "running",
			CurrentStep: "draft", // next = "review" (human)
			State:       map[string]any{},
			Definition:  sampleAgentDefinition(),
		},
	}
	e := newEngineWithStore(store)

	e.handleTaskCompleted(taskCompletedEvent("task-draft"))

	if len(store.createCalls) != 0 {
		t.Fatalf("did not expect a new agent task at human gate, got %d", len(store.createCalls))
	}
	if len(store.updateCalls) != 1 {
		t.Fatalf("expected one UpdateRun call, got %d", len(store.updateCalls))
	}
	upd := store.updateCalls[0]
	if upd.status != "awaiting_approval" || upd.step != "review" {
		t.Fatalf("update = (%s, %s), want (awaiting_approval, review)", upd.status, upd.step)
	}
}

// issue:resolved with approved → next_on_approve branch.
func TestWorkflowEngine_Agent_IssueResolved_Approved_TakesApproveBranch(t *testing.T) {
	store := &fakeAgentStore{
		run: &agentWorkflowRun{
			ID:          "run-2",
			WorkflowID:  "wf-1",
			WorkspaceID: "ws-1",
			AgentID:     "agent-1",
			Status:      "awaiting_approval",
			CurrentStep: "review",
			State:       map[string]any{"period": "2024-09"},
			Definition:  sampleAgentDefinition(),
		},
		taskIDToReturn: "task-file",
	}
	e := newEngineWithStore(store)

	e.handleIssueResolved(issueResolvedEvent("issue-7", "approved"))

	if len(store.createCalls) != 1 {
		t.Fatalf("expected 1 create call after approve, got %d", len(store.createCalls))
	}
	upd := lastAgentUpdate(t, store)
	if upd.step != "file" || upd.status != "running" {
		t.Fatalf("update = (%s, %s), want (running, file)", upd.status, upd.step)
	}
	if len(store.setTaskCalls) != 1 {
		t.Fatalf("expected exactly one SetRunCurrentTask call, got %+v", store.setTaskCalls)
	}
}

// issue:resolved with rejected → next_on_reject branch (loops back to
// 'draft' which is also an agent step).
func TestWorkflowEngine_Agent_IssueResolved_Rejected_TakesRejectBranch(t *testing.T) {
	store := &fakeAgentStore{
		run: &agentWorkflowRun{
			ID:          "run-3",
			WorkflowID:  "wf-1",
			WorkspaceID: "ws-1",
			AgentID:     "agent-1",
			Status:      "awaiting_approval",
			CurrentStep: "review",
			State:       map[string]any{},
			Definition:  sampleAgentDefinition(),
		},
		taskIDToReturn: "task-redraft",
	}
	e := newEngineWithStore(store)

	e.handleIssueResolved(issueResolvedEvent("issue-9", "rejected"))

	if len(store.createCalls) != 1 {
		t.Fatalf("expected 1 create call on reject, got %d", len(store.createCalls))
	}
	upd := lastAgentUpdate(t, store)
	if upd.step != "draft" {
		t.Fatalf("update.step = %q, want draft (reject branch)", upd.step)
	}
}

// issue:resolved with an unknown resolution string is a silent no-op.
func TestWorkflowEngine_Agent_IssueResolved_UnknownResolution_IsNoop(t *testing.T) {
	store := &fakeAgentStore{
		run: &agentWorkflowRun{
			ID:          "run-noop",
			WorkflowID:  "wf-1",
			WorkspaceID: "ws-1",
			AgentID:     "agent-1",
			Status:      "awaiting_approval",
			CurrentStep: "review",
			State:       map[string]any{},
			Definition:  sampleAgentDefinition(),
		},
	}
	e := newEngineWithStore(store)

	e.handleIssueResolved(issueResolvedEvent("issue-x", "deferred"))

	if total := len(store.createCalls) + len(store.updateCalls) + len(store.doneCalls); total != 0 {
		t.Fatalf("expected no side effects on unknown resolution, store=%+v", store)
	}
}

// Terminal step (next null) marks run done.
func TestWorkflowEngine_Agent_TerminalStep_MarksRunDone(t *testing.T) {
	store := &fakeAgentStore{
		run: &agentWorkflowRun{
			ID:          "run-4",
			WorkflowID:  "wf-1",
			WorkspaceID: "ws-1",
			AgentID:     "agent-1",
			Status:      "running",
			CurrentStep: "archive", // terminal: no `next` in definition
			State:       map[string]any{},
			Definition:  sampleAgentDefinition(),
		},
	}
	e := newEngineWithStore(store)

	e.handleTaskCompleted(taskCompletedEvent("task-archive"))

	if len(store.createCalls) != 0 {
		t.Fatalf("did not expect a new task at terminal step, got %d", len(store.createCalls))
	}
	if len(store.doneCalls) != 1 || store.doneCalls[0] != "run-4" {
		t.Fatalf("expected MarkDone(run-4), got %v", store.doneCalls)
	}
}

// Run not found → no-op, no error.
func TestWorkflowEngine_Agent_RunNotFound_IsNoop(t *testing.T) {
	store := &fakeAgentStore{findErr: pgx.ErrNoRows}
	e := newEngineWithStore(store)

	e.handleTaskCompleted(taskCompletedEvent("orphan-task"))
	e.handleIssueResolved(issueResolvedEvent("orphan-issue", "approved"))

	if total := len(store.createCalls) + len(store.updateCalls) + len(store.doneCalls) + len(store.failedCalls); total != 0 {
		t.Fatalf("expected no side effects for unknown task/issue, store=%+v", store)
	}
}

// Non-pgx errors from FindRun also stay silent (logged but no side
// effects). Captures the engine's "fail closed, never panic" stance.
func TestWorkflowEngine_Agent_FindRunGenericError_IsNoop(t *testing.T) {
	store := &fakeAgentStore{findErr: errors.New("connection reset")}
	e := newEngineWithStore(store)

	e.handleTaskCompleted(taskCompletedEvent("task-x"))

	if len(store.createCalls) != 0 {
		t.Fatalf("expected no dispatch on lookup error, got %d", len(store.createCalls))
	}
}

// Definition with current_step missing → marks run failed.
func TestWorkflowEngine_Agent_UnknownCurrentStep_MarksFailed(t *testing.T) {
	store := &fakeAgentStore{
		run: &agentWorkflowRun{
			ID:          "run-fail",
			WorkflowID:  "wf-1",
			WorkspaceID: "ws-1",
			AgentID:     "agent-1",
			Status:      "running",
			CurrentStep: "ghost",
			State:       map[string]any{},
			Definition:  sampleAgentDefinition(),
		},
	}
	e := newEngineWithStore(store)

	e.handleTaskCompleted(taskCompletedEvent("task-bad"))

	if len(store.failedCalls) != 1 || store.failedCalls[0].runID != "run-fail" {
		t.Fatalf("expected one MarkFailed for run-fail, got %+v", store.failedCalls)
	}
}

// ---------------------------------------------------------------------------
// Team path (stub coverage)
// ---------------------------------------------------------------------------

// task:completed with team_id + role_id is routed to the team path and
// must NOT touch the agent store. Until the team-workflow schema lands
// upstream, the engine logs and returns; the agent store should record
// zero calls.
func TestWorkflowEngine_Team_TaskCompletedWithTeamPayload_DoesNotTouchAgentStore(t *testing.T) {
	store := &fakeAgentStore{
		run: &agentWorkflowRun{
			ID:          "should-not-be-touched",
			WorkflowID:  "wf-1",
			WorkspaceID: "ws-1",
			AgentID:     "agent-1",
			Status:      "running",
			CurrentStep: "ingest",
			State:       map[string]any{},
			Definition:  sampleAgentDefinition(),
		},
	}
	e := newEngineWithStore(store)

	e.handleTaskCompleted(events.Event{
		Type:        "task:completed",
		WorkspaceID: "ws-1",
		Payload: map[string]any{
			"task_id": "task-team-1",
			"team_id": "team-7",
			"role_id": "role-3",
		},
	})

	if total := len(store.createCalls) + len(store.updateCalls) + len(store.doneCalls) + len(store.failedCalls); total != 0 {
		t.Fatalf("team path must not touch agent store, store=%+v", store)
	}
}

// EmitTeamWorkflowBlocked publishes a team:workflow_blocked event via the
// bus when called.
func TestWorkflowEngine_Team_EmitTeamWorkflowBlocked_PublishesEvent(t *testing.T) {
	bus := events.New()
	e := NewWorkflowEngine(nil, nil, bus, nil)

	got := make(chan events.Event, 1)
	bus.Subscribe("team:workflow_blocked", func(evt events.Event) {
		got <- evt
	})

	e.EmitTeamWorkflowBlocked("ws-1", "team-7", "auditor", "no agent assigned")

	select {
	case evt := <-got:
		payload, _ := evt.Payload.(map[string]any)
		if payload["team_id"] != "team-7" || payload["role_name"] != "auditor" {
			t.Fatalf("unexpected blocked payload: %+v", payload)
		}
	default:
		t.Fatalf("expected team:workflow_blocked event")
	}
}

// ---------------------------------------------------------------------------
// helpers
// ---------------------------------------------------------------------------

func taskCompletedEvent(taskID string) events.Event {
	return events.Event{
		Type: "task:completed",
		Payload: map[string]any{
			"task_id": taskID,
		},
	}
}

func issueResolvedEvent(issueID, resolution string) events.Event {
	return events.Event{
		Type: "issue:resolved",
		Payload: map[string]any{
			"issue_id":   issueID,
			"resolution": resolution,
		},
	}
}

func lastAgentUpdate(t *testing.T, s *fakeAgentStore) agentUpdateCall {
	t.Helper()
	if len(s.updateCalls) == 0 {
		t.Fatalf("expected at least one UpdateRun call")
	}
	return s.updateCalls[len(s.updateCalls)-1]
}
