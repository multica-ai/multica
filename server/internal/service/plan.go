package service

import (
	"context"

	"github.com/jackc/pgx/v5/pgtype"
	db "github.com/multica-ai/multica/server/pkg/db/generated"
	util "github.com/multica-ai/multica/server/internal/util"
)

type PlanService struct {
	Queries   *db.Queries
	TxStarter TxStarter
}

type PlanOutput struct {
	ID          string  `json:"id"`
	WorkspaceID string  `json:"workspace_id"`
	CreatorID   string  `json:"creator_id"`
	Title       string  `json:"title"`
	Content     *string `json:"content"`
	Status      string  `json:"status"`
	WorkflowID  *string `json:"workflow_id"`
	CreatedAt   string  `json:"created_at"`
	UpdatedAt   string  `json:"updated_at"`
}

func (s *PlanService) Create(ctx context.Context, wsID, creatorID, title string, content *string) (PlanOutput, error) {
	tx, err := s.TxStarter.Begin(ctx)
	if err != nil {
		return PlanOutput{}, err
	}
	defer tx.Rollback(ctx)

	qtx := s.Queries.WithTx(tx)

	// 1. Create Plan with NULL workflow_id first
	plan, err := qtx.CreatePlan(ctx, db.CreatePlanParams{
		WorkspaceID: util.MustParseUUID(wsID),
		CreatorID:   util.MustParseUUID(creatorID),
		Title:       title,
		Content:     util.PtrToText(content),
		WorkflowID:  pgtype.UUID{Valid: false}, // NULL
	})
	if err != nil {
		return PlanOutput{}, err
	}

	// 2. Create Workflow referencing the plan
	wf, err := qtx.CreateWorkflow(ctx, db.CreateWorkflowParams{
		PlanID: plan.ID,
		Title:  title,
	})
	if err != nil {
		return PlanOutput{}, err
	}

	// 3. Update plan with workflow_id
	plan, err = qtx.UpdatePlan(ctx, db.UpdatePlanParams{
		ID:         plan.ID,
		Title:      pgtype.Text{Valid: false},
		Content:    pgtype.Text{Valid: false},
		Status:     pgtype.Text{Valid: false},
		WorkflowID: wf.ID,
	})
	if err != nil {
		return PlanOutput{}, err
	}

	if err := tx.Commit(ctx); err != nil {
		return PlanOutput{}, err
	}
	return planToOutput(plan), nil
}

func (s *PlanService) Get(ctx context.Context, planID pgtype.UUID) (PlanOutput, error) {
	plan, err := s.Queries.GetPlan(ctx, planID)
	if err != nil {
		return PlanOutput{}, err
	}
	return planToOutput(plan), nil
}

func (s *PlanService) List(ctx context.Context, wsID string) ([]PlanOutput, error) {
	plans, err := s.Queries.GetPlanByWorkspace(ctx, util.MustParseUUID(wsID))
	if err != nil {
		return nil, err
	}
	out := make([]PlanOutput, len(plans))
	for i, p := range plans {
		out[i] = planToOutput(p)
	}
	return out, nil
}

func (s *PlanService) Update(ctx context.Context, planID pgtype.UUID, title, content, status *string) (PlanOutput, error) {
	plan, err := s.Queries.UpdatePlan(ctx, db.UpdatePlanParams{
		ID:         planID,
		Title:      util.PtrToText(title),
		Content:    util.PtrToText(content),
		Status:     util.PtrToText(status),
		WorkflowID: pgtype.UUID{Valid: false},
	})
	if err != nil {
		return PlanOutput{}, err
	}
	return planToOutput(plan), nil
}

func planToOutput(p db.Plan) PlanOutput {
	return PlanOutput{
		ID:          util.UUIDToString(p.ID),
		WorkspaceID: util.UUIDToString(p.WorkspaceID),
		CreatorID:   util.UUIDToString(p.CreatorID),
		Title:       p.Title,
		Content:     util.TextToPtr(p.Content),
		Status:      p.Status,
		WorkflowID:  util.UUIDToPtr(p.WorkflowID),
		CreatedAt:   util.TimestampToString(p.CreatedAt),
		UpdatedAt:   util.TimestampToString(p.UpdatedAt),
	}
}
