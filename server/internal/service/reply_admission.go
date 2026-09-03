package service

import (
	"context"
	"encoding/json"
	"fmt"
	"time"

	"github.com/jackc/pgx/v5/pgtype"
	obsmetrics "github.com/multica-ai/multica/server/internal/metrics"
	"github.com/multica-ai/multica/server/internal/replyadmission"
	"github.com/multica-ai/multica/server/internal/util"
	db "github.com/multica-ai/multica/server/pkg/db/generated"
	"github.com/multica-ai/multica/server/pkg/protocol"
	"github.com/multica-ai/multica/server/pkg/redact"
)

type completionFallbackAdmission struct {
	Content string
	Issue   db.Issue
	Parent  *db.Comment
}

// prepareCompletionFallbackAdmission checks the exact comment that
// CompleteTask would synthesize and returns the locked parent when one is
// needed. The caller must insert the returned comment using the same qtx before
// committing the task status change; otherwise a parent edit between preflight
// and insertion could leave a completed task with its substantive output
// silently absent.
func (s *TaskService) prepareCompletionFallbackAdmission(ctx context.Context, q *db.Queries, taskID pgtype.UUID, result []byte) (*completionFallbackAdmission, error) {
	task, err := q.GetAgentTask(ctx, taskID)
	if err != nil {
		return nil, err
	}
	if !task.IssueID.Valid {
		return nil, nil
	}

	suppressNoAction, err := HasSquadLeaderNoActionEvaluationForTask(ctx, q, task)
	if err != nil {
		return nil, fmt.Errorf("check no_action evaluation: %w", err)
	}
	if suppressNoAction {
		return nil, nil
	}
	issue, err := q.GetIssue(ctx, task.IssueID)
	if err != nil {
		return nil, fmt.Errorf("load completion fallback issue: %w", err)
	}
	lockedIssue, err := q.LockIssueForDescriptionUpdate(ctx, db.LockIssueForDescriptionUpdateParams{
		ID:          issue.ID,
		WorkspaceID: issue.WorkspaceID,
	})
	if err != nil {
		return nil, fmt.Errorf("lock completion fallback issue: %w", err)
	}
	agentCommented, err := q.HasAgentCommentedSince(ctx, db.HasAgentCommentedSinceParams{
		IssueID:  task.IssueID,
		AuthorID: task.AgentID,
		Since:    task.StartedAt,
	})
	if err != nil {
		return nil, fmt.Errorf("check existing agent comment: %w", err)
	}
	if agentCommented {
		return nil, nil
	}

	var payload protocol.TaskCompletedPayload
	if err := json.Unmarshal(result, &payload); err != nil || payload.Output == "" {
		return nil, nil
	}
	body := util.UnescapeBackslashEscapes(payload.Output)
	if task.TriggerCommentID.Valid && isTrivialDoneOutput(body) {
		return nil, nil
	}
	content := truncateFallbackCommentBody(redact.Text(body), maxSynthesizedFallbackCommentRunes)
	if !task.TriggerCommentID.Valid {
		// Assignment-triggered tasks retain the historical top-level fallback.
		// There is no request parent to admit, but the row still belongs in the
		// same transaction as completion so the delivery invariant is atomic.
		return &completionFallbackAdmission{Content: content, Issue: lockedIssue}, nil
	}
	// Lock the exact request row. The lock is held through the guarded insert
	// in CompleteTask's transaction, serializing a parent edit against this
	// admission decision.
	parent, err := q.GetCommentForUpdate(ctx, task.TriggerCommentID)
	if err != nil {
		return nil, fmt.Errorf("load completion fallback parent: %w", err)
	}
	if util.UUIDToString(parent.IssueID) != util.UUIDToString(lockedIssue.ID) || util.UUIDToString(parent.WorkspaceID) != util.UUIDToString(lockedIssue.WorkspaceID) {
		return nil, fmt.Errorf("completion fallback parent issue mismatch")
	}
	started := time.Now()
	decision := replyadmission.Evaluate(replyadmission.Parent{
		ID:          util.UUIDToString(parent.ID),
		IssueID:     util.UUIDToString(parent.IssueID),
		WorkspaceID: util.UUIDToString(parent.WorkspaceID),
		AuthorType:  parent.AuthorType,
		AuthorID:    util.UUIDToString(parent.AuthorID),
		Content:     parent.Content,
		IsReply:     parent.ParentID.Valid,
	}, content)
	s.Metrics.RecordReplyAdmission(obsmetrics.ReplyAdmissionPathTaskCompletion, decision.Outcome(), decision.Reason, time.Since(started))
	if !decision.Admitted {
		return nil, fmt.Errorf("completion fallback reply admission: %w", &replyadmission.MissingRequesterMentionError{RequesterID: decision.RequesterID})
	}
	return &completionFallbackAdmission{Content: content, Issue: lockedIssue, Parent: &parent}, nil
}
