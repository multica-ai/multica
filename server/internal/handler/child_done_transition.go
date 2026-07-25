package handler

import (
	"context"
	"fmt"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgtype"

	db "github.com/multica-ai/multica/server/pkg/db/generated"
)

func newChildDoneGroupID() pgtype.UUID {
	return pgtype.UUID{Bytes: [16]byte(uuid.New()), Valid: true}
}

// updateIssueAndRecordChildDone makes the terminal status write and its
// recoverable notification event one transaction. The row lock gives the
// transition guard the actual committed predecessor even when status writers
// race.
func (h *Handler) updateIssueAndRecordChildDone(
	ctx context.Context,
	params db.UpdateIssueParams,
	groupID pgtype.UUID,
	deferGroup bool,
) (db.Issue, db.Issue, error) {
	tx, err := h.TxStarter.Begin(ctx)
	if err != nil {
		return db.Issue{}, db.Issue{}, fmt.Errorf("begin issue update: %w", err)
	}
	defer tx.Rollback(ctx) // no-op after Commit

	q := h.Queries.WithTx(tx)
	previous, err := q.GetIssueForUpdate(ctx, params.ID)
	if err != nil {
		return db.Issue{}, db.Issue{}, fmt.Errorf("lock issue update: %w", err)
	}
	updated, err := q.UpdateIssue(ctx, params)
	if err != nil {
		return db.Issue{}, db.Issue{}, err
	}
	if err := recordChildDoneTransition(ctx, q, previous, updated, groupID, deferGroup); err != nil {
		return db.Issue{}, db.Issue{}, err
	}
	if err := tx.Commit(ctx); err != nil {
		return db.Issue{}, db.Issue{}, fmt.Errorf("commit issue update: %w", err)
	}
	return previous, updated, nil
}

func (h *Handler) updateIssueStatusAndRecordChildDone(
	ctx context.Context,
	id, workspaceID pgtype.UUID,
	status string,
	groupID pgtype.UUID,
) (db.Issue, db.Issue, error) {
	tx, err := h.TxStarter.Begin(ctx)
	if err != nil {
		return db.Issue{}, db.Issue{}, fmt.Errorf("begin issue status update: %w", err)
	}
	defer tx.Rollback(ctx) // no-op after Commit

	q := h.Queries.WithTx(tx)
	previous, err := q.GetIssueForUpdate(ctx, id)
	if err != nil {
		return db.Issue{}, db.Issue{}, fmt.Errorf("lock issue status update: %w", err)
	}
	if previous.WorkspaceID != workspaceID {
		return db.Issue{}, db.Issue{}, fmt.Errorf("issue does not belong to workspace")
	}
	updated, err := q.UpdateIssueStatus(ctx, db.UpdateIssueStatusParams{
		ID:          id,
		Status:      status,
		WorkspaceID: workspaceID,
	})
	if err != nil {
		return db.Issue{}, db.Issue{}, err
	}
	if err := recordChildDoneTransition(ctx, q, previous, updated, groupID, false); err != nil {
		return db.Issue{}, db.Issue{}, err
	}
	if err := tx.Commit(ctx); err != nil {
		return db.Issue{}, db.Issue{}, fmt.Errorf("commit issue status update: %w", err)
	}
	return previous, updated, nil
}

func recordChildDoneTransition(
	ctx context.Context,
	q *db.Queries,
	previous, updated db.Issue,
	groupID pgtype.UUID,
	deferGroup bool,
) error {
	if !groupID.Valid || !updated.ParentIssueID.Valid ||
		!entersChildDoneBarrier(previous.Status, updated.Status) {
		return nil
	}
	if err := q.CreateChildDoneTransition(ctx, db.CreateChildDoneTransitionParams{
		GroupID:        groupID,
		ChildIssueID:   updated.ID,
		ParentIssueID:  updated.ParentIssueID,
		WorkspaceID:    updated.WorkspaceID,
		TerminalStatus: updated.Status,
		Stage:          updated.Stage,
		TransitionAt:   updated.UpdatedAt,
		DeferGroup:     deferGroup,
	}); err != nil {
		return fmt.Errorf("record child-done transition: %w", err)
	}
	return nil
}

func (h *Handler) deleteIssueWithChildDoneTransitions(
	ctx context.Context,
	params db.DeleteIssueParams,
) error {
	tx, err := h.TxStarter.Begin(ctx)
	if err != nil {
		return fmt.Errorf("begin issue delete: %w", err)
	}
	defer tx.Rollback(ctx)

	q := h.Queries.WithTx(tx)
	if err := q.DeleteChildDoneTransitionsByIssue(ctx, params.ID); err != nil {
		return fmt.Errorf("delete child-done transitions: %w", err)
	}
	if err := q.DeleteIssue(ctx, params); err != nil {
		return err
	}
	if err := tx.Commit(ctx); err != nil {
		return fmt.Errorf("commit issue delete: %w", err)
	}
	return nil
}
