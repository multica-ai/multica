// Package issueworkflow owns the additive workflow projection introduced by
// MUL-7022. During rollout issue.status remains the compatibility handle while
// every write also pins the issue to a stable workflow status node.
package issueworkflow

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

// LegacyProjection keeps issue.status usable by older clients while
// workflow_status_id remains the source of truth for workflow-native nodes.
func LegacyProjection(status db.IssueWorkflowStatus) string {
	if status.LegacyStatusKey.Valid {
		return status.LegacyStatusKey.String
	}
	switch status.Phase {
	case PhaseBacklog:
		return "backlog"
	case PhaseUnstarted:
		return "todo"
	case PhaseCompleted:
		return "done"
	case PhaseCancelled:
		return "cancelled"
	default:
		return "in_progress"
	}
}

// Querier is the transaction-bound query surface used by workflow bootstrap
// and transition recording.
type Querier interface {
	EnsureDefaultIssueWorkflow(context.Context, pgtype.UUID) (db.IssueWorkflow, error)
	EnsureProjectIssueWorkflow(context.Context, db.EnsureProjectIssueWorkflowParams) (db.IssueWorkflow, error)
	SeedIssueStatusEntries(context.Context, pgtype.UUID) error
	SetWorkspaceDefaultIssueWorkflow(context.Context, db.SetWorkspaceDefaultIssueWorkflowParams) error
	SetProjectIssueWorkflow(context.Context, db.SetProjectIssueWorkflowParams) (db.Project, error)
	ClearProjectIssueWorkflow(context.Context, db.ClearProjectIssueWorkflowParams) (db.Project, error)
	SyncDefaultIssueWorkflowStatuses(context.Context, db.SyncDefaultIssueWorkflowStatusesParams) error
	SetDefaultIssueWorkflowInitialStatus(context.Context, db.SetDefaultIssueWorkflowInitialStatusParams) error
	GetDefaultIssueWorkflow(context.Context, pgtype.UUID) (db.IssueWorkflow, error)
	GetEffectiveIssueWorkflow(context.Context, db.GetEffectiveIssueWorkflowParams) (db.IssueWorkflow, error)
	GetIssueWorkflowByID(context.Context, db.GetIssueWorkflowByIDParams) (db.IssueWorkflow, error)
	BumpIssueWorkflowRevision(context.Context, db.BumpIssueWorkflowRevisionParams) (db.IssueWorkflow, error)
	CountIssueWorkflowStatuses(context.Context, db.CountIssueWorkflowStatusesParams) (int64, error)
	CloneIssueWorkflowStatuses(context.Context, db.CloneIssueWorkflowStatusesParams) (int64, error)
	GetIssueWorkflowStatusByLegacyKey(context.Context, db.GetIssueWorkflowStatusByLegacyKeyParams) (db.IssueWorkflowStatus, error)
	GetIssueWorkflowStatusByID(context.Context, db.GetIssueWorkflowStatusByIDParams) (db.IssueWorkflowStatus, error)
	BindIssueToDefaultWorkflow(context.Context, db.BindIssueToDefaultWorkflowParams) (db.Issue, error)
	BindIssueToWorkflowStatus(context.Context, db.BindIssueToWorkflowStatusParams) (db.Issue, error)
	InsertIssueTransition(context.Context, db.InsertIssueTransitionParams) (int64, error)
	GetIssueTransitionByRevision(context.Context, db.GetIssueTransitionByRevisionParams) (db.IssueTransition, error)
	SetIssueLastTransition(context.Context, db.SetIssueLastTransitionParams) (db.Issue, error)
}

// Effective resolves the workflow used by a newly created issue. A project
// with no override inherits the workspace default; existing issues never call
// this during ordinary reads, because their concrete workflow_id is pinned.
func Effective(ctx context.Context, q Querier, workspaceID, projectID pgtype.UUID) (db.IssueWorkflow, error) {
	workflow, err := q.GetEffectiveIssueWorkflow(ctx, db.GetEffectiveIssueWorkflowParams{
		WorkspaceID: workspaceID,
		ProjectID:   projectID,
	})
	if err != nil {
		return db.IssueWorkflow{}, fmt.Errorf("get effective issue workflow: %w", err)
	}
	return workflow, nil
}

