package workflows

import (
	"context"
	"log/slog"

	"github.com/jackc/pgx/v5/pgtype"

	"github.com/multica-ai/multica/server/internal/events"
	"github.com/multica-ai/multica/server/internal/util"
	"github.com/multica-ai/multica/server/pkg/protocol"
)

// TaskFailureContext is everything the on.task.failure hook event needs about
// a freshly-failed task. RetryPending distinguishes "a retry child is already
// queued and will report on its own" from "this failure is terminal" — the
// default guard hook only comments/blocks in the terminal case.
type TaskFailureContext struct {
	HooksEnabled  bool
	TaskID        string
	WorkspaceID   string
	ProjectID     string
	IssueID       string
	IssueStatus   string
	AgentID       string
	Model         string
	SessionID     string
	FailureReason string
	ErrorText     string
	Attempt       int
	MaxAttempts   int
	RetryPending  bool
}

type TaskFailureContextStore interface {
	LoadTaskFailureContext(context.Context, pgtype.UUID) (TaskFailureContext, error)
}

// TaskFailureGate connects the in-process task:failed broadcast to the
// on.task.failure hook event. Until this gate existed the event type was
// listed in the catalog but nothing ever fired it.
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

// Attach subscribes the gate on the bus. Safe to call once at server startup.
func (g *TaskFailureGate) Attach(bus *events.Bus) {
	bus.Subscribe(protocol.EventTaskFailed, g.onTaskFailed)
}

func (g *TaskFailureGate) onTaskFailed(e events.Event) {
	if !g.featureEnabled || g.store == nil || g.evaluator == nil {
		return
	}
	payload, ok := payloadToMap(e.Payload)
	if !ok {
		return
	}
	// Cancellations are re-broadcast on the task:failed channel so frontends
	// clear the live card; a user-cancelled run is not a failure.
	if status, _ := payload["status"].(string); status != "failed" {
		return
	}
	taskIDRaw, _ := payload["task_id"].(string)
	taskID, err := util.ParseUUID(taskIDRaw)
	if err != nil {
		return
	}
	ctx := context.Background()
	failure, err := g.store.LoadTaskFailureContext(ctx, taskID)
	if err != nil {
		// Chat-session tasks have no issue row and resolve to no context;
		// the guard hook is issue-scoped by design.
		return
	}
	if !failure.HooksEnabled || failure.IssueID == "" {
		return
	}
	event := HookEvent{
		EventID:     "task-failure:" + failure.TaskID,
		Type:        HookOnTaskFailure,
		WorkspaceID: failure.WorkspaceID,
		ProjectID:   failure.ProjectID,
		AgentID:     failure.AgentID,
		Model:       failure.Model,
		IssueID:     failure.IssueID,
		SessionID:   failure.SessionID,
		Attempt:     failure.Attempt,
		Context: map[string]any{
			"task": map[string]any{
				"id":             failure.TaskID,
				"failure_reason": failure.FailureReason,
				"error":          failure.ErrorText,
				"attempt":        failure.Attempt,
				"max_attempts":   failure.MaxAttempts,
			},
			"retry": map[string]any{"pending": failure.RetryPending},
			"issue": map[string]any{"id": failure.IssueID, "status": failure.IssueStatus},
		},
	}
	if _, err := g.evaluator.Evaluate(ctx, event); err != nil {
		slog.Warn("task failure hook evaluation failed",
			"task_id", failure.TaskID,
			"issue_id", failure.IssueID,
			"workspace_id", failure.WorkspaceID,
			"error", err,
		)
	}
}
