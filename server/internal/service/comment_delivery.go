package service

import (
	"context"
	"errors"
	"fmt"
	"github.com/jackc/pgx/v5"
	"github.com/multica-ai/multica/server/internal/agentinvoke"
	"github.com/multica-ai/multica/server/internal/util"
	"slices"

	"github.com/jackc/pgx/v5/pgtype"
	"github.com/multica-ai/multica/server/internal/attribution"
	db "github.com/multica-ai/multica/server/pkg/db/generated"
)

// EnqueueAcceptedComment preserves the actor of the accepted comment action,
// including an editor who differs from the comment's original author.
func (s *TaskService) EnqueueAcceptedComment(ctx context.Context, issue db.Issue, accepted db.CommentDeliveryObligation, issueAssignee bool, squadID pgtype.UUID, origin RunOrigin) (db.AgentTaskQueue, error) {
	if accepted.WorkspaceID != issue.WorkspaceID || accepted.IssueID != issue.ID {
		return db.AgentTaskQueue{}, fmt.Errorf("accepted comment scope does not match issue")
	}
	if issueAssignee && !squadID.Valid {
		if issue.AssigneeID != accepted.AgentID {
			return db.AgentTaskQueue{}, fmt.Errorf("accepted comment recipient is no longer assigned")
		}
		return s.enqueueIssueTaskWithCommentPlan(ctx, issue, accepted.CommentID, nil, false, "", pgtype.UUID{}, pgtype.UUID{}, pgtype.Timestamptz{}, origin, false, &accepted)
	}
	return s.enqueueMentionTaskWithCommentPlan(ctx, issue, accepted.AgentID, accepted.CommentID, nil, squadID.Valid, squadID, false, "", pgtype.UUID{}, pgtype.UUID{}, origin, false, false, &accepted)
}

func (s *TaskService) attributionForAcceptedComment(ctx context.Context, accepted db.CommentDeliveryObligation, source attribution.Source) (attribution.Result, error) {
	facts := attribution.CommentFacts{
		CommentID: accepted.CommentID, AuthorType: accepted.ActorType,
		AuthorID: accepted.ActorID, CreatedAt: accepted.InputUpdatedAt,
	}
	if accepted.ActorType == "agent" && accepted.AuthoringTaskID.Valid {
		parent, err := s.Queries.GetAgentTask(ctx, accepted.AuthoringTaskID)
		if err != nil {
			return attribution.Result{}, fmt.Errorf("load accepted comment lineage: %w", err)
		}
		if parent.AgentID != accepted.ActorID || parent.OriginatorUserID != accepted.OriginatorUserID {
			return attribution.Result{}, ErrAttributionFailClosed
		}
		facts.SourceTaskID = accepted.AuthoringTaskID
		facts.ParentOriginator = parent.OriginatorUserID
		facts.ParentAccountable = parent.AccountableUserID
	}
	return attribution.ClassifyComment(facts, source), nil
}

// Missing linkage permits an unkeyed task; a failed lookup does not.
func commentDeliveryReviewHead(ctx context.Context, q *db.Queries, issueID pgtype.UUID) (pgtype.Text, error) {
	sha, err := q.GetIssueReviewHeadSha(ctx, issueID)
	if err != nil && !errors.Is(err, pgx.ErrNoRows) {
		return pgtype.Text{}, fmt.Errorf("resolve accepted comment review head: %w", err)
	}
	return headShaText(sha), nil
}

var ErrCommentDeliveryChanged = errors.New("accepted comment input or execution context changed")

