package runtime

import (
	"context"
	"encoding/json"
	"fmt"

	cerebroloops "github.com/multica-ai/multica/server/internal/cerebro/loops"
	"github.com/multica-ai/multica/server/internal/util"
	db "github.com/multica-ai/multica/server/pkg/db/generated"
)

// LoopStepCapability is derived from the current task's trusted context. It
// pins open_loop_step to one issue/workflow/phase/block instance and carries
// the limits authored in the chain, so the agent cannot widen either bound.
type LoopStepCapability struct {
	Current cerebroloops.StepRef
	Steps   cerebroloops.StepsConfig
	Limits  cerebroloops.PhaseLimits
}

// FirtalOpenLoopStepTool lets an agent ask for the successor of its current
// steps-enabled block instance. The tool takes no identity or limit fields;
// all authority comes from LoopStepCapability.
type FirtalOpenLoopStepTool struct {
	store      *cerebroloops.Store
	capability LoopStepCapability
}

func (t *FirtalOpenLoopStepTool) Name() string { return "open_loop_step" }
func (t *FirtalOpenLoopStepTool) Description() string {
	return "Open the next step of the workflow block you are currently running. " +
		"Use this only when the same block needs another agent session before it can complete. " +
		"The server enforces both the block step limit and the phase step limit."
}
func (t *FirtalOpenLoopStepTool) InputSchema() map[string]any {
	return map[string]any{
		"type":                 "object",
		"additionalProperties": false,
		"properties":           map[string]any{},
	}
}
func (t *FirtalOpenLoopStepTool) Call(ctx context.Context, _ map[string]any) (string, error) {
	step, phase, err := t.store.OpenNextStep(ctx, t.capability.Current, t.capability.Steps, t.capability.Limits)
	if err != nil {
		return "", fmt.Errorf("open_loop_step: %w", err)
	}
	raw, _ := json.Marshal(map[string]any{
		"opened":       true,
		"block_id":     step.BlockID,
		"step_number":  step.Number,
		"status":       step.Status,
		"steps_opened": phase.StepsOpened,
	})
	return string(raw), nil
}

func loopStepCapabilityFromTask(task db.AgentTaskQueue) *LoopStepCapability {
	if !task.IssueID.Valid || len(task.Context) == 0 {
		return nil
	}
	var envelope struct {
		LoopStep struct {
			WorkflowID string                   `json:"workflow_id"`
			PhaseID    string                   `json:"phase_id"`
			BlockID    string                   `json:"block_id"`
			StepNumber int32                    `json:"step_number"`
			Steps      cerebroloops.StepsConfig `json:"steps"`
			Limits     cerebroloops.PhaseLimits `json:"phase_limits"`
		} `json:"loop_step"`
	}
	if json.Unmarshal(task.Context, &envelope) != nil || !envelope.LoopStep.Steps.Allowed || envelope.LoopStep.Steps.Max <= 0 {
		return nil
	}
	workflowID, err := util.ParseUUID(envelope.LoopStep.WorkflowID)
	if err != nil || envelope.LoopStep.PhaseID == "" || envelope.LoopStep.BlockID == "" || envelope.LoopStep.StepNumber <= 0 {
		return nil
	}
	return &LoopStepCapability{
		Current: cerebroloops.StepRef{
			PhaseRunKey: cerebroloops.PhaseRunKey{
				IssueID: task.IssueID, WorkflowID: workflowID, PhaseID: envelope.LoopStep.PhaseID,
			},
			BlockID: envelope.LoopStep.BlockID,
			Number:  envelope.LoopStep.StepNumber,
		},
		Steps:  envelope.LoopStep.Steps,
		Limits: envelope.LoopStep.Limits,
	}
}
