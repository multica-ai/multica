package service

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"strings"

	"github.com/jackc/pgx/v5/pgtype"
	"github.com/multica-ai/multica/server/internal/events"
	"github.com/multica-ai/multica/server/internal/util"
	db "github.com/multica-ai/multica/server/pkg/db/generated"
	"github.com/multica-ai/multica/server/pkg/protocol"
	"github.com/multica-ai/multica/server/pkg/redact"
)

// escalateTaskFailure is the shared escalation entry point for a single
// failed task. It is safe to call multiple times for the same task —
// metadata-level dedup prevents duplicate comments and dispatches.
//
// Must only be called after the auto-retry decision has settled (i.e.
// retried == nil), otherwise a transient failure that will be retried
// would incorrectly trigger escalation.
func (s *TaskService) escalateTaskFailure(ctx context.Context, task db.AgentTaskQueue) {
	if !task.IssueID.Valid {
		// Chat-only and quick-create tasks do not get issue escalation.
		return
	}

	issue, err := s.Queries.GetIssue(ctx, task.IssueID)
	if err != nil {
		slog.Warn("escalation: failed to load issue",
			"task_id", util.UUIDToString(task.ID),
			"issue_id", util.UUIDToString(task.IssueID),
			"error", err,
		)
		return
	}

	// Skip done / cancelled issues — no point escalating historical failures.
	if issue.Status == "done" || issue.Status == "cancelled" {
		return
	}

	failureReason := taskFailureReason(task)

	// --- Dedup: check metadata for prior escalation of the same task ---
	meta := parseIssueMetadata(issue.Metadata)
	if priorTaskID, ok := meta["failure_escalated_task_id"].(string); ok && priorTaskID == util.UUIDToString(task.ID) {
		slog.Debug("escalation skipped: already escalated for this task",
			"task_id", util.UUIDToString(task.ID),
			"issue_id", util.UUIDToString(task.IssueID),
		)
		return
	}

	// --- Suppress escalation when a later completed task exists ---
	// A failure that was overtaken by a successful task (e.g. the agent
	// produced valid output before an idle_watchdog killed the runner)
	// should not produce an escalation comment.
	if s.hasCompletedTaskAfter(ctx, task) {
		slog.Debug("escalation skipped: later completed task exists",
			"task_id", util.UUIDToString(task.ID),
			"issue_id", util.UUIDToString(task.IssueID),
		)
		return
	}

	// --- Build escalation comment ---
	errSummary := ""
	if task.Error.Valid && task.Error.String != "" {
		errSummary = redact.Text(task.Error.String)
	}
	commentBody := s.buildEscalationComment(task, issue, failureReason, errSummary)

	// Sanitise user-controlled fields (issue title, error text) against
	// mention injection so a malicious title or error cannot trigger
	// side-effect agent mentions.
	commentBody = stripMentionInjections(commentBody)

	// --- Create system comment ---
	// Use author_type="system" with a zero UUID so the comment is not
	// attributed to any agent. This matches the pattern established in
	// issue_child_done.go / migration 107.
	comment, err := s.Queries.CreateComment(ctx, db.CreateCommentParams{
		IssueID:     issue.ID,
		WorkspaceID: issue.WorkspaceID,
		AuthorType:  "system",
		AuthorID:    pgtype.UUID{Valid: true}, // zero UUID
		Content:     commentBody,
		Type:        "system",
		ParentID:    pgtype.UUID{}, // top-level
	})
	if err != nil {
		slog.Warn("escalation: create system comment failed",
			"task_id", util.UUIDToString(task.ID),
			"issue_id", util.UUIDToString(task.IssueID),
			"error", err,
		)
		return
	}

	// --- Write escalation metadata for dedup ---
	metaPayload := map[string]string{
		"failure_escalated_task_id":     util.UUIDToString(task.ID),
		"failure_escalated_reason":      failureReason,
		"failure_escalated_agent_id":    util.UUIDToString(task.AgentID),
		"failure_escalation_comment_id": util.UUIDToString(comment.ID),
		"failure_escalation_status":     "notified_leader",
	}
	for k, v := range metaPayload {
		valBytes, err := json.Marshal(v)
		if err != nil {
			slog.Warn("escalation: marshal metadata value failed",
				"key", k, "error", err,
			)
			continue
		}
		if _, err := s.Queries.SetIssueMetadataKey(ctx, db.SetIssueMetadataKeyParams{
			ID:          issue.ID,
			WorkspaceID: issue.WorkspaceID,
			Key:         k,
			Value:       valBytes,
		}); err != nil {
			slog.Warn("escalation: set metadata key failed",
				"key", k, "error", err,
			)
		}
	}

	// --- Broadcast the comment event ---
	s.Bus.Publish(events.Event{
		Type:        protocol.EventCommentCreated,
		WorkspaceID: util.UUIDToString(issue.WorkspaceID),
		ActorType:   "system",
		ActorID:     "",
		Payload: map[string]any{
			"comment": map[string]any{
				"id":          util.UUIDToString(comment.ID),
				"issue_id":    util.UUIDToString(comment.IssueID),
				"author_type": comment.AuthorType,
				"author_id":   util.UUIDToString(comment.AuthorID),
				"content":     comment.Content,
				"type":        comment.Type,
				"parent_id":   util.UUIDToPtr(comment.ParentID),
				"created_at":  comment.CreatedAt.Time.Format("2006-01-02T15:04:05Z"),
			},
			"issue_title":  issue.Title,
			"issue_status": issue.Status,
		},
	})

	// --- Explicit dispatch to next handler ---
	s.dispatchEscalationTarget(ctx, issue, task, failureReason)

	slog.Info("task failure escalated",
		"task_id", util.UUIDToString(task.ID),
		"issue_id", util.UUIDToString(task.IssueID),
		"agent_id", util.UUIDToString(task.AgentID),
		"reason", failureReason,
		"comment_id", util.UUIDToString(comment.ID),
	)
}

