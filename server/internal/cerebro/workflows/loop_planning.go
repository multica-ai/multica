package workflows

// loop_planning.go wires the `loop_planning` toggle on a run_skill workflow
// (FIR-2283, packages/cerebro-workflows/views/workflow-form.tsx's "Planning
// mode" switch) into a real second dispatch rule.
//
// Before this file, ActionConfigRunSkill.LoopPlanning was persisted on the
// workflow row and read by nothing — the loop.yaml-driven compiler in the
// loops package (loops.Compile) is the only code that ever turned it into a
// loop:planning-dispatch rule, and nothing calls Compile from a workflow
// created through the form. workflows cannot import loops here either — loops
// already imports workflows to build Rules, so the reverse edge would be a
// cycle (see check_gate.go for the identical problem on the delivery gate).
// The dependency is inverted the same way: workflows declares the
// LoopPlanningMaterializer interface it needs, the loops package supplies a
// concrete implementation, and router.go plugs it in via
// Handler.WithLoopPlanningMaterializer.

import (
	"context"

	"github.com/jackc/pgx/v5/pgtype"
)

// LoopPlanningMaterializer creates the companion loop:planning-dispatch
// workflow row for a run_skill workflow whose action_config carries
// loop_planning=true. It is the seam Handler.Create calls; the loops package
// implements it (loops.PlanningDispatchRule, persisted via cerebrodb) without
// requiring a full authored loop.yaml spec — the planning rule only needs a
// worker agent and a skill, not goal/verification/caps.
type LoopPlanningMaterializer interface {
	CreatePlanningDispatch(ctx context.Context, workspaceID, createdByID pgtype.UUID, createdByType, agentID, skillName string) error
}

// WithLoopPlanningMaterializer plugs in the loop planning implementation.
// Optional and nil-safe: without it, the loop_planning toggle is stored on
// the workflow row but has no runtime effect (identical to the behavior
// before this wiring existed). Returns the receiver for chainable init.
func (h *Handler) WithLoopPlanningMaterializer(m LoopPlanningMaterializer) *Handler {
	h.loopPlanningMaterializer = m
	return h
}