// CustomizeProject creates (or reuses) one workflow owned by the project.
// The workspace default is cloned only for a brand-new, empty definition. A
// project that switches back to Use Default can later re-enable its previous
// custom definition without silently importing newer workspace nodes.
func CustomizeProject(ctx context.Context, q Querier, workspaceID, projectID pgtype.UUID) (db.IssueWorkflow, error) {
	workspaceDefault, err := q.GetDefaultIssueWorkflow(ctx, workspaceID)
	if err != nil {
		return db.IssueWorkflow{}, fmt.Errorf("get workspace default issue workflow: %w", err)
	}
	custom, err := q.EnsureProjectIssueWorkflow(ctx, db.EnsureProjectIssueWorkflowParams{
		ProjectID: projectID, WorkspaceID: workspaceID,
	})
	if err != nil {
		return db.IssueWorkflow{}, fmt.Errorf("ensure project issue workflow: %w", err)
	}
	count, err := q.CountIssueWorkflowStatuses(ctx, db.CountIssueWorkflowStatusesParams{
		WorkspaceID: workspaceID, WorkflowID: custom.ID,
	})
	if err != nil {
		return db.IssueWorkflow{}, fmt.Errorf("count project workflow statuses: %w", err)
	}
	if count == 0 {
		if _, err := q.CloneIssueWorkflowStatuses(ctx, db.CloneIssueWorkflowStatusesParams{
			WorkspaceID: workspaceID, SourceWorkflowID: workspaceDefault.ID, TargetWorkflowID: custom.ID,
		}); err != nil {
			return db.IssueWorkflow{}, fmt.Errorf("clone workspace workflow statuses: %w", err)
		}
	}
	if err := q.SetDefaultIssueWorkflowInitialStatus(ctx, db.SetDefaultIssueWorkflowInitialStatusParams{
		WorkspaceID: workspaceID, WorkflowID: custom.ID,
	}); err != nil {
		return db.IssueWorkflow{}, fmt.Errorf("set project workflow initial status: %w", err)
	}
	if _, err := q.SetProjectIssueWorkflow(ctx, db.SetProjectIssueWorkflowParams{
		ProjectID: projectID, WorkspaceID: workspaceID, WorkflowID: custom.ID,
	}); err != nil {
		return db.IssueWorkflow{}, fmt.Errorf("set project issue workflow: %w", err)
	}
	custom, err = q.GetIssueWorkflowByID(ctx, db.GetIssueWorkflowByIDParams{ID: custom.ID, WorkspaceID: workspaceID})
	if err != nil {
		return db.IssueWorkflow{}, fmt.Errorf("reload project issue workflow: %w", err)
	}
	return custom, nil
}

// UseWorkspaceDefault removes only the project's default pointer. Issues that
// were already created remain pinned to their original workflow and status.
func UseWorkspaceDefault(ctx context.Context, q Querier, workspaceID, projectID pgtype.UUID) error {
	if _, err := q.ClearProjectIssueWorkflow(ctx, db.ClearProjectIssueWorkflowParams{
		ProjectID: projectID, WorkspaceID: workspaceID,
	}); err != nil {
		return fmt.Errorf("clear project issue workflow: %w", err)
	}
	return nil
}

// EnsureDefault creates or repairs a workspace's shadow workflow projection.
// It is safe to call repeatedly and must run in the same transaction as a new
// workspace's legacy status seed.
func EnsureDefault(ctx context.Context, q Querier, workspaceID pgtype.UUID) (db.IssueWorkflow, error) {
	workflow, err := q.EnsureDefaultIssueWorkflow(ctx, workspaceID)
	if err != nil {
		return db.IssueWorkflow{}, fmt.Errorf("ensure default issue workflow: %w", err)
	}
	if err := q.SetWorkspaceDefaultIssueWorkflow(ctx, db.SetWorkspaceDefaultIssueWorkflowParams{
		ID: workspaceID, DefaultIssueWorkflowID: workflow.ID,
	}); err != nil {
		return db.IssueWorkflow{}, fmt.Errorf("set workspace default issue workflow: %w", err)
	}
	if err := q.SyncDefaultIssueWorkflowStatuses(ctx, db.SyncDefaultIssueWorkflowStatusesParams{
		WorkspaceID: workspaceID, WorkflowID: workflow.ID,
	}); err != nil {
		return db.IssueWorkflow{}, fmt.Errorf("sync default issue workflow statuses: %w", err)
	}
	if err := q.SetDefaultIssueWorkflowInitialStatus(ctx, db.SetDefaultIssueWorkflowInitialStatusParams{
		WorkspaceID: workspaceID, WorkflowID: workflow.ID,
	}); err != nil {
		return db.IssueWorkflow{}, fmt.Errorf("set default issue workflow initial status: %w", err)
	}
	workflow, err = q.GetIssueWorkflowByID(ctx, db.GetIssueWorkflowByIDParams{
		ID: workflow.ID, WorkspaceID: workspaceID,
	})
	if err != nil {
		return db.IssueWorkflow{}, fmt.Errorf("reload default issue workflow: %w", err)
	}
	return workflow, nil
}

