package service

import (
	"context"
	"errors"
	"fmt"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/multica-ai/multica/server/internal/issuelifecycle"
	"github.com/multica-ai/multica/server/internal/issuestatus"
	db "github.com/multica-ai/multica/server/pkg/db/generated"
)

var (
	// ErrIssueTransitionConflict means a caller attempted to transition from a
	// stale issue revision or lifecycle entry.
	ErrIssueTransitionConflict = errors.New("issue transition conflict")
	// ErrIssueTransitionStatusUnavailable means the target status is absent or
	// archived in the issue's workspace catalog.
	ErrIssueTransitionStatusUnavailable = errors.New("issue transition status unavailable")
)

type IssueTransitionParams struct {
	IssueID              pgtype.UUID
	WorkspaceID          pgtype.UUID
	Status               string
	Actor                issuelifecycle.TransitionActor
	Cause                string
	ExpectedRevision     pgtype.Int8
	ExpectedTransitionID pgtype.UUID
}

type IssueStatusNodeTransitionParams struct {
	IssueID              pgtype.UUID
	WorkspaceID          pgtype.UUID
	LifecycleStatusID    pgtype.UUID
	Actor                issuelifecycle.TransitionActor
	Cause                string
	ExpectedRevision     pgtype.Int8
	ExpectedTransitionID pgtype.UUID
}

type IssueTransitionResult struct {
	Previous   db.Issue
	Issue      db.Issue
	Transition db.IssueTransition
	Changed    bool
}

// TransitionIssue is the canonical status-only write boundary. It serializes
// the issue row, applies optimistic preconditions, dual-writes the legacy key
// and lifecycle node, and records the immutable transition in one transaction.
func TransitionIssue(ctx context.Context, q *db.Queries, txStarter TxStarter, p IssueTransitionParams) (IssueTransitionResult, error) {
	if txStarter == nil {
		return IssueTransitionResult{}, errors.New("issue transition requires transaction starter")
	}
	tx, err := txStarter.Begin(ctx)
	if err != nil {
		return IssueTransitionResult{}, fmt.Errorf("begin issue transition: %w", err)
	}
	defer tx.Rollback(ctx)
	qtx := q.WithTx(tx)

	// Match the catalog archive protocol used by HTTP updates: catalog first,
	// then issue row, so archive and transition cannot deadlock.
	if !issuestatus.IsBuiltIn(p.Status) {
		if err := qtx.LockIssueStatusCatalogShared(ctx, p.WorkspaceID); err != nil {
			return IssueTransitionResult{}, fmt.Errorf("lock issue status catalog: %w", err)
		}
		if _, err := issuestatus.Resolve(ctx, qtx, p.WorkspaceID, p.Status); err != nil {
			if errors.Is(err, issuestatus.ErrUnknownStatus) {
				return IssueTransitionResult{}, ErrIssueTransitionStatusUnavailable
			}
			return IssueTransitionResult{}, err
		}
	}

	previous, err := qtx.LockIssueForDescriptionUpdate(ctx, db.LockIssueForDescriptionUpdateParams{
		ID: p.IssueID, WorkspaceID: p.WorkspaceID,
	})
	if err != nil {
		return IssueTransitionResult{}, err
	}
	if p.ExpectedRevision.Valid && previous.Revision != p.ExpectedRevision.Int64 {
		return IssueTransitionResult{}, ErrIssueTransitionConflict
	}
	if p.ExpectedTransitionID.Valid && previous.LastTransitionID != p.ExpectedTransitionID {
		return IssueTransitionResult{}, ErrIssueTransitionConflict
	}

	current, err := qtx.UpdateIssueStatus(ctx, db.UpdateIssueStatusParams{
		ID: p.IssueID, WorkspaceID: p.WorkspaceID, Status: p.Status,
	})
	if err != nil {
		return IssueTransitionResult{}, err
	}
	current, transition, changed, err := issuelifecycle.RecordTransition(
		ctx, qtx, &previous, current, p.Actor, p.Cause,
	)
	if err != nil {
		return IssueTransitionResult{}, err
	}
	if err := tx.Commit(ctx); err != nil {
		return IssueTransitionResult{}, fmt.Errorf("commit issue transition: %w", err)
	}
	return IssueTransitionResult{
		Previous: previous, Issue: current, Transition: transition, Changed: changed,
	}, nil
}

