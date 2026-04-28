package service

import (
	"context"
	"fmt"
	"log/slog"
	"math/rand"
	"time"

	"github.com/jackc/pgx/v5/pgtype"
	"github.com/multica-ai/multica/server/internal/events"
	"github.com/multica-ai/multica/server/internal/metrics"
	"github.com/multica-ai/multica/server/internal/util"
	"github.com/multica-ai/multica/server/pkg/agent"
	db "github.com/multica-ai/multica/server/pkg/db/generated"
)

const (
	// MaxRetryAttempts is the global retry budget across all transient failure categories.
	MaxRetryAttempts = 3
	maxBackoff       = 60 * time.Second
)

// FailureReason values consumed and produced by the retry executor.
const (
	FailureReasonRetryExhausted = "retry_exhausted"
	FailureReasonPermanentError = "permanent_error"
	FailureReasonInfraFailure   = "infra_failure"
	FailureReasonAgentError     = "agent_error"
	FailureReasonTimeout        = "timeout"
	FailureReasonRuntimeOffline = "runtime_offline"
	FailureReasonRuntimeRecover = "runtime_recovery"
)

// retryableReasons enumerates failure reasons that the unified retry executor
// is allowed to act on. Agent-side errors (compile failures, model rejections,
// etc.) are intentionally excluded — those are real problems that the user
// should see, not infrastructure flakiness.
var retryableReasons = map[string]bool{
	FailureReasonTimeout:        true,
	FailureReasonRuntimeOffline: true,
	FailureReasonRuntimeRecover: true,
	FailureReasonInfraFailure:   true,
}

// RetryExecutor implements the unified retry policy with exponential backoff
// and jitter. It subsumes the legacy MaybeRetryFailedTask path.
type RetryExecutor struct {
	Queries *db.Queries
	Bus     *events.Bus
	Metrics *metrics.RetryMetrics
	Enabled bool
}

// NewRetryExecutor creates a RetryExecutor. enabled gates the entire wrapper
// (when false, the legacy fast path is used).
func NewRetryExecutor(q *db.Queries, bus *events.Bus, m *metrics.RetryMetrics, enabled bool) *RetryExecutor {
	return &RetryExecutor{Queries: q, Bus: bus, Metrics: m, Enabled: enabled}
}

// MaybeRetry evaluates a failed task and enqueues a retry when the failure is
// transient and the global retry budget has not been exhausted. When retries
// are exhausted it posts a dead-letter comment to the parent issue and
// mentions the workspace owner.
//
// retryAfter is an optional duration hint from the provider (e.g. parsed from
// a Retry-After header). It is only meaningful for rate-limited failures.
func (r *RetryExecutor) MaybeRetry(ctx context.Context, parent db.AgentTaskQueue, failureReason string, retryAfter time.Duration) (*db.AgentTaskQueue, error) {
	if !r.Enabled {
		return r.legacyRetry(ctx, parent)
	}

	if parent.Status != "failed" {
		return nil, nil
	}

	if parent.AutopilotRunID.Valid {
		// Autopilot has its own retry semantics; do not double-trigger.
		return nil, nil
	}
	if !parent.IssueID.Valid && !parent.ChatSessionID.Valid {
		return nil, nil
	}

	reason := failureReason
	if reason == "" {
		reason = FailureReasonAgentError
	}

	// Classify the failure.
	if !r.isRetryable(reason) {
		slog.Info("retry skipped: non-retryable failure reason",
			"task_id", util.UUIDToString(parent.ID),
			"reason", reason,
		)
		return nil, nil
	}

	// Budget check: max 3 retries total (attempt can reach 4).
	if parent.Attempt >= MaxRetryAttempts {
		return r.handleExhaustion(ctx, parent, reason)
	}

	// Compute backoff.
	delay := r.computeDelay(int(parent.Attempt), retryAfter)
	scheduledAt := time.Now().UTC().Add(delay)

	child, err := r.Queries.CreateRetryTask(ctx, db.CreateRetryTaskParams{
		ID:          parent.ID,
		ScheduledAt: pgtype.Timestamptz{Time: scheduledAt, Valid: true},
	})
	if err != nil {
		slog.Warn("retry enqueue failed",
			"parent_task_id", util.UUIDToString(parent.ID),
			"reason", reason,
			"error", err,
		)
		return nil, err
	}

	slog.Info("task retry enqueued",
		"parent_task_id", util.UUIDToString(parent.ID),
		"child_task_id", util.UUIDToString(child.ID),
		"reason", reason,
		"attempt", child.Attempt,
		"delay_ms", delay.Milliseconds(),
	)

	if r.Metrics != nil {
		r.Metrics.RecordAttempt(reason)
	}

	return &child, nil
}

// legacyRetry preserves the pre-unified-executor behaviour for callers that
// have the feature flag disabled.
func (r *RetryExecutor) legacyRetry(ctx context.Context, parent db.AgentTaskQueue) (*db.AgentTaskQueue, error) {
	if parent.Status != "failed" {
		return nil, nil
	}
	reason := ""
	if parent.FailureReason.Valid {
		reason = parent.FailureReason.String
	}
	if !retryableReasons[reason] {
		return nil, nil
	}
	if parent.Attempt >= parent.MaxAttempts {
		slog.Info("task auto-retry skipped: budget exhausted",
			"task_id", util.UUIDToString(parent.ID),
			"attempt", parent.Attempt,
			"max_attempts", parent.MaxAttempts,
		)
		return nil, nil
	}
	if parent.AutopilotRunID.Valid {
		return nil, nil
	}
	if !parent.IssueID.Valid && !parent.ChatSessionID.Valid {
		return nil, nil
	}

	child, err := r.Queries.CreateRetryTask(ctx, db.CreateRetryTaskParams{
		ID:          parent.ID,
		ScheduledAt: pgtype.Timestamptz{Time: time.Now().UTC(), Valid: true},
	})
	if err != nil {
		slog.Warn("task auto-retry failed",
			"parent_task_id", util.UUIDToString(parent.ID),
			"reason", reason,
			"error", err,
		)
		return nil, err
	}
	slog.Info("task auto-retry enqueued",
		"parent_task_id", util.UUIDToString(parent.ID),
		"child_task_id", util.UUIDToString(child.ID),
		"reason", reason,
		"attempt", child.Attempt,
		"max_attempts", child.MaxAttempts,
	)
	return &child, nil
}

