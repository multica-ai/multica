package service

import (
	"context"
	"log/slog"
	"strings"

	"github.com/jackc/pgx/v5/pgtype"
	"github.com/multica-ai/multica/server/internal/events"
	"github.com/multica-ai/multica/server/internal/util"
	db "github.com/multica-ai/multica/server/pkg/db/generated"
	"github.com/multica-ai/multica/server/pkg/protocol"
)

// FailureClass classifies a task failure to decide retry behavior.
type FailureClass int

const (
	// FailurePermanent — bug, cancellation, hard agent error. No retry.
	FailurePermanent FailureClass = iota
	// FailureTransient — runtime crash, timeout, network. Retry immediately.
	FailureTransient
	// FailureRateLimited — provider 429 or quota error. Retry (with backoff
	// once v2 lands; for v1 the anti-storm cap in CreateRetryTask is the
	// only governor).
	FailureRateLimited
)

func (c FailureClass) String() string {
	switch c {
	case FailureTransient:
		return "transient"
	case FailureRateLimited:
		return "rate_limited"
	default:
		return "permanent"
	}
}

// ClassifyFailure decides retry policy from the structured failure_reason
// (when known) and the free-form error message (always present).
//
// Priority order:
//  1. Structured reason wins when set — it's set by code that knows what
//     happened (sweeper, daemon callback).
//  2. Otherwise fall back to substring matching on the error string for
//     well-known transient/rate-limit signatures.
//  3. Default = permanent (don't retry on unknown errors).
func ClassifyFailure(reason, errMsg string) FailureClass {
	switch reason {
	case "runtime_offline", "timed_out", "task timed out", "runtime went offline":
		return FailureTransient
	case "rate_limited":
		return FailureRateLimited
	case "agent_error", "permanent":
		// fall through to string analysis — agent_error is a catch-all the
		// daemon writes, so we still inspect the error message for 429.
	}

	low := strings.ToLower(errMsg)
	switch {
	case strings.Contains(low, "rate limit"),
		strings.Contains(low, "rate_limit"),
		strings.Contains(low, "ratelimit"),
		strings.Contains(low, "429"),
		strings.Contains(low, "quota"),
		strings.Contains(low, "too many requests"):
		return FailureRateLimited
	case strings.Contains(low, "runtime went offline"),
		strings.Contains(low, "task timed out"),
		strings.Contains(low, "context deadline exceeded"),
		strings.Contains(low, "connection reset"),
		strings.Contains(low, "broken pipe"),
		strings.Contains(low, "i/o timeout"):
		return FailureTransient
	}

	return FailurePermanent
}

// HandleTaskFailure is the single entry point invoked whenever a task
// transitions to 'failed'. It:
//  1. Broadcasts task:failed so live UIs update.
//  2. Schedules a retry when the failure class warrants it AND the retry
//     guard in CreateRetryTask permits it (attempt < max_attempts AND no
//     anti-storm cap hit).
//  3. If no retry was scheduled and the issue is stuck in 'in_progress'
//     with no other active task, resets it to 'todo' so a manual or
//     autopilot re-trigger can pick it up. Mirrors the previous
//     sweeper-side behavior.
//  4. Reconciles agent status (idle/working).
//
// Returns the retry task when one was scheduled, otherwise nil.
//
// Both [TaskService.FailTask] and the runtime sweeper call this so the
// retry decision lives in exactly one place.
func (s *TaskService) HandleTaskFailure(ctx context.Context, task db.AgentTaskQueue, errMsg string) *db.AgentTaskQueue {
	// 1. Broadcast the failure.
	s.broadcastTaskEvent(ctx, protocol.EventTaskFailed, task)

	// 2. Decide retry.
	reason := ""
	if task.FailureReason.Valid {
		reason = task.FailureReason.String
	}
	class := ClassifyFailure(reason, errMsg)

	var retry *db.AgentTaskQueue
	if class != FailurePermanent && task.Attempt < task.MaxAttempts {
		row, err := s.Queries.CreateRetryTask(ctx, task.ID)
		switch {
		case err == nil:
			retry = &row
			slog.Info("task retry scheduled",
				"parent_task_id", util.UUIDToString(task.ID),
				"retry_task_id", util.UUIDToString(row.ID),
				"attempt", row.Attempt,
				"max_attempts", row.MaxAttempts,
				"class", class.String(),
				"agent_id", util.UUIDToString(row.AgentID),
				"issue_id", util.UUIDToString(row.IssueID),
			)
			s.broadcastRetryScheduled(ctx, task, row, class)
		default:
			// Guard blocked the retry (cap hit, attempt == max_attempts, or
			// parent already gone). Treat as no-retry rather than failing
			// loud — the workspace gets a `task:failed` and stays there.
			slog.Info("task retry skipped",
				"parent_task_id", util.UUIDToString(task.ID),
				"class", class.String(),
				"reason", err.Error(),
			)
		}
	} else {
		slog.Debug("task retry skipped (permanent or out of attempts)",
			"parent_task_id", util.UUIDToString(task.ID),
			"attempt", task.Attempt,
			"max_attempts", task.MaxAttempts,
			"class", class.String(),
		)
	}

	// 3. Reset stuck issue when no retry will pick it up.
	if retry == nil && task.IssueID.Valid {
		s.resetStuckIssue(ctx, task.IssueID)
	}

	// 4. Reconcile agent status.
	s.ReconcileAgentStatus(ctx, task.AgentID)

	return retry
}

// resetStuckIssue flips an issue back to 'todo' when its task failed with
// no retry scheduled AND nothing else is actively working on it. This
// prevents the issue from sitting in 'in_progress' forever.
func (s *TaskService) resetStuckIssue(ctx context.Context, issueID pgtype.UUID) {
	issue, err := s.Queries.GetIssue(ctx, issueID)
	if err != nil || issue.Status != "in_progress" {
		return
	}
	hasActive, err := s.Queries.HasActiveTaskForIssue(ctx, issueID)
	if err != nil || hasActive {
		return
	}
	if _, err := s.Queries.UpdateIssueStatus(ctx, db.UpdateIssueStatusParams{
		ID:     issueID,
		Status: "todo",
	}); err != nil {
		slog.Warn("reset stuck issue to todo failed",
			"issue_id", util.UUIDToString(issueID),
			"error", err,
		)
	}
}

// broadcastRetryScheduled emits task:retry_scheduled so UIs can show
// "Retrying… (2/3)" instead of a flat "Failed".
func (s *TaskService) broadcastRetryScheduled(ctx context.Context, parent, retry db.AgentTaskQueue, class FailureClass) {
	workspaceID := s.ResolveTaskWorkspaceID(ctx, retry)
	if workspaceID == "" {
		return
	}
	s.Bus.Publish(events.Event{
		Type:        protocol.EventTaskRetryScheduled,
		WorkspaceID: workspaceID,
		ActorType:   "system",
		Payload: map[string]any{
			"parent_task_id": util.UUIDToString(parent.ID),
			"retry_task_id":  util.UUIDToString(retry.ID),
			"agent_id":       util.UUIDToString(retry.AgentID),
			"issue_id":       util.UUIDToString(retry.IssueID),
			"attempt":        retry.Attempt,
			"max_attempts":   retry.MaxAttempts,
			"class":          class.String(),
		},
	})
}
