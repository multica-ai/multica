package workflows

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"time"

	"github.com/jackc/pgx/v5/pgtype"
	"github.com/multica-ai/multica/server/internal/service"
)

var (
	ErrTaskContinuationRequired         = errors.New("task completion requires an actual continuation")
	ErrTaskCompletionContextUnavailable = errors.New("task completion context is unavailable")
)

type TaskCompletionGuidance = service.WorkflowCompletionGuidance

var TaskCompletionGuidanceFromError = service.WorkflowCompletionGuidanceFromError

type taskCompletionGuidanceError struct {
	guidance service.WorkflowCompletionGuidance
}

func (e taskCompletionGuidanceError) Error() string {
	return fmt.Sprintf("%s: %s", ErrTaskContinuationRequired, e.guidance.Requirement)
}

func (e taskCompletionGuidanceError) Unwrap() error { return ErrTaskContinuationRequired }

func (e taskCompletionGuidanceError) WorkflowCompletionGuidance() service.WorkflowCompletionGuidance {
	return e.guidance
}

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
	// SilentRun is true when the run reported execution messages, none of them
	// was a tool call, and the agent left no comment on the issue while it ran.
	// Such a run wrote nothing — no comment, no wakeup, no dispatch, no
	// handoff, no status change — so it has nothing to continue. A run whose
	// runtime reported no messages at all is unknown rather than silent and
	// stays gated.
	SilentRun bool
}

type TaskCompletionContextStore interface {
	WorkflowHooksEnabled(context.Context, pgtype.UUID) (bool, error)
	LoadTaskCompletionContext(context.Context, pgtype.UUID) (TaskCompletionContext, error)
	ResolveContinuation(context.Context, TaskCompletionContext) (*TaskContinuation, error)
	NotifyCompletionWarning(context.Context, TaskCompletionContext, service.WorkflowCompletionGuidance) error
}

type HookEvaluator interface {
	Evaluate(context.Context, HookEvent) (HookResult, error)
}

type TaskCompletionGate struct {
	store          TaskCompletionContextStore
	evaluator      HookEvaluator
	featureEnabled bool
}

func NewTaskCompletionGate(store TaskCompletionContextStore, evaluator HookEvaluator, serverFeatureEnabled ...bool) *TaskCompletionGate {
	enabled := true
	if len(serverFeatureEnabled) > 0 {
		enabled = serverFeatureEnabled[0]
	}
	return &TaskCompletionGate{store: store, evaluator: evaluator, featureEnabled: enabled}
}

func (g *TaskCompletionGate) BeforeComplete(ctx context.Context, taskID pgtype.UUID, result []byte) ([]byte, error) {
	if !g.featureEnabled {
		return result, nil
	}
	enabled, err := g.store.WorkflowHooksEnabled(ctx, taskID)
	if err != nil {
		return result, fmt.Errorf("%w: %v", ErrTaskCompletionContextUnavailable, err)
	}
	if !enabled {
		return result, nil
	}
	completion, err := g.store.LoadTaskCompletionContext(ctx, taskID)
	if err != nil {
		return result, fmt.Errorf("%w: %v", ErrTaskCompletionContextUnavailable, err)
	}
	if completion.IssueID == "" || isTerminalIssueStatus(completion.IssueStatus) {
		return result, nil
	}
	// A run that called no tool left nothing behind to continue. Demanding a
	// wakeup or a dispatch from it turns "there was nothing to do" into a
	// failed task whose error text replaces the agent's answer in the human's
	// thread (FIR-4643). The gate exists to catch abandoned work, not silence.
	if completion.SilentRun {
		return result, nil
	}
	continuation, err := g.store.ResolveContinuation(ctx, completion)
	if err != nil {
		return result, err
	}
	attempt := completionAttempt(result)
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
	requirement := "Create a wakeup, ask or notify a member, dispatch an agent or squad, create a handoff, or set the issue to blocked."
	if len(evaluation.Requirements) > 0 {
		requirement = evaluation.Requirements[0]
	}
	guidance := completionGuidance(requirement, attempt, evaluation)
	if attempt == 1 {
		return result, taskCompletionGuidanceError{guidance: guidance}
	}
	result, err = withCompletionWarning(result, guidance)
	if err == nil {
		_ = g.store.NotifyCompletionWarning(ctx, completion, guidance)
	}
	return result, err
}

func combineCompletionEvaluations(results ...HookResult) HookResult {
	combined := HookResult{Decision: HookAllow}
	for _, result := range results {
		combined.Evaluated = combined.Evaluated || result.Evaluated
		combined.Decision = strongerDecision(combined.Decision, result.Decision)
		combined.WouldDecision = strongerDecision(combined.WouldDecision, result.WouldDecision)
		combined.Requirements = append(combined.Requirements, result.Requirements...)
		combined.Matches = append(combined.Matches, result.Matches...)
		combined.Warning = result.Warning
	}
	if combined.Decision == HookBlock || combined.Decision == HookRequire {
		combined.BlockedBy = combined.blockingHook()
	}
	return combined
}

func completionGuidance(requirement string, attempt int, evaluation HookResult) service.WorkflowCompletionGuidance {
	guidance := service.WorkflowCompletionGuidance{
		Code:         "workflow_gate_rejected",
		Requirement:  requirement,
		Alternatives: completionAlternatives(),
		Attempt:      attempt,
	}
	if blocking := evaluation.blockingHook(); blocking != nil {
		guidance.HookID = blocking.ID
		guidance.HookName = blocking.Name
	}
	return guidance
}

func completionAttempt(result []byte) int {
	var payload struct {
		Attempt int `json:"completion_attempt"`
	}
	if json.Unmarshal(result, &payload) == nil && payload.Attempt > 1 {
		return payload.Attempt
	}
	return 1
}

func completionAlternatives() []string {
	return []string{
		"Create a wakeup.",
		"Ask or notify a member.",
		"Dispatch an agent or squad.",
		"Create a handoff.",
		"Set the issue to blocked.",
	}
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

func withCompletionWarning(result []byte, warning service.WorkflowCompletionGuidance) ([]byte, error) {
	payload := map[string]any{}
	if len(result) > 0 {
		if err := json.Unmarshal(result, &payload); err != nil {
			return result, err
		}
	}
	payload["completion_warning"] = warning
	return json.Marshal(payload)
}
