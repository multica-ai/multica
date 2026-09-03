// Package issuelifecycle owns the additive lifecycle projection introduced by
// MUL-7022. During rollout issue.status remains the compatibility handle while
// every write also pins the issue to a stable lifecycle status node.
package issuelifecycle

import (
	"context"
	"errors"
	"fmt"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"
	db "github.com/multica-ai/multica/server/pkg/db/generated"
	"github.com/multica-ai/multica/server/pkg/dbid"
)

const (
	PhaseBacklog   = "backlog"
	PhaseUnstarted = "unstarted"
	PhaseStarted   = "started"
	PhaseCompleted = "completed"
	PhaseCancelled = "cancelled"
)

// LegacyCategoryPhase is the one-time compatibility mapping. It intentionally
// collapses in_progress, in_review, and blocked into started; behavior that
// distinguishes those categories belongs in an explicit policy consumer.
func LegacyCategoryPhase(category string) (phase string, outcome pgtype.Text, err error) {
	switch category {
	case "backlog":
		return PhaseBacklog, pgtype.Text{}, nil
	case "todo":
		return PhaseUnstarted, pgtype.Text{}, nil
	case "in_progress", "in_review", "blocked":
		return PhaseStarted, pgtype.Text{}, nil
	case "done":
		return PhaseCompleted, pgtype.Text{String: "completed", Valid: true}, nil
	case "cancelled":
		return PhaseCancelled, pgtype.Text{String: "cancelled", Valid: true}, nil
	default:
		return "", pgtype.Text{}, fmt.Errorf("unknown legacy issue status category %q", category)
	}
}

// Querier is the transaction-bound query surface used by lifecycle bootstrap
// and transition recording.
type Querier interface {
	EnsureDefaultIssueLifecycle(context.Context, pgtype.UUID) (db.IssueLifecycle, error)
	SeedIssueStatusEntries(context.Context, pgtype.UUID) error
	SetWorkspaceDefaultIssueLifecycle(context.Context, db.SetWorkspaceDefaultIssueLifecycleParams) error
	SyncDefaultIssueLifecycleStatuses(context.Context, db.SyncDefaultIssueLifecycleStatusesParams) error
	GetDefaultIssueLifecycle(context.Context, pgtype.UUID) (db.IssueLifecycle, error)
	GetIssueLifecycleByID(context.Context, db.GetIssueLifecycleByIDParams) (db.IssueLifecycle, error)
	BumpIssueLifecycleRevision(context.Context, db.BumpIssueLifecycleRevisionParams) (db.IssueLifecycle, error)
	GetIssueLifecycleStatusByLegacyKey(context.Context, db.GetIssueLifecycleStatusByLegacyKeyParams) (db.IssueLifecycleStatus, error)
	BindIssueToDefaultLifecycle(context.Context, db.BindIssueToDefaultLifecycleParams) (db.Issue, error)
	InsertIssueTransition(context.Context, db.InsertIssueTransitionParams) (int64, error)
	GetIssueTransitionByRevision(context.Context, db.GetIssueTransitionByRevisionParams) (db.IssueTransition, error)
	SetIssueLastTransition(context.Context, db.SetIssueLastTransitionParams) (db.Issue, error)
}

// EnsureDefault creates or repairs a workspace's shadow lifecycle projection.
// It is safe to call repeatedly and must run in the same transaction as a new
// workspace's legacy status seed.
func EnsureDefault(ctx context.Context, q Querier, workspaceID pgtype.UUID) (db.IssueLifecycle, error) {
	lifecycle, err := q.EnsureDefaultIssueLifecycle(ctx, workspaceID)
	if err != nil {
		return db.IssueLifecycle{}, fmt.Errorf("ensure default issue lifecycle: %w", err)
	}
	if err := q.SetWorkspaceDefaultIssueLifecycle(ctx, db.SetWorkspaceDefaultIssueLifecycleParams{
		ID: workspaceID, DefaultIssueLifecycleID: lifecycle.ID,
	}); err != nil {
		return db.IssueLifecycle{}, fmt.Errorf("set workspace default issue lifecycle: %w", err)
	}
	if err := q.SyncDefaultIssueLifecycleStatuses(ctx, db.SyncDefaultIssueLifecycleStatusesParams{
		WorkspaceID: workspaceID, LifecycleID: lifecycle.ID,
	}); err != nil {
		return db.IssueLifecycle{}, fmt.Errorf("sync default issue lifecycle statuses: %w", err)
	}
	return lifecycle, nil
}

// SyncDefault projects catalog edits (rename, reorder, archive) into the
// workspace's default lifecycle without changing any issue or starting work.
func SyncDefault(ctx context.Context, q Querier, workspaceID pgtype.UUID) error {
	lifecycle, err := q.GetDefaultIssueLifecycle(ctx, workspaceID)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			_, ensureErr := EnsureDefault(ctx, q, workspaceID)
			return ensureErr
		}
		return fmt.Errorf("get default issue lifecycle: %w", err)
	}
	if err := q.SyncDefaultIssueLifecycleStatuses(ctx, db.SyncDefaultIssueLifecycleStatusesParams{
		WorkspaceID: workspaceID, LifecycleID: lifecycle.ID,
	}); err != nil {
		return fmt.Errorf("sync default issue lifecycle statuses: %w", err)
	}
	if _, err := q.BumpIssueLifecycleRevision(ctx, db.BumpIssueLifecycleRevisionParams{
		ID: lifecycle.ID, WorkspaceID: workspaceID,
	}); err != nil {
		return fmt.Errorf("bump issue lifecycle revision: %w", err)
	}
	return nil
}

