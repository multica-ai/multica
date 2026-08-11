package service

import (
	"context"
	"errors"
	"fmt"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"
	db "github.com/multica-ai/multica/server/pkg/db/generated"
)

var ErrCommentFollowupScope = errors.New("comment follow-up scope mismatch")

type commentFollowupCursor struct {
	UpdatedAt pgtype.Timestamptz
	ID        pgtype.UUID
}

// PersistCommentFollowup durably records an uncovered Issue-comment trigger.
// Locking the Comment before the obligation is the Workspace-delete barrier:
// once deletion has passed the Comment row, its following cleanup statement
// can see and remove every obligation committed behind that lock.
func (s *TaskService) PersistCommentFollowup(
	ctx context.Context,
	issueID pgtype.UUID,
	agentID pgtype.UUID,
	commentID pgtype.UUID,
	headSHA string,
) error {
	if s == nil || s.Queries == nil {
		return errors.New("comment follow-up queries are required")
	}
	return s.runInTx(ctx, func(qtx *db.Queries) error {
		comment, err := qtx.LockCommentForFollowup(ctx, commentID)
		if errors.Is(err, pgx.ErrNoRows) {
			return ErrCommentFollowupScope
		}
		if err != nil {
			return fmt.Errorf("lock comment for follow-up: %w", err)
		}
		issue, err := qtx.GetIssue(ctx, issueID)
		if err != nil {
			return fmt.Errorf("load follow-up Issue: %w", err)
		}
		agent, err := qtx.GetAgent(ctx, agentID)
		if err != nil {
			return fmt.Errorf("load follow-up Agent: %w", err)
		}
		if comment.IssueID != issue.ID || comment.WorkspaceID != issue.WorkspaceID || agent.WorkspaceID != issue.WorkspaceID {
			return ErrCommentFollowupScope
		}
		_, err = qtx.UpsertCommentFollowupObligation(ctx, db.UpsertCommentFollowupObligationParams{
			IssueID:          issue.ID,
			AgentID:          agent.ID,
			CommentID:        comment.ID,
			CommentUpdatedAt: comment.UpdatedAt,
			HeadSha:          headSHA,
		})
		if err != nil {
			return fmt.Errorf("upsert comment follow-up obligation: %w", err)
		}
		return nil
	})
}

// ProcessCommentFollowups replays a bounded page of durable Issue-comment
// obligations. The cursor advances after every attempted row and wraps only
// after an empty page, so one persistently blocked Issue cannot starve newer
// obligations. Creation is intentionally routed through the normal Task 9
// Pool entry; the obligation is removed only after a Task is observably linked
// to the Comment.
func (s *TaskService) ProcessCommentFollowups(ctx context.Context, limit int32) error {
	if s == nil || s.Queries == nil {
		return errors.New("comment follow-up queries are required")
	}
	if limit <= 0 {
		return nil
	}
	s.commentFollowupMu.Lock()
	defer s.commentFollowupMu.Unlock()

	list := func() ([]db.AgentCommentFollowupObligation, error) {
		return s.Queries.ListCommentFollowupObligations(ctx, db.ListCommentFollowupObligationsParams{
			AfterUpdatedAt: s.commentFollowupCursor.UpdatedAt,
			AfterID:        s.commentFollowupCursor.ID,
			ScanLimit:      limit,
		})
	}
	obligations, err := list()
	if err != nil {
		return fmt.Errorf("list comment follow-ups: %w", err)
	}
	if len(obligations) == 0 && s.commentFollowupCursor.ID.Valid {
		s.commentFollowupCursor = commentFollowupCursor{}
		obligations, err = list()
		if err != nil {
			return fmt.Errorf("wrap comment follow-up cursor: %w", err)
		}
	}
	for _, obligation := range obligations {
		s.commentFollowupCursor = commentFollowupCursor{UpdatedAt: obligation.UpdatedAt, ID: obligation.ID}
		if err := s.processCommentFollowup(ctx, obligation.AgentID, obligation.CommentID); err != nil {
			if errors.Is(err, ErrDuplicatePendingTask) || errors.Is(err, ErrCommentFollowupScope) {
				continue
			}
			return err
		}
	}
	return nil
}

