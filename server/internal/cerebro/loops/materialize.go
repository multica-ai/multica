package loops

// materialize.go implements workflows.LoopPlanningMaterializer: it persists
// PlanningDispatchRule as a real cerebro_workflow row. This is the
// FIR-2283 "Planning mode" wiring — the counterpart to check_gate.go, which
// wires the delivery gate the other direction (workflows -> loops). Here
// loops writes directly through cerebrodb (already an existing loops
// dependency via workflows -> ... no cycle: cerebrodb imports neither
// workflows nor loops), so workflows.Handler never needs to import loops.

import (
	"context"
	"encoding/json"
	"fmt"

	"github.com/jackc/pgx/v5/pgtype"

	cerebrodb "github.com/multica-ai/multica/server/internal/cerebro/db/generated"
	"github.com/multica-ai/multica/server/internal/cerebro/workflows"
)

// PlanningMaterializer implements workflows.LoopPlanningMaterializer over the
// generated cerebro DB queries.
type PlanningMaterializer struct {
	queries *cerebrodb.Queries
}

// NewPlanningMaterializer builds a PlanningMaterializer over the given
// generated queries.
func NewPlanningMaterializer(queries *cerebrodb.Queries) *PlanningMaterializer {
	return &PlanningMaterializer{queries: queries}
}

// CreatePlanningDispatch persists PlanningDispatchRule(agentID, skillName, "", "")
// as its own enabled cerebro_workflow row, owned by the same workspace/creator
// as the run_skill workflow that requested it. The standalone loop_planning
// toggle carries no authored spec, so planning/build statuses fall to their
// defaults (todo -> in_progress) inside PlanningDispatchRule.
func (m *PlanningMaterializer) CreatePlanningDispatch(ctx context.Context, workspaceID, createdByID pgtype.UUID, createdByType, agentID, skillName string) error {
	rule := PlanningDispatchRule(agentID, skillName, "", "")

	triggerConfigJSON, err := json.Marshal(rule.TriggerConfig)
	if err != nil {
		return fmt.Errorf("marshal planning-dispatch trigger config: %w", err)
	}
	actionConfigJSON, err := json.Marshal(rule.ActionConfig)
	if err != nil {
		return fmt.Errorf("marshal planning-dispatch action config: %w", err)
	}

	_, err = m.queries.CreateCerebroWorkflow(ctx, cerebrodb.CreateCerebroWorkflowParams{
		WorkspaceID:   workspaceID,
		Name:          rule.Name,
		Enabled:       true,
		TriggerType:   rule.TriggerType,
		TriggerConfig: triggerConfigJSON,
		Conditions:    []byte("[]"),
		ActionType:    rule.ActionType,
		ActionConfig:  actionConfigJSON,
		EditorMode:    workflows.EditorModeForm,
		EditorLayout:  []byte("null"),
		CreatedByID:   createdByID,
		CreatedByType: createdByType,
	})
	if err != nil {
		return fmt.Errorf("create planning-dispatch workflow: %w", err)
	}
	return nil
}