// TransitionActor is persisted on the immutable transition record. ActorID is
// nullable only for system and integration transitions that have no principal.
type TransitionActor struct {
	Type string
	ID   pgtype.UUID
}

// RecordTransition appends the immutable transition corresponding to a
// committed-in-this-transaction issue mutation, then pins its ID on the issue.
// It performs no work when the legacy status did not change.
func RecordTransition(
	ctx context.Context,
	q Querier,
	previous *db.Issue,
	current db.Issue,
	actor TransitionActor,
	cause string,
) (db.Issue, db.IssueTransition, bool, error) {
	if previous != nil && previous.Status == current.Status {
		return current, db.IssueTransition{}, false, nil
	}
	if actor.Type == "" {
		actor.Type = "system"
	}
	if cause == "" {
		cause = "status_update"
	}

	if !current.LifecycleID.Valid || !current.LifecycleStatusID.Valid {
		// A workspace written by an older rolling-deploy binary may not have a
		// default lifecycle yet. Bootstrap it lazily before binding the issue;
		// both operations are idempotent and remain inside the caller's tx.
		if err := q.SeedIssueStatusEntries(ctx, current.WorkspaceID); err != nil {
			return db.Issue{}, db.IssueTransition{}, false, fmt.Errorf("seed issue status catalog: %w", err)
		}
		if _, err := EnsureDefault(ctx, q, current.WorkspaceID); err != nil {
			return db.Issue{}, db.IssueTransition{}, false, fmt.Errorf("ensure issue lifecycle: %w", err)
		}
		bound, err := q.BindIssueToDefaultLifecycle(ctx, db.BindIssueToDefaultLifecycleParams{
			IssueID: current.ID, WorkspaceID: current.WorkspaceID,
		})
		if err != nil {
			return db.Issue{}, db.IssueTransition{}, false, fmt.Errorf("bind issue lifecycle: %w", err)
		}
		current = bound
	}

	lifecycle, err := q.GetIssueLifecycleByID(ctx, db.GetIssueLifecycleByIDParams{
		ID: current.LifecycleID, WorkspaceID: current.WorkspaceID,
	})
	if err != nil {
		return db.Issue{}, db.IssueTransition{}, false, fmt.Errorf("load issue lifecycle: %w", err)
	}
	toStatus, err := q.GetIssueLifecycleStatusByLegacyKey(ctx, db.GetIssueLifecycleStatusByLegacyKeyParams{
		WorkspaceID: current.WorkspaceID, LifecycleID: current.LifecycleID,
		LegacyStatusKey: pgtype.Text{String: current.Status, Valid: true},
	})
	if err != nil {
		return db.Issue{}, db.IssueTransition{}, false, fmt.Errorf("resolve lifecycle status %q: %w", current.Status, err)
	}
	if current.LifecycleStatusID != toStatus.ID {
		return db.Issue{}, db.IssueTransition{}, false, errors.New("issue lifecycle status projection is inconsistent")
	}

	fromStatusID := pgtype.UUID{}
	revisionBefore := current.Revision
	if previous != nil {
		revisionBefore = previous.Revision
		if previous.LifecycleStatusID.Valid {
			fromStatusID = previous.LifecycleStatusID
		} else {
			fromLifecycleID := previous.LifecycleID
			if !fromLifecycleID.Valid {
				// A rolling-deploy writer may have left the previous issue
				// completely unbound. The lifecycle bootstrapped above contains
				// the same legacy catalog, so it can still resolve the from-node.
				fromLifecycleID = current.LifecycleID
			}
			fromStatus, resolveErr := q.GetIssueLifecycleStatusByLegacyKey(ctx, db.GetIssueLifecycleStatusByLegacyKeyParams{
				WorkspaceID: previous.WorkspaceID, LifecycleID: fromLifecycleID,
				LegacyStatusKey: pgtype.Text{String: previous.Status, Valid: true},
			})
			if resolveErr == nil {
				fromStatusID = fromStatus.ID
			}
		}
	}

	transitionID := dbid.NewV7()
	_, err = q.InsertIssueTransition(ctx, db.InsertIssueTransitionParams{
		ID: transitionID, WorkspaceID: current.WorkspaceID, IssueID: current.ID,
		LifecycleID: current.LifecycleID, LifecycleRevision: lifecycle.Revision,
		FromStatusID: fromStatusID, ToStatusID: toStatus.ID,
		ActorType: actor.Type, ActorID: actor.ID, Cause: cause,
		IssueRevisionBefore: revisionBefore, IssueRevisionAfter: current.Revision,
	})
	if err != nil {
		return db.Issue{}, db.IssueTransition{}, false, fmt.Errorf("record issue transition: %w", err)
	}
	transition, err := q.GetIssueTransitionByRevision(ctx, db.GetIssueTransitionByRevisionParams{
		IssueID: current.ID, IssueRevisionAfter: current.Revision,
	})
	if err != nil {
		return db.Issue{}, db.IssueTransition{}, false, fmt.Errorf("load issue transition: %w", err)
	}
	current, err = q.SetIssueLastTransition(ctx, db.SetIssueLastTransitionParams{
		ID: current.ID, WorkspaceID: current.WorkspaceID, LastTransitionID: transition.ID,
	})
	if err != nil {
		return db.Issue{}, db.IssueTransition{}, false, fmt.Errorf("pin issue transition: %w", err)
	}
	return current, transition, true, nil
}