// TransitionIssueToStatusNode is the canonical lifecycle-native status write.
// The stable node ID, not the legacy status key, selects the destination. The
// legacy key is updated in the same transaction as a compatibility projection
// for installed clients and rolling rollback.
func TransitionIssueToStatusNode(ctx context.Context, q *db.Queries, txStarter TxStarter, p IssueStatusNodeTransitionParams) (IssueTransitionResult, error) {
	if txStarter == nil {
		return IssueTransitionResult{}, errors.New("issue transition requires transaction starter")
	}
	tx, err := txStarter.Begin(ctx)
	if err != nil {
		return IssueTransitionResult{}, fmt.Errorf("begin issue transition: %w", err)
	}
	defer tx.Rollback(ctx)
	qtx := q.WithTx(tx)

	previous, err := qtx.LockIssueForDescriptionUpdate(ctx, db.LockIssueForDescriptionUpdateParams{
		ID: p.IssueID, WorkspaceID: p.WorkspaceID,
	})
	if err != nil {
		return IssueTransitionResult{}, err
	}
	if p.ExpectedRevision.Valid && previous.Revision != p.ExpectedRevision.Int64 {
		return IssueTransitionResult{}, ErrIssueTransitionConflict
	}
	if p.ExpectedTransitionID.Valid && previous.LastTransitionID != p.ExpectedTransitionID {
		return IssueTransitionResult{}, ErrIssueTransitionConflict
	}
	if !previous.LifecycleID.Valid {
		return IssueTransitionResult{}, ErrIssueTransitionConflict
	}
	target, err := qtx.GetIssueLifecycleStatusByID(ctx, db.GetIssueLifecycleStatusByIDParams{
		WorkspaceID: p.WorkspaceID,
		LifecycleID: previous.LifecycleID,
		ID:          p.LifecycleStatusID,
	})
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return IssueTransitionResult{}, ErrIssueTransitionStatusUnavailable
		}
		return IssueTransitionResult{}, err
	}
	if target.ArchivedAt.Valid || !target.LegacyStatusKey.Valid {
		return IssueTransitionResult{}, ErrIssueTransitionStatusUnavailable
	}
	if previous.LifecycleStatusID == target.ID {
		return IssueTransitionResult{Previous: previous, Issue: previous}, nil
	}

	current, err := qtx.UpdateIssueLifecycleStatus(ctx, db.UpdateIssueLifecycleStatusParams{
		IssueID: p.IssueID, WorkspaceID: p.WorkspaceID, LifecycleStatusID: target.ID,
	})
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return IssueTransitionResult{}, ErrIssueTransitionStatusUnavailable
		}
		return IssueTransitionResult{}, err
	}
	current, transition, changed, err := issuelifecycle.RecordTransition(
		ctx, qtx, &previous, current, p.Actor, p.Cause,
	)
	if err != nil {
		return IssueTransitionResult{}, err
	}
	if err := tx.Commit(ctx); err != nil {
		return IssueTransitionResult{}, fmt.Errorf("commit issue transition: %w", err)
	}
	return IssueTransitionResult{
		Previous: previous, Issue: current, Transition: transition, Changed: changed,
	}, nil
}

func (s *IssueService) TransitionStatus(ctx context.Context, p IssueTransitionParams) (IssueTransitionResult, error) {
	return TransitionIssue(ctx, s.Queries, s.TxStarter, p)
}

func (s *IssueService) TransitionStatusNode(ctx context.Context, p IssueStatusNodeTransitionParams) (IssueTransitionResult, error) {
	return TransitionIssueToStatusNode(ctx, s.Queries, s.TxStarter, p)
}

func (s *TaskService) transitionIssueStatus(ctx context.Context, p IssueTransitionParams) (IssueTransitionResult, error) {
	return TransitionIssue(ctx, s.Queries, s.TxStarter, p)
}