// SyncDefault projects catalog edits (rename, reorder, archive) into the
// workspace's default workflow without changing any issue or starting work.
func SyncDefault(ctx context.Context, q Querier, workspaceID pgtype.UUID) error {
	workflow, err := q.GetDefaultIssueWorkflow(ctx, workspaceID)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			_, ensureErr := EnsureDefault(ctx, q, workspaceID)
			return ensureErr
		}
		return fmt.Errorf("get default issue workflow: %w", err)
	}
	if err := q.SyncDefaultIssueWorkflowStatuses(ctx, db.SyncDefaultIssueWorkflowStatusesParams{
		WorkspaceID: workspaceID, WorkflowID: workflow.ID,
	}); err != nil {
		return fmt.Errorf("sync default issue workflow statuses: %w", err)
	}
	if err := q.SetDefaultIssueWorkflowInitialStatus(ctx, db.SetDefaultIssueWorkflowInitialStatusParams{
		WorkspaceID: workspaceID, WorkflowID: workflow.ID,
	}); err != nil {
		return fmt.Errorf("set default issue workflow initial status: %w", err)
	}
	if _, err := q.BumpIssueWorkflowRevision(ctx, db.BumpIssueWorkflowRevisionParams{
		ID: workflow.ID, WorkspaceID: workspaceID,
	}); err != nil {
		return fmt.Errorf("bump issue workflow revision: %w", err)
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
	if previous != nil && previous.Status == current.Status &&
		previous.WorkflowID == current.WorkflowID && previous.WorkflowStatusID == current.WorkflowStatusID {
		return current, db.IssueTransition{}, false, nil
	}
	if actor.Type == "" {
		actor.Type = "system"
	}
	if cause == "" {
		cause = "status_update"
	}

	if !current.WorkflowID.Valid || !current.WorkflowStatusID.Valid {
		// A workspace written by an older rolling-deploy binary may not have a
		// default workflow yet. Bootstrap it lazily, then bind the issue to the
		// workflow effective for its project. Both operations are idempotent and
		// remain inside the caller's transaction.
		if err := q.SeedIssueStatusEntries(ctx, current.WorkspaceID); err != nil {
			return db.Issue{}, db.IssueTransition{}, false, fmt.Errorf("seed issue status catalog: %w", err)
		}
		if _, err := EnsureDefault(ctx, q, current.WorkspaceID); err != nil {
			return db.Issue{}, db.IssueTransition{}, false, fmt.Errorf("ensure issue workflow: %w", err)
		}
		workflow, err := Effective(ctx, q, current.WorkspaceID, current.ProjectID)
		if err != nil {
			return db.Issue{}, db.IssueTransition{}, false, err
		}
		status, err := q.GetIssueWorkflowStatusByLegacyKey(ctx, db.GetIssueWorkflowStatusByLegacyKeyParams{
			WorkspaceID: current.WorkspaceID, WorkflowID: workflow.ID,
			LegacyStatusKey: pgtype.Text{String: current.Status, Valid: true},
		})
		if err != nil {
			return db.Issue{}, db.IssueTransition{}, false, fmt.Errorf("resolve effective workflow status %q: %w", current.Status, err)
		}
		bound, err := q.BindIssueToWorkflowStatus(ctx, db.BindIssueToWorkflowStatusParams{
			IssueID: current.ID, WorkspaceID: current.WorkspaceID,
			WorkflowID: workflow.ID, WorkflowStatusID: status.ID,
		})
		if err != nil {
			return db.Issue{}, db.IssueTransition{}, false, fmt.Errorf("bind issue workflow: %w", err)
		}
		current = bound
	}

	workflow, err := q.GetIssueWorkflowByID(ctx, db.GetIssueWorkflowByIDParams{
		ID: current.WorkflowID, WorkspaceID: current.WorkspaceID,
	})
	if err != nil {
		return db.Issue{}, db.IssueTransition{}, false, fmt.Errorf("load issue workflow: %w", err)
	}
	toStatus, err := q.GetIssueWorkflowStatusByID(ctx, db.GetIssueWorkflowStatusByIDParams{
		WorkspaceID: current.WorkspaceID, WorkflowID: current.WorkflowID, ID: current.WorkflowStatusID,
	})
	if err != nil {
		return db.Issue{}, db.IssueTransition{}, false, fmt.Errorf("resolve workflow status: %w", err)
	}
	if current.Status != LegacyProjection(toStatus) {
		return db.Issue{}, db.IssueTransition{}, false, errors.New("issue workflow status projection is inconsistent")
	}

	fromStatusID := pgtype.UUID{}
	revisionBefore := current.Revision
	if previous != nil {
		revisionBefore = previous.Revision
		if previous.WorkflowStatusID.Valid {
			fromStatusID = previous.WorkflowStatusID
		} else {
			fromWorkflowID := previous.WorkflowID
			if !fromWorkflowID.Valid {
				// A rolling-deploy writer may have left the previous issue
				// completely unbound. The workflow bootstrapped above contains
				// the same legacy catalog, so it can still resolve the from-node.
				fromWorkflowID = current.WorkflowID
			}
			fromStatus, resolveErr := q.GetIssueWorkflowStatusByLegacyKey(ctx, db.GetIssueWorkflowStatusByLegacyKeyParams{
				WorkspaceID: previous.WorkspaceID, WorkflowID: fromWorkflowID,
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
		WorkflowID: current.WorkflowID, WorkflowRevision: workflow.Revision,
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
