package workflows

import (
	"context"
	"encoding/json"
	"fmt"
	"strconv"

	"github.com/jackc/pgx/v5/pgtype"
	"github.com/multica-ai/multica/server/internal/service"
	db "github.com/multica-ai/multica/server/pkg/db/generated"
)

type TaskFailureContextStore interface {
	WorkflowHooksEnabledForTask(context.Context, pgtype.UUID) (bool, error)
	LoadTaskFailureContext(context.Context, pgtype.UUID) (TaskCompletionContext, error)
}

type TaskFailureGate struct {
	store          TaskFailureContextStore
	evaluator      HookEvaluator
	featureEnabled bool
}

func NewTaskFailureGate(store TaskFailureContextStore, evaluator HookEvaluator, serverFeatureEnabled ...bool) *TaskFailureGate {
	enabled := true
	if len(serverFeatureEnabled) > 0 {
		enabled = serverFeatureEnabled[0]
	}
	return &TaskFailureGate{store: store, evaluator: evaluator, featureEnabled: enabled}
}

func (g *TaskFailureGate) EvaluateTaskFailure(ctx context.Context, task db.AgentTaskQueue, reason, message string) (service.TaskFailureDecision, error) {
	if !g.featureEnabled || g.store == nil || g.evaluator == nil {
		return service.TaskFailureDecision{}, nil
	}
	enabled, err := g.store.WorkflowHooksEnabledForTask(ctx, task.ID)
	if err != nil || !enabled {
		return service.TaskFailureDecision{}, err
	}
	failureContext, err := g.store.LoadTaskFailureContext(ctx, task.ID)
	if err != nil {
		return service.TaskFailureDecision{}, err
	}
	result, err := g.evaluator.Evaluate(ctx, HookEvent{
		EventID:     fmt.Sprintf("%s:task-failure:%s:%d", failureContext.TaskID, reason, task.Attempt),
		Type:        HookOnTaskFailure,
		WorkspaceID: failureContext.WorkspaceID, ProjectID: failureContext.ProjectID,
		WorkflowID: failureContext.WorkflowID, AgentID: failureContext.AgentID,
		Model: failureContext.Model, IssueID: failureContext.IssueID,
		SessionID: failureContext.SessionID, Attempt: int(task.Attempt),
		Context: map[string]any{
			"failure": map[string]any{
				"reason": reason, "message": message,
				"attempt": task.Attempt, "max_attempts": task.MaxAttempts,
			},
			"task": map[string]any{"id": failureContext.TaskID, "status": task.Status},
		},
		MutableFields: []string{"failure_action", "fresh_session", "retry_limit", "user_message"},
	})
	if err != nil {
		return service.TaskFailureDecision{}, err
	}
	decision := service.TaskFailureDecision{
		Action:    service.TaskFailureSurface,
		Evaluated: result.Evaluated,
	}
	if action, ok := result.Modifications["failure_action"].(string); ok {
		switch service.TaskFailureAction(action) {
		case service.TaskFailureRetry, service.TaskFailurePause, service.TaskFailureAlert, service.TaskFailureSurface:
			decision.Action = service.TaskFailureAction(action)
		}
	}
	if fresh, ok := result.Modifications["fresh_session"].(bool); ok {
		decision.FreshSession = fresh
	}
	if retryLimit, ok := integerModification(result.Modifications["retry_limit"]); ok {
		decision.RetryLimit = int32(retryLimit)
	}
	if userMessage, ok := result.Modifications["user_message"].(string); ok {
		decision.UserMessage = userMessage
	}
	return decision, nil
}

func integerModification(value any) (int64, bool) {
	switch typed := value.(type) {
	case int:
		return int64(typed), true
	case int32:
		return int64(typed), true
	case int64:
		return typed, true
	case float64:
		return int64(typed), typed == float64(int64(typed))
	case json.Number:
		parsed, err := typed.Int64()
		return parsed, err == nil
	case string:
		parsed, err := strconv.ParseInt(typed, 10, 32)
		return parsed, err == nil
	default:
		return 0, false
	}
}

var _ service.WorkflowFailureGate = (*TaskFailureGate)(nil)
