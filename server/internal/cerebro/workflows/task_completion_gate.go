package workflows

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"sync"
	"time"

	"github.com/jackc/pgx/v5/pgtype"
)

var ErrTaskContinuationRequired = errors.New("task completion requires an actual continuation")

type TaskContinuationKind string

const (
	ContinuationCurrentRun   TaskContinuationKind = "continue_current_run"
	ContinuationWakeup       TaskContinuationKind = "wakeup"
	ContinuationAskMember    TaskContinuationKind = "ask_member"
	ContinuationNotifyMember TaskContinuationKind = "notify_member"
	ContinuationAgent        TaskContinuationKind = "agent_dispatch"
	ContinuationSquad        TaskContinuationKind = "squad_dispatch"
	ContinuationHandoff      TaskContinuationKind = "handoff"
	ContinuationBlocked      TaskContinuationKind = "blocked"
)

type TaskContinuation struct {
	Kind       TaskContinuationKind `json:"kind"`
	EvidenceID string               `json:"evidence_id"`
}

type TaskCompletionContext struct {
	TaskID      string
	WorkspaceID string
	ProjectID   string
	WorkflowID  string
	IssueID     string
	IssueStatus string
	AgentID     string
	Model       string
	SessionID   string
	TaskAttempt int
	StartedAt   time.Time
}

type TaskCompletionContextStore interface {
	WorkflowHooksEnabled(context.Context, pgtype.UUID) (bool, error)
	LoadTaskCompletionContext(context.Context, pgtype.UUID) (TaskCompletionContext, error)
	ResolveContinuation(context.Context, TaskCompletionContext) (*TaskContinuation, error)
}

type HookEvaluator interface {
	Evaluate(context.Context, HookEvent) (HookResult, error)
}

type TaskCompletionGate struct {
	store          TaskCompletionContextStore
	evaluator      HookEvaluator
	featureEnabled bool
	mu             sync.Mutex
	attempts       map[string]int
}

func NewTaskCompletionGate(store TaskCompletionContextStore, evaluator HookEvaluator, serverFeatureEnabled ...bool) *TaskCompletionGate {
	enabled := true
	if len(serverFeatureEnabled) > 0 {
		enabled = serverFeatureEnabled[0]
	}
	return &TaskCompletionGate{store: store, evaluator: evaluator, featureEnabled: enabled, attempts: make(map[string]int)}
}

func (g *TaskCompletionGate) BeforeComplete(ctx context.Context, taskID pgtype.UUID, result []byte) ([]byte, error) {
	if !g.featureEnabled {
		return result, nil
	}
	enabled, err := g.store.WorkflowHooksEnabled(ctx, taskID)
	if err != nil || !enabled {
		return result, nil
	}
	completion, err := g.store.LoadTaskCompletionContext(ctx, taskID)
	if err != nil {
		return result, nil
	}
	if completion.IssueID == "" || isTerminalIssueStatus(completion.IssueStatus) {
		return result, nil
	}
	continuation, err := g.store.ResolveContinuation(ctx, completion)
	if err != nil {
		return result, err
	}
	attempt := g.nextAttempt(completion.TaskID)
	event := HookEvent{
		EventID: fmt.Sprintf("%s:agent-stop:%d", completion.TaskID, attempt), Type: HookBeforeAgentStop,
		WorkspaceID: completion.WorkspaceID, ProjectID: completion.ProjectID, WorkflowID: completion.WorkflowID,
		AgentID: completion.AgentID, Model: completion.Model, IssueID: completion.IssueID, SessionID: completion.SessionID,
		Attempt: attempt,
		Context: map[string]any{
			"issue":        map[string]any{"id": completion.IssueID, "status": completion.IssueStatus, "terminal": false},
			"continuation": continuationContext(continuation),
		},
	}
	agentStop, err := g.evaluator.Evaluate(ctx, event)
	if err != nil {
		return result, err
	}
	event.EventID = fmt.Sprintf("%s:complete:%d", completion.TaskID, attempt)
	event.Type = HookBeforeTaskComplete
	taskComplete, err := g.evaluator.Evaluate(ctx, event)
	if err != nil {
		return result, err
	}
	evaluation := combineCompletionEvaluations(agentStop, taskComplete)
	if continuation != nil {
		result, err = withTaskContinuation(result, *continuation)
		if err != nil {
			return result, err
		}
	}
	if evaluation.Decision != HookBlock && evaluation.Decision != HookRequire {
		return result, nil
	}
	if continuation != nil {
		return result, nil
	}
	if attempt >= 3 {
		return result, nil
	}
	requirement := "create a wakeup, member request, dispatch, handoff, or mark the issue blocked"
	if len(evaluation.Requirements) > 0 {
		requirement = evaluation.Requirements[0]
	}
	return result, fmt.Errorf("%w: %s", ErrTaskContinuationRequired, requirement)
}

func combineCompletionEvaluations(results ...HookResult) HookResult {
	combined := HookResult{Decision: HookAllow}
	for _, result := range results {
		combined.Decision = strongerDecision(combined.Decision, result.Decision)
		combined.WouldDecision = strongerDecision(combined.WouldDecision, result.WouldDecision)
		combined.Requirements = append(combined.Requirements, result.Requirements...)
		combined.Warning = result.Warning
	}
	return combined
}

func (g *TaskCompletionGate) nextAttempt(taskID string) int {
	g.mu.Lock()
	defer g.mu.Unlock()
	g.attempts[taskID]++
	return g.attempts[taskID]
}

func isTerminalIssueStatus(status string) bool {
	return status == "done" || status == "cancelled"
}

func continuationContext(continuation *TaskContinuation) map[string]any {
	if continuation == nil {
		return map[string]any{"present": false}
	}
	return map[string]any{"present": true, "kind": continuation.Kind, "evidence_id": continuation.EvidenceID}
}

func withTaskContinuation(result []byte, continuation TaskContinuation) ([]byte, error) {
	payload := map[string]any{}
	if len(result) > 0 {
		if err := json.Unmarshal(result, &payload); err != nil {
			return result, err
		}
	}
	payload["continuation"] = continuation
	return json.Marshal(payload)
}