// isRetryable reports whether a failure reason is eligible for automatic retry.
func (r *RetryExecutor) isRetryable(reason string) bool {
	// Legacy infra reasons are always retryable.
	if retryableReasons[reason] {
		return true
	}
	// Provider-level transient errors are retryable when the executor is enabled.
	switch reason {
	case string(agent.ErrRateLimited),
		string(agent.ErrServiceUnavailable),
		string(agent.ErrGatewayError),
		string(agent.ErrTimeout):
		return true
	}
	return false
}

// computeDelay calculates the backoff for a retry attempt.
// delay = max(retryAfter, jittered_backoff)
// jittered_backoff = random(0, min(60s, 1s * 2^attempt))
func (r *RetryExecutor) computeDelay(attempt int, retryAfter time.Duration) time.Duration {
	capDuration := maxBackoff
	computed := time.Duration(1<<attempt) * time.Second
	if computed > capDuration {
		computed = capDuration
	}
	jitter := time.Duration(rand.Int63n(int64(computed)))

	delay := jitter
	if retryAfter > delay {
		delay = retryAfter
	}
	return delay
}

// handleExhaustion posts a dead-letter comment to the parent issue when retries
// are exhausted, mentions the workspace owner, and returns nil so the caller
// knows no retry was created.
func (r *RetryExecutor) handleExhaustion(ctx context.Context, parent db.AgentTaskQueue, reason string) (*db.AgentTaskQueue, error) {
	slog.Info("task retry exhausted",
		"task_id", util.UUIDToString(parent.ID),
		"attempt", parent.Attempt,
		"reason", reason,
	)

	if r.Metrics != nil {
		r.Metrics.RecordAttempt(FailureReasonRetryExhausted)
	}

	if !parent.IssueID.Valid {
		return nil, nil
	}

	// Update the parent task's failure_reason so callers know it was exhausted.
	// We do a best-effort update; if it fails we still post the comment.
	if err := r.Queries.UpdateTaskFailureReason(ctx, db.UpdateTaskFailureReasonParams{
		ID:            parent.ID,
		FailureReason: pgtype.Text{String: FailureReasonRetryExhausted, Valid: true},
	}); err != nil {
		slog.Warn("retry exhaustion: failed to update failure_reason",
			"task_id", util.UUIDToString(parent.ID),
			"error", err,
		)
	}

	issue, err := r.Queries.GetIssue(ctx, parent.IssueID)
	if err != nil {
		slog.Warn("retry exhaustion: could not load issue for dead-letter comment",
			"issue_id", util.UUIDToString(parent.IssueID),
			"error", err,
		)
		return nil, nil
	}

	// Find the first workspace owner by created_at ASC.
	ownerMention := ""
	members, err := r.Queries.ListMembersWithUser(ctx, issue.WorkspaceID)
	if err != nil {
		slog.Warn("retry exhaustion: could not list workspace members",
			"workspace_id", util.UUIDToString(issue.WorkspaceID),
			"error", err,
		)
	} else {
		for _, m := range members {
			if m.Role == "owner" {
				ownerMention = fmt.Sprintf("[@%s](mention://member/%s)", m.UserName, util.UUIDToString(m.UserID))
				break
			}
		}
	}

	content := fmt.Sprintf(
		"Task retry attempts exhausted (attempt %d, failure reason: `%s`). Please investigate.",
		parent.Attempt, reason,
	)
	if ownerMention != "" {
		content += " " + ownerMention
	}

	comment, err := r.Queries.CreateComment(ctx, db.CreateCommentParams{
		IssueID:     parent.IssueID,
		WorkspaceID: issue.WorkspaceID,
		AuthorType:  "agent",
		AuthorID:    parent.AgentID,
		Content:     content,
		Type:        "system",
		ParentID:    parent.TriggerCommentID,
	})
	if err != nil {
		slog.Warn("retry exhaustion: failed to post dead-letter comment",
			"issue_id", util.UUIDToString(parent.IssueID),
			"error", err,
		)
		return nil, nil
	}

	if r.Bus != nil {
		r.Bus.Publish(events.Event{
			Type:        "comment:created",
			WorkspaceID: util.UUIDToString(issue.WorkspaceID),
			ActorType:   "agent",
			ActorID:     util.UUIDToString(parent.AgentID),
			Payload: map[string]any{
				"comment": map[string]any{
					"id":          util.UUIDToString(comment.ID),
					"issue_id":    util.UUIDToString(comment.IssueID),
					"author_type": comment.AuthorType,
					"author_id":   util.UUIDToString(comment.AuthorID),
					"content":     comment.Content,
					"type":        comment.Type,
					"parent_id":   util.UUIDToPtr(comment.ParentID),
					"created_at":  comment.CreatedAt.Time.Format(time.RFC3339),
				},
				"issue_title":  issue.Title,
				"issue_status": issue.Status,
			},
		})
	}

	return nil, nil
}