func (s *TaskService) processCommentFollowup(ctx context.Context, agentID, commentID pgtype.UUID) error {
	var obligation db.AgentCommentFollowupObligation
	var issue db.Issue
	var comment db.Comment
	err := s.runInTx(ctx, func(qtx *db.Queries) error {
		lockedComment, err := qtx.LockCommentForFollowup(ctx, commentID)
		if errors.Is(err, pgx.ErrNoRows) {
			_, deleteErr := qtx.DeleteCommentFollowupObligationInvalid(ctx, db.DeleteCommentFollowupObligationInvalidParams{
				AgentID: agentID, CommentID: commentID,
			})
			return deleteErr
		}
		if err != nil {
			return fmt.Errorf("lock follow-up Comment: %w", err)
		}
		lockedObligation, err := qtx.LockCommentFollowupObligation(ctx, db.LockCommentFollowupObligationParams{
			AgentID: agentID, CommentID: commentID,
		})
		if errors.Is(err, pgx.ErrNoRows) {
			return nil
		}
		if err != nil {
			return fmt.Errorf("lock comment follow-up obligation: %w", err)
		}
		lockedIssue, err := qtx.GetIssue(ctx, lockedObligation.IssueID)
		if err != nil {
			return fmt.Errorf("load follow-up Issue: %w", err)
		}
		agent, err := qtx.GetAgent(ctx, lockedObligation.AgentID)
		if err != nil {
			return fmt.Errorf("load follow-up Agent: %w", err)
		}
		if lockedComment.IssueID != lockedIssue.ID || lockedComment.WorkspaceID != lockedIssue.WorkspaceID || agent.WorkspaceID != lockedIssue.WorkspaceID {
			_, err = qtx.DeleteCommentFollowupObligationInvalid(ctx, db.DeleteCommentFollowupObligationInvalidParams{
				AgentID: agentID, CommentID: commentID,
			})
			return err
		}
		if lockedComment.UpdatedAt != lockedObligation.CommentUpdatedAt {
			headSHA := ""
			if value, headErr := qtx.GetIssueReviewHeadSha(ctx, lockedIssue.ID); headErr == nil {
				headSHA = value
			} else if !errors.Is(headErr, pgx.ErrNoRows) {
				return fmt.Errorf("load current follow-up HEAD: %w", headErr)
			}
			_, err = qtx.RefreshCommentFollowupObligation(ctx, db.RefreshCommentFollowupObligationParams{
				AgentID:          agentID,
				CommentID:        commentID,
				CommentUpdatedAt: lockedComment.UpdatedAt,
				HeadSha:          headSHA,
			})
			return err
		}
		obligation, issue, comment = lockedObligation, lockedIssue, lockedComment
		return nil
	})
	if err != nil || !obligation.ID.Valid {
		return err
	}

	covered, err := s.Queries.CommentFollowupCoveredByTask(ctx, db.CommentFollowupCoveredByTaskParams{
		IssueID: issue.ID, AgentID: agentID, CommentID: comment.ID,
	})
	if err != nil {
		return fmt.Errorf("check comment follow-up coverage: %w", err)
	}
	if !covered {
		_, enqueueErr := s.EnqueueTaskForMention(ctx, issue, agentID, commentID)
		covered, err = s.Queries.CommentFollowupCoveredByTask(ctx, db.CommentFollowupCoveredByTaskParams{
			IssueID: issue.ID, AgentID: agentID, CommentID: comment.ID,
		})
		if err != nil {
			return fmt.Errorf("recheck comment follow-up coverage: %w", err)
		}
		if !covered {
			if enqueueErr != nil {
				return enqueueErr
			}
			return ErrDuplicatePendingTask
		}
	}
	affected, err := s.Queries.DeleteCommentFollowupObligation(ctx, db.DeleteCommentFollowupObligationParams{
		AgentID:          obligation.AgentID,
		CommentID:        obligation.CommentID,
		CommentUpdatedAt: obligation.CommentUpdatedAt,
		HeadSha:          obligation.HeadSha,
	})
	if err != nil {
		return fmt.Errorf("delete covered comment follow-up: %w", err)
	}
	if affected != 1 {
		return nil
	}
	return nil
}