// buildEscalationComment constructs the diagnostic comment body for a
// task failure escalation.
func (s *TaskService) buildEscalationComment(task db.AgentTaskQueue, issue db.Issue, failureReason, errSummary string) string {
	agentName := s.lookupAgentName(task.AgentID)
	taskID := util.UUIDToString(task.ID)
	agentID := util.UUIDToString(task.AgentID)
	route := recommendRoute(failureReason)
	nextHandler := recommendNextHandler(failureReason)

	var b strings.Builder
	b.WriteString("**Task Failure Escalation**\n\n")
	b.WriteString(fmt.Sprintf("- **Failed Agent**: %s (%s)\n", agentName, agentID))
	b.WriteString(fmt.Sprintf("- **Task ID**: %s\n", taskID))
	b.WriteString(fmt.Sprintf("- **Failure Reason**: `%s`\n", failureReason))
	b.WriteString(fmt.Sprintf("- **Issue Status**: %s\n", issue.Status))
	if task.Attempt > 0 {
		b.WriteString(fmt.Sprintf("- **Attempt**: %d / %d\n", task.Attempt, task.MaxAttempts))
	}
	if errSummary != "" {
		// Limit error summary to 500 characters so the comment stays readable.
		if len(errSummary) > 500 {
			errSummary = errSummary[:500] + "…"
		}
		b.WriteString(fmt.Sprintf("- **Error**: %s\n", errSummary))
	}
	b.WriteString(fmt.Sprintf("\n**Recommended Next Step**: %s\n", route))
	b.WriteString(fmt.Sprintf("**Suggested Handler**: %s\n", nextHandler))
	b.WriteString("\n> This is an automated escalation. No daemon restart was performed. ")
	b.WriteString("If daemon/runtime restart is needed, a human must explicitly approve it. ")
	b.WriteString("Do not @mention the failed agent back — their next run requires explicit rerun or reassignment.")

	return b.String()
}

// lookupAgentName returns the agent's name, or "unknown" if the lookup fails.
func (s *TaskService) lookupAgentName(agentID pgtype.UUID) string {
	if s.Queries == nil {
		return "unknown"
	}
	agent, err := s.Queries.GetAgent(context.Background(), agentID)
	if err != nil {
		return "unknown"
	}
	return agent.Name
}


// hasCompletedTaskAfter returns true when another task for the same issue
// completed after this one failed — the failure was overtaken by success
// and escalation should be suppressed.
func (s *TaskService) hasCompletedTaskAfter(ctx context.Context, task db.AgentTaskQueue) bool {
	if !task.IssueID.Valid || !task.CompletedAt.Valid {
		return false
	}
	count, err := s.Queries.CountCompletedTasksForIssueAfter(ctx, db.CountCompletedTasksForIssueAfterParams{
		IssueID:     task.IssueID,
		CompletedAt: task.CompletedAt,
	})
	if err != nil {
		slog.Warn("escalation: count completed tasks failed",
			"task_id", util.UUIDToString(task.ID),
			"error", err,
		)
		return false
	}
	return count > 0
}

// dispatchEscalationTarget enqueues a follow-up task for the escalation
// target based on the failure reason and issue assignee.
func (s *TaskService) dispatchEscalationTarget(ctx context.Context, issue db.Issue, task db.AgentTaskQueue, failureReason string) {
	// Determine target agent for the escalation follow-up.
	targetID, isLeader := s.resolveEscalationTarget(ctx, issue, task, failureReason)
	if targetID == nil {
		return
	}

	// Do not dispatch back to the agent that just failed — that would
	// create a no-op re-trigger loop.
	if util.UUIDToString(*targetID) == util.UUIDToString(task.AgentID) {
		slog.Debug("escalation dispatch skipped: target is the failed agent",
			"task_id", util.UUIDToString(task.ID),
			"agent_id", util.UUIDToString(task.AgentID),
		)
		return
	}

	if isLeader {
		if _, err := s.EnqueueTaskForSquadLeader(ctx, issue, *targetID, pgtype.UUID{}); err != nil {
			slog.Warn("escalation dispatch: enqueue squad leader failed",
				"task_id", util.UUIDToString(task.ID),
				"leader_id", util.UUIDToString(*targetID),
				"error", err,
			)
		}
	} else {
		if _, err := s.EnqueueTaskForMention(ctx, issue, *targetID, pgtype.UUID{}); err != nil {
			slog.Warn("escalation dispatch: enqueue mention task failed",
				"task_id", util.UUIDToString(task.ID),
				"agent_id", util.UUIDToString(*targetID),
				"error", err,
			)
		}
	}
}

