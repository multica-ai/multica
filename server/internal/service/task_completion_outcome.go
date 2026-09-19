package service

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/multica-ai/multica/server/internal/events"
	"github.com/multica-ai/multica/server/internal/util"
	db "github.com/multica-ai/multica/server/pkg/db/generated"
	"github.com/multica-ai/multica/server/pkg/dbid"
	"github.com/multica-ai/multica/server/pkg/protocol"
	"github.com/multica-ai/multica/server/pkg/redact"
)

const (
	issueNoResponseFallback          = "The agent completed this task without a text response."
	taskCompletionOutboxMaxAttempts  = 10
	taskCompletionOutboxLeaseSeconds = 60
)

type TaskCompletionOutboxDeliveryResult struct {
	Claimed   int
	Published int
	Failed    int
}

func completionAnsweredCommentIDs(task db.AgentTaskQueue) []pgtype.UUID {
	seen := make(map[string]struct{}, len(task.CoalescedCommentIds)+1)
	result := make([]pgtype.UUID, 0, len(task.CoalescedCommentIds)+1)
	appendID := func(id pgtype.UUID) {
		if !id.Valid {
			return
		}
		key := util.UUIDToString(id)
		if _, exists := seen[key]; exists {
			return
		}
		seen[key] = struct{}{}
		result = append(result, id)
	}
	appendID(task.TriggerCommentID)
	for _, id := range task.CoalescedCommentIds {
		appendID(id)
	}
	return result
}

func completionResultDigest(result []byte) string {
	digest := sha256.Sum256(result)
	return hex.EncodeToString(digest[:])
}

func issueCompletionContent(result []byte) (kind, content string) {
	var payload protocol.TaskCompletedPayload
	if err := json.Unmarshal(result, &payload); err != nil || payload.Output == "" {
		return "no_response", issueNoResponseFallback
	}
	body := util.UnescapeBackslashEscapes(payload.Output)
	body = truncateFallbackCommentBody(redact.Text(body), maxSynthesizedFallbackCommentRunes)
	if body == "" {
		return "no_response", issueNoResponseFallback
	}
	return "final", body
}

// writeIssueCompletionOutcome runs inside the same transaction as the task's
// running -> completed transition. Any failure rolls the transition back, so a
// daemon retry can safely persist the terminal result without re-running work.
func (s *TaskService) writeIssueCompletionOutcome(ctx context.Context, qtx *db.Queries, task db.AgentTaskQueue, result []byte) error {
	suppressed, err := HasSquadLeaderNoActionEvaluationForTask(ctx, qtx, task)
	if err != nil {
		return fmt.Errorf("resolve no-action outcome: %w", err)
	}
	issue, err := qtx.GetIssue(ctx, task.IssueID)
	if err != nil {
		return fmt.Errorf("load completion issue: %w", err)
	}
	answered := completionAnsweredCommentIDs(task)
	if suppressed {
		_, err = qtx.CreateAgentTaskCompletionOutcome(ctx, db.CreateAgentTaskCompletionOutcomeParams{
			TaskID: task.ID, WorkspaceID: issue.WorkspaceID, IssueID: task.IssueID,
			AgentID: task.AgentID, OutcomeKind: "suppressed", Content: "",
			ResultSha256: completionResultDigest(result), AnsweredCommentIds: answered,
		})
		if err != nil {
			return fmt.Errorf("persist suppressed completion outcome: %w", err)
		}
		return nil
	}

	finalComment, candidateErr := qtx.GetTaskFinalCommentCandidate(ctx, db.GetTaskFinalCommentCandidateParams{
		IssueID: task.IssueID, WorkspaceID: issue.WorkspaceID,
		AgentID: task.AgentID, SourceTaskID: task.ID,
	})
	outcomeKind := "final"
	content := finalComment.Content
	issueRevision := issue.Revision
	outboxState := "published"
	if candidateErr != nil {
		if !errors.Is(candidateErr, pgx.ErrNoRows) {
			return fmt.Errorf("load task final comment candidate: %w", candidateErr)
		}
		outcomeKind, content = issueCompletionContent(result)
		created, createErr := qtx.CreateComment(ctx, db.CreateCommentParams{
			ID: dbid.NewV7(), IssueID: task.IssueID, WorkspaceID: issue.WorkspaceID,
			AuthorType: "agent", AuthorID: task.AgentID, Content: content, Type: "comment",
			ParentID: task.TriggerCommentID, SourceTaskID: task.ID,
		})
		if createErr != nil {
			return fmt.Errorf("create final issue comment: %w", createErr)
		}
		finalComment = created.Comment()
		issueRevision = created.IssueRevision
		outboxState = "pending"
	} else if content == "" {
		outcomeKind = "no_response"
	}

	if _, err := qtx.CreateAgentTaskCompletionOutcome(ctx, db.CreateAgentTaskCompletionOutcomeParams{
		TaskID: task.ID, WorkspaceID: issue.WorkspaceID, IssueID: task.IssueID,
		AgentID: task.AgentID, OutcomeKind: outcomeKind, Content: content,
		ResultSha256: completionResultDigest(result), FinalCommentID: finalComment.ID,
		AnsweredCommentIds: answered,
	}); err != nil {
		return fmt.Errorf("persist issue completion outcome: %w", err)
	}
	if _, err := qtx.CreateTaskCompletionOutbox(ctx, db.CreateTaskCompletionOutboxParams{
		TaskID: task.ID, WorkspaceID: issue.WorkspaceID, IssueID: task.IssueID,
		AgentID: task.AgentID, CommentID: finalComment.ID,
		IssueRevision: issueRevision, State: outboxState,
	}); err != nil {
		return fmt.Errorf("persist issue completion outbox: %w", err)
	}
	return nil
}