// WithAcceptedCommentWrite holds the accepted input through the queue mutation.
// Prepare overlays before calling it, and publish events only after it returns.
func (s *TaskService) WithAcceptedCommentWrite(ctx context.Context, issue db.Issue, expectedAgent db.Agent, accepted db.CommentDeliveryObligation, originator, accountable pgtype.UUID, write func(*db.Queries, pgtype.Text) error) error {
	if s.TxStarter == nil {
		return fmt.Errorf("accepted comment write requires a transaction")
	}
	tx, err := s.TxStarter.Begin(ctx)
	if err != nil {
		return err
	}
	defer tx.Rollback(ctx)
	q := s.Queries.WithTx(tx)
	// Owners precede the input locks. An edit/delete locks its issue first,
	// so neither side can hold the input while waiting for the other's issue.
	agent, err := q.LockAgentForCommentDelivery(ctx, db.LockAgentForCommentDeliveryParams{AgentID: accepted.AgentID, WorkspaceID: accepted.WorkspaceID})
	if err != nil {
		return err
	}
	if agent.ArchivedAt.Valid || !agent.RuntimeID.Valid {
		return ErrCommentDeliveryChanged
	}
	locked, err := q.LockCommentDeliveryOwners(ctx, db.LockCommentDeliveryOwnersParams{AgentID: agent.ID, IssueID: accepted.IssueID, RuntimeID: agent.RuntimeID})
	if err != nil {
		return err
	}
	if !locked {
		return ErrCommentDeliveryChanged
	}
	comment, err := q.LockCommentDeliverySource(ctx, db.LockCommentDeliverySourceParams{CommentID: accepted.CommentID, WorkspaceID: accepted.WorkspaceID})
	if err != nil {
		return err
	}
	if comment.IssueID != accepted.IssueID || comment.DeletedAt.Valid || comment.Content != accepted.CommentContent || comment.Revision < accepted.CommentRevision || slices.Contains(comment.SuppressedAgentIds, agent.ID) {
		return ErrCommentDeliveryChanged
	}
	_, err = q.LockClaimedCommentDeliveryObligation(ctx, db.LockClaimedCommentDeliveryObligationParams{WorkspaceID: accepted.WorkspaceID, CommentID: accepted.CommentID, AgentID: accepted.AgentID, CommentRevision: accepted.CommentRevision, LeaseUntil: accepted.LeaseUntil})
	if err != nil {
		return err
	}
	// The owner/source locks above may have waited while agent metadata changed.
	agent, err = q.GetAgentInWorkspace(ctx, db.GetAgentInWorkspaceParams{ID: accepted.AgentID, WorkspaceID: accepted.WorkspaceID})
	if err != nil {
		return err
	}
	if agent.ArchivedAt.Valid || agent.OwnerID != expectedAgent.OwnerID || agent.RuntimeID != expectedAgent.RuntimeID || agent.PermissionMode != expectedAgent.PermissionMode || !agent.UpdatedAt.Time.Equal(expectedAgent.UpdatedAt.Time) {
		return ErrCommentDeliveryChanged
	}
	currentIssue, err := q.GetIssue(ctx, accepted.IssueID)
	if err != nil {
		return err
	}
	if currentIssue.WorkspaceID != accepted.WorkspaceID || currentIssue.ProjectID != issue.ProjectID || currentIssue.AssigneeID != issue.AssigneeID || currentIssue.AssigneeType != issue.AssigneeType {
		return ErrCommentDeliveryChanged
	}
	effective := accepted.OriginatorUserID
	if accepted.ActorType == "member" {
		effective = accepted.ActorID
	}
	if effective.Valid {
		if _, err := q.GetMemberByUserAndWorkspace(ctx, db.GetMemberByUserAndWorkspaceParams{UserID: effective, WorkspaceID: accepted.WorkspaceID}); err != nil {
			return err
		}
	}
	if accepted.ActorType == "agent" {
		actor, err := q.GetAgentInWorkspace(ctx, db.GetAgentInWorkspaceParams{ID: accepted.ActorID, WorkspaceID: accepted.WorkspaceID})
		if err != nil {
			return err
		}
		if actor.ArchivedAt.Valid {
			return ErrCommentDeliveryChanged
		}
		if accepted.AuthoringTaskID.Valid {
			parent, err := q.GetAgentTask(ctx, accepted.AuthoringTaskID)
			if err != nil {
				return err
			}
			if parent.AgentID != accepted.ActorID || parent.OriginatorUserID != accepted.OriginatorUserID {
				return ErrCommentDeliveryChanged
			}
		}
	}
	if !agentinvoke.Allowed(ctx, q, agent, accepted.ActorType, util.UUIDToString(accepted.ActorID), util.UUIDToString(accepted.OriginatorUserID), util.UUIDToString(accepted.WorkspaceID)) {
		return ErrCommentDeliveryChanged
	}
	if err := guardRunProjectsAccess(ctx, q, accepted.WorkspaceID, agent, originator, accountable, currentIssue.ProjectID); err != nil {
		return err
	}
	head, err := commentDeliveryReviewHead(ctx, q, accepted.IssueID)
	if err != nil {
		return err
	}
	if err := write(q, head); err != nil {
		return err
	}
	return tx.Commit(ctx)
}