// resolveEscalationTarget returns the agent ID and whether they should be
// dispatched as a squad leader, or nil if no suitable target is found.
func (s *TaskService) resolveEscalationTarget(ctx context.Context, issue db.Issue, task db.AgentTaskQueue, failureReason string) (_ *pgtype.UUID, isLeader bool) {
	// 1. If the issue is assigned to a squad, route to the squad leader.
	if issue.AssigneeType.Valid && issue.AssigneeType.String == "squad" && issue.AssigneeID.Valid {
		squad, err := s.Queries.GetSquad(ctx, issue.AssigneeID)
		if err == nil && squad.LeaderID.Valid {
			return &squad.LeaderID, true
		}
	}

	// 2. If the issue has a specific agent assignee (different from the
	//    failed agent), route to that assignee.
	if issue.AssigneeType.Valid && issue.AssigneeType.String == "agent" && issue.AssigneeID.Valid {
		if util.UUIDToString(issue.AssigneeID) != util.UUIDToString(task.AgentID) {
			return &issue.AssigneeID, false
		}
	}

	// 3. Fall back to the failed agent's owner if set.
	failedAgent, err := s.Queries.GetAgent(ctx, task.AgentID)
	if err == nil && failedAgent.OwnerID.Valid {
		ownerAgent, err := s.Queries.GetAgent(ctx, failedAgent.OwnerID)
		if err == nil && !ownerAgent.ArchivedAt.Valid && ownerAgent.RuntimeID.Valid {
			return &failedAgent.OwnerID, false
		}
	}

	return nil, false
}

// recommendRoute returns a human-readable recommendation for the next
// action based on the failure reason.
func recommendRoute(failureReason string) string {
	switch failureReason {
	case "idle_watchdog", "codex_semantic_inactivity":
		return "Check if valid results exist from previous attempts. If so, have QA/owner review. Otherwise, reassign to an alternative agent or use a more stable runtime."
	case "timeout":
		return "Auto-retry may have been attempted. If retries are exhausted, reassign to a different agent or break the task into smaller units."
	case "runtime_offline", "runtime_recovery", "queued_expired":
		return "Runtime infrastructure issue. Route to DevOps for investigation."
	case "provider_error":
		return "Provider/model error. Route to runtime QA / DevOps to check provider configuration, quotas, and model availability."
	case "permission_error", "sandbox_error":
		return "Permission or sandbox restriction. Route to DevOps for access review. Do not auto-elevate permissions."
	case "qa_fail", "needs_fix":
		return "QA failure or fix needed. Route to the implementation owner or parent issue owner."
	case "block":
		return "Task is blocked. Route to team lead for decision on issue status."
	case "cancelled":
		return "Task was cancelled (non-user action). Route to team lead for review."
	case "agent_error":
		fallthrough
	default:
		return "Unknown agent error. Route to Spark preflight or QA for log review."
	}
}

// recommendNextHandler returns a human-readable suggested handler role.
func recommendNextHandler(failureReason string) string {
	switch failureReason {
	case "idle_watchdog", "codex_semantic_inactivity":
		return "QA / Issue Owner"
	case "timeout":
		return "Team Lead"
	case "runtime_offline", "runtime_recovery", "queued_expired":
		return "DevOps"
	case "provider_error":
		return "Runtime QA / DevOps"
	case "permission_error", "sandbox_error":
		return "DevOps"
	case "qa_fail", "needs_fix":
		return "Implementation Owner"
	case "block":
		return "Team Lead"
	case "cancelled":
		return "Team Lead"
	default:
		return "Spark Preflight / QA"
	}
}

// stripMentionInjections removes mention://agent, mention://member, and
// mention://squad markdown links from a string to prevent side-effect
// agent mentions. mention://issue links are preserved.
func stripMentionInjections(s string) string {
	// Replace mention links for agent/member/squad types by inserting a space
	// before the closing parenthesis so the mention regex no longer matches.
	result := strings.ReplaceAll(s, "](mention://agent/", "] (mention://agent/")
	result = strings.ReplaceAll(result, "](mention://member/", "] (mention://member/")
	result = strings.ReplaceAll(result, "](mention://squad/", "] (mention://squad/")
	return result
}

// parseIssueMetadata decodes JSONB metadata bytes into a map.
func parseIssueMetadata(raw []byte) map[string]any {
	if len(raw) == 0 {
		return map[string]any{}
	}
	var out map[string]any
	if err := json.Unmarshal(raw, &out); err != nil || out == nil {
		return map[string]any{}
	}
	return out
}
