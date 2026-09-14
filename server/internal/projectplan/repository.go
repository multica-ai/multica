package projectplan

import (
	"context"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"
	db "github.com/multica-ai/multica/server/pkg/db/generated"
)

// Repository is the persistence boundary for plan writes. A transaction-bound
// copy must be used for every service mutation because the schema deliberately
// has no foreign keys or database cascades.
type Repository struct {
	queries *db.Queries
}

func NewRepository(database db.DBTX) *Repository {
	return &Repository{queries: db.New(database)}
}

func (r *Repository) withTx(tx pgx.Tx) *Repository {
	return &Repository{queries: r.queries.WithTx(tx)}
}

func (r *Repository) lockWorkspaceAndProject(
	ctx context.Context,
	workspaceID pgtype.UUID,
	projectID pgtype.UUID,
) error {
	if _, err := r.queries.LockWorkspaceForProjectPlanWrite(ctx, workspaceID); err != nil {
		return err
	}
	_, err := r.queries.LockProjectForProjectPlanWrite(ctx, db.LockProjectForProjectPlanWriteParams{
		ID: projectID, WorkspaceID: workspaceID,
	})
	return err
}

func (r *Repository) getPlan(ctx context.Context, workspaceID, planID pgtype.UUID) (db.ProjectPlan, error) {
	return r.queries.GetProjectPlanForWrite(ctx, db.GetProjectPlanForWriteParams{
		ID: planID, WorkspaceID: workspaceID,
	})
}

func (r *Repository) getActivePlan(
	ctx context.Context,
	workspaceID pgtype.UUID,
	projectID pgtype.UUID,
) (db.ProjectPlan, error) {
	return r.queries.GetActiveProjectPlanForWrite(ctx, db.GetActiveProjectPlanForWriteParams{
		ProjectID: projectID, WorkspaceID: workspaceID,
	})
}

func (r *Repository) createPlan(ctx context.Context, params db.CreateProjectPlanParams) (db.ProjectPlan, error) {
	return r.queries.CreateProjectPlan(ctx, params)
}

func (r *Repository) createPhase(ctx context.Context, params db.CreateProjectPlanPhaseParams) (db.ProjectPlanPhase, error) {
	return r.queries.CreateProjectPlanPhase(ctx, params)
}

func (r *Repository) createPart(ctx context.Context, params db.CreateProjectPlanPartParams) (db.ProjectPlanPart, error) {
	return r.queries.CreateProjectPlanPart(ctx, params)
}

func (r *Repository) createPartIssue(
	ctx context.Context,
	params db.CreateProjectPlanPartIssueParams,
) (db.ProjectPlanPartIssue, error) {
	return r.queries.CreateProjectPlanPartIssue(ctx, params)
}

func (r *Repository) deletePlanStructure(ctx context.Context, workspaceID, planID pgtype.UUID) error {
	if err := r.queries.DeleteProjectPlanDependencies(ctx, planID); err != nil {
		return err
	}
	if err := r.queries.DeleteProjectPlanPartIssues(ctx, planID); err != nil {
		return err
	}
	if err := r.queries.DeleteProjectPlanParts(ctx, planID); err != nil {
		return err
	}
	if err := r.queries.DeleteProjectPlanPhases(ctx, planID); err != nil {
		return err
	}
	return r.queries.DeleteProjectPlan(ctx, db.DeleteProjectPlanParams{ID: planID, WorkspaceID: workspaceID})
}

func (r *Repository) deletePhaseStructure(ctx context.Context, planID, phaseID pgtype.UUID) error {
	params := db.DeleteProjectPlanPhaseDependenciesParams{
		ProjectPlanID: planID, ProjectPlanPhaseID: phaseID,
	}
	if err := r.queries.DeleteProjectPlanPhaseDependencies(ctx, params); err != nil {
		return err
	}
	if err := r.queries.DeleteProjectPlanPhasePartIssues(ctx, db.DeleteProjectPlanPhasePartIssuesParams(params)); err != nil {
		return err
	}
	if err := r.queries.DeleteProjectPlanPhaseParts(ctx, db.DeleteProjectPlanPhasePartsParams(params)); err != nil {
		return err
	}
	return r.queries.DeleteProjectPlanPhase(ctx, db.DeleteProjectPlanPhaseParams{
		ID: phaseID, ProjectPlanID: planID,
	})
}

func (r *Repository) deletePartStructure(ctx context.Context, planID, partID pgtype.UUID) error {
	if err := r.queries.DeleteProjectPlanPartDependencies(ctx, db.DeleteProjectPlanPartDependenciesParams{
		ProjectPlanID: planID, BlockedPartID: partID,
	}); err != nil {
		return err
	}
	if err := r.queries.DeleteProjectPlanPartPartIssues(ctx, db.DeleteProjectPlanPartPartIssuesParams{
		ProjectPlanID: planID, ProjectPlanPartID: partID,
	}); err != nil {
		return err
	}
	return r.queries.DeleteProjectPlanPart(ctx, db.DeleteProjectPlanPartParams{
		ID: partID, ProjectPlanID: planID,
	})
}