func completionOutboxRetryAt(attempt int32) time.Time {
	delay := time.Duration(attempt) * 15 * time.Second
	if delay < 15*time.Second {
		delay = 15 * time.Second
	}
	if delay > 5*time.Minute {
		delay = 5 * time.Minute
	}
	return time.Now().UTC().Add(delay)
}

func (s *TaskService) retryCompletionOutbox(ctx context.Context, row db.TaskCompletionOutbox, owner string, deliveryErr error) {
	message := truncateFallbackCommentBody(redact.Text(deliveryErr.Error()), 1000)
	if _, err := s.Queries.RetryTaskCompletionOutbox(ctx, db.RetryTaskCompletionOutboxParams{
		MaxAttempts:   taskCompletionOutboxMaxAttempts,
		NextAttemptAt: pgtype.Timestamptz{Time: completionOutboxRetryAt(row.Attempts), Valid: true},
		LastError:     pgtype.Text{String: message, Valid: message != ""},
		TaskID:        row.TaskID, LeaseOwner: pgtype.Text{String: owner, Valid: true},
	}); err != nil {
		slog.Warn("task completion outbox retry update failed", "task_id", util.UUIDToString(row.TaskID), "error", err)
	}
}

// DeliverTaskCompletionOutbox publishes durable final-comment records without
// re-running the completed task. Delivery is at-least-once; clients converge on
// the stable comment ID, and an expired publishing lease is safely reclaimed.
func (s *TaskService) DeliverTaskCompletionOutbox(ctx context.Context, limit int) (TaskCompletionOutboxDeliveryResult, error) {
	var result TaskCompletionOutboxDeliveryResult
	if s == nil || s.Queries == nil || limit <= 0 {
		return result, nil
	}
	owner := util.UUIDToString(dbid.NewV7())
	rows, err := s.Queries.ClaimTaskCompletionOutbox(ctx, db.ClaimTaskCompletionOutboxParams{
		LeaseOwner:   pgtype.Text{String: owner, Valid: true},
		LeaseSeconds: taskCompletionOutboxLeaseSeconds,
		MaxAttempts:  taskCompletionOutboxMaxAttempts,
		RowLimit:     int32(limit),
	})
	if err != nil {
		return result, fmt.Errorf("claim task completion outbox: %w", err)
	}
	result.Claimed = len(rows)
	for _, row := range rows {
		if s.Bus == nil {
			result.Failed++
			s.retryCompletionOutbox(ctx, row, owner, errors.New("event bus unavailable"))
			continue
		}
		comment, err := s.Queries.GetCommentInWorkspace(ctx, db.GetCommentInWorkspaceParams{
			ID: row.CommentID, WorkspaceID: row.WorkspaceID,
		})
		if err != nil {
			result.Failed++
			s.retryCompletionOutbox(ctx, row, owner, fmt.Errorf("load final comment: %w", err))
			continue
		}
		issue, err := s.Queries.GetIssueInWorkspace(ctx, db.GetIssueInWorkspaceParams{
			ID: row.IssueID, WorkspaceID: row.WorkspaceID,
		})
		if err != nil {
			result.Failed++
			s.retryCompletionOutbox(ctx, row, owner, fmt.Errorf("load final issue: %w", err))
			continue
		}
		commentFields := commentEventFields(comment)
		commentFields["revision"] = comment.Revision
		s.Bus.Publish(events.Event{
			Type: protocol.EventCommentCreated, WorkspaceID: util.UUIDToString(row.WorkspaceID),
			ActorType: "agent", ActorID: util.UUIDToString(row.AgentID),
			Payload: map[string]any{
				"comment": commentFields, "issue_title": issue.Title,
				"issue_status": issue.Status, "issue_revision": row.IssueRevision,
			},
		})
		if comment.ParentID.Valid {
			if root, rootErr := s.Queries.GetThreadRoot(ctx, db.GetThreadRootParams{
				CommentID: comment.ParentID, WorkspaceID: row.WorkspaceID,
			}); rootErr == nil {
				s.AutoUnresolveThreadOnReply(ctx, &root, util.UUIDToString(row.WorkspaceID), "agent", util.UUIDToString(row.AgentID))
			}
		}
		updated, err := s.Queries.MarkTaskCompletionOutboxPublished(ctx, db.MarkTaskCompletionOutboxPublishedParams{
			TaskID: row.TaskID, LeaseOwner: pgtype.Text{String: owner, Valid: true},
		})
		if err != nil || updated != 1 {
			result.Failed++
			if err == nil {
				err = fmt.Errorf("completion outbox lease was lost")
			}
			slog.Warn("task completion outbox publish receipt failed", "task_id", util.UUIDToString(row.TaskID), "error", err)
			continue
		}
		result.Published++
	}
	return result, nil
}
