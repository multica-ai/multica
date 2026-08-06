package loops

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"

	"github.com/multica-ai/multica/server/internal/cerebro/workflows"
	"github.com/multica-ai/multica/server/internal/util"
	db "github.com/multica-ai/multica/server/pkg/db/generated"
)

const finalStepApprovalRequirement = "Complete and approve the final required Workflow step before moving the issue to Done."

// WithHooks connects Chain v2 lifecycle events to the shared Workflow engine.
func (b *IssueLoopBridge) WithHooks(evaluator workflows.HookEvaluator) *IssueLoopBridge {
	b.hooks = evaluator
	b.driver.lifecycle = b
	return b
}

// BeforeIssueStatusChange is the shared status seam used by both Chain v2 and
// the normal issue API. It only adds Chain facts when the issue has an active
// Chain v2 binding; ordinary issues continue unchanged.
func (b *IssueLoopBridge) BeforeIssueStatusChange(ctx context.Context, issue db.Issue, proposed, actorType, actorID string) (string, error) {
	if b == nil || b.columns == nil || proposed == "" || proposed == issue.Status {
		return proposed, nil
	}
	if !ValidIssueStatus(proposed) {
		return "", fmt.Errorf("invalid issue status %q", proposed)
	}

	workflowID, active, err := b.columns.ActiveWorkflowForIssue(ctx, issue.ID)
	if err != nil {
		return "", err
	}
	if !active {
		return proposed, nil
	}
	fields, err := b.columns.Get(ctx, workflowID)
	if err != nil {
		return "", err
	}
	chain, err := decodeChain(fields.LoopSpec)
	if err != nil {
		return "", err
	}
	approved, err := b.chainApprovedForDone(ctx, issue.ID, workflowID, chain)
	if err != nil {
		return "", err
	}

	if b.hooks != nil {
		result, evalErr := b.hooks.Evaluate(ctx, workflows.HookEvent{
			EventID:     fmt.Sprintf("%s:status:%s:%s:%t", util.UUIDToString(issue.ID), issue.Status, proposed, approved),
			Type:        workflows.HookBeforeIssueStatus,
			WorkspaceID: util.UUIDToString(issue.WorkspaceID),
			ProjectID:   util.UUIDToString(issue.ProjectID),
			WorkflowID:  util.UUIDToString(workflowID),
			IssueID:     util.UUIDToString(issue.ID),
			Actor:       workflows.HookActor{Type: actorType, ID: actorID},
			Proposed:    map[string]any{"status": proposed},
			BeforeState: map[string]any{"status": issue.Status},
			Context: map[string]any{
				"status": map[string]any{"from": issue.Status, "to": proposed},
				"chain": map[string]any{
					"active": true, "approved_for_done": approved,
				},
			},
			MutableFields: []string{"status"},
		})
		if evalErr != nil {
			return "", fmt.Errorf("evaluate issue status Workflow: %w", evalErr)
		}
		if err := blockingHookError(result); err != nil {
			return "", err
		}
		if modified, ok := result.Modifications["status"].(string); ok {
			if !ValidIssueStatus(modified) {
				return "", fmt.Errorf("Workflow returned invalid issue status %q", modified)
			}
			proposed = modified
		}
	}

	// This is the non-configurable safety floor behind the managed Workflow
	// policy. A disabled rollout flag may suppress custom hooks, but it must
	// never make an active Chain finish without its durable final approval.
	if proposed == "done" && !approved {
		return "", errors.New(finalStepApprovalRequirement)
	}
	return proposed, nil
}

func (b *IssueLoopBridge) chainApprovedForDone(ctx context.Context, issueID, workflowID pgtype.UUID, chain *Chain) (bool, error) {
	if chain == nil || len(chain.Phases) == 0 {
		return false, nil
	}
	store := NewStore(b.pool)
	for _, phase := range chain.Phases {
		state, err := store.LoadPhaseRun(ctx, PhaseRunKey{IssueID: issueID, WorkflowID: workflowID, PhaseID: phase.ID})
		if errors.Is(err, pgx.ErrNoRows) || (err == nil && state.Status != PhaseCompleted) {
			return false, nil
		}
		if err != nil {
			return false, err
		}
	}

	finalPhase := chain.Phases[len(chain.Phases)-1]
	if len(finalPhase.Blocks) == 0 {
		return false, nil
	}
	finalBlock := finalPhase.Blocks[len(finalPhase.Blocks)-1]
	steps, err := store.ListSteps(ctx, PhaseRunKey{IssueID: issueID, WorkflowID: workflowID, PhaseID: finalPhase.ID})
	if err != nil {
		return false, err
	}
	var finalSteps []ChainStep
	for _, step := range steps {
		if step.BlockID == finalBlock.ID {
			finalSteps = append(finalSteps, step)
		}
	}
	if len(finalSteps) == 0 {
		return false, nil
	}
	for _, step := range finalSteps {
		if step.Status != StepCompleted {
			return false, nil
		}
	}
	if finalBlock.Type != BlockHuman {
		return true, nil
	}
	var outcome struct {
		Approved bool `json:"approved"`
	}
	if err := json.Unmarshal(finalSteps[len(finalSteps)-1].Outcome, &outcome); err != nil {
		return false, nil
	}
	return outcome.Approved, nil
}

// AfterStepCompleted emits the durable step outcome only after it has been
// stored. Raw step output stays out of the hook event; a short digest keeps
// retries idempotent without exposing command or model content.
func (b *IssueLoopBridge) AfterStepCompleted(ctx context.Context, dispatch BlockDispatch, step ChainStep) error {
	if b == nil || b.hooks == nil {
		return nil
	}
	issue, err := b.issues.GetIssue(ctx, dispatch.Run.IssueID)
	if err != nil {
		return fmt.Errorf("load issue for completed Workflow step: %w", err)
	}
	digest := sha256.Sum256(step.Outcome)
	result, err := b.hooks.Evaluate(ctx, workflows.HookEvent{
		EventID: fmt.Sprintf("%s:%s:step:%s:%s:%d:%s",
			util.UUIDToString(dispatch.Run.WorkflowID), util.UUIDToString(dispatch.Run.IssueID),
			dispatch.Phase.ID, dispatch.Block.ID, step.Number,
			hex.EncodeToString(digest[:8])),
		Type:        workflows.HookAfterWorkflowStep,
		WorkspaceID: util.UUIDToString(issue.WorkspaceID),
		ProjectID:   util.UUIDToString(issue.ProjectID),
		WorkflowID:  util.UUIDToString(dispatch.Run.WorkflowID),
		IssueID:     util.UUIDToString(issue.ID),
		AgentID:     dispatch.Run.AgentID,
		Context: map[string]any{"workflow": map[string]any{
			"phase_id":    dispatch.Phase.ID,
			"block_id":    dispatch.Block.ID,
			"block_type":  dispatch.Block.Type,
			"step_number": step.Number,
			"step_status": step.Status,
		}},
	})
	if err != nil {
		return fmt.Errorf("evaluate completed Workflow step: %w", err)
	}
	return blockingHookError(result)
}

func blockingHookError(result workflows.HookResult) error {
	return workflows.BlockingHookError(result)
}
