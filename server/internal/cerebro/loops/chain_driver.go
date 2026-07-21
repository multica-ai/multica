package loops

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"
)

// ChainRun identifies one issue's execution of a workflow chain. AgentID is
// the issue-assignee fallback for blocks that do not name a runner yet; bid 4
// adds availability-aware selection across Block.Agents.
type ChainRun struct {
	IssueID     pgtype.UUID
	WorkflowID  pgtype.UUID
	IssueStatus string
	AgentID     string
}

// ChainDecisionKind is the one result a caller needs after advancing a chain.
type ChainDecisionKind string

const (
	// ChainWait means one block step is running or waiting for its outcome.
	ChainWait ChainDecisionKind = "wait"
	// ChainSetStatus asks the workflow engine to enter the next explicit phase.
	ChainSetStatus ChainDecisionKind = "set_status"
	// ChainDone means every phase and block completed.
	ChainDone ChainDecisionKind = "done"
	// ChainFailed means a step or phase failed durably.
	ChainFailed ChainDecisionKind = "failed"
)

// ChainDecision is the driver's complete public result. Step is set for a
// wait/failure caused by a block; Status is set for status transitions/done.
type ChainDecision struct {
	Kind   ChainDecisionKind
	Step   ChainStep
	Status string
	Reason string
}

// BlockDispatch is the single seam through which every block type leaves the
// chain driver. Adapters may enqueue an issue task or return a waiting state
// for an external decision such as a member approval or eval run.
type BlockDispatch struct {
	Run   ChainRun
	Phase Phase
	Block Block
	Step  ChainStep
}

// BlockDispatchResult is the durable state immediately after dispatch.
type BlockDispatchResult struct {
	Status  StepStatus
	Outcome json.RawMessage
}

// BlockDispatcher hides the five block-specific transports behind one deep
// interface. The driver owns ordering and persistence; the adapter only starts
// the selected block.
type BlockDispatcher interface {
	DispatchBlock(context.Context, BlockDispatch) (BlockDispatchResult, error)
}

// ChainDriver advances a chain until it must wait, change issue status, fail,
// or finish. Re-entering Advance is safe because completed steps and phases
// are read from the durable store instead of dispatched again.
type ChainDriver struct {
	store      *Store
	dispatcher BlockDispatcher
}

func NewChainDriver(store *Store, dispatcher BlockDispatcher) *ChainDriver {
	return &ChainDriver{store: store, dispatcher: dispatcher}
}

// Advance performs the one decision that moves a chain forward.
func (d *ChainDriver) Advance(ctx context.Context, chain *Chain, run ChainRun) (ChainDecision, error) {
	if d == nil || d.store == nil || d.dispatcher == nil {
		return ChainDecision{}, errors.New("chain driver needs a store and dispatcher")
	}
	if chain == nil {
		return ChainDecision{}, errors.New("chain is required")
	}
	if err := chain.Validate(); err != nil {
		return ChainDecision{}, fmt.Errorf("invalid chain: %w", err)
	}
	if !run.IssueID.Valid || !run.WorkflowID.Valid {
		return ChainDecision{}, errors.New("chain run needs issue and workflow ids")
	}

	for _, phase := range chain.Phases {
		key := PhaseRunKey{IssueID: run.IssueID, WorkflowID: run.WorkflowID, PhaseID: phase.ID}
		state, err := d.store.LoadPhaseRun(ctx, key)
		switch {
		case err == nil && state.Status == PhaseCompleted:
			continue
		case err == nil && state.Status == PhaseFailed:
			return ChainDecision{Kind: ChainFailed, Reason: state.FailureReason}, nil
		case err != nil && !errors.Is(err, pgx.ErrNoRows):
			return ChainDecision{}, err
		}

		if phase.Status != "" && phase.Status != run.IssueStatus {
			return ChainDecision{Kind: ChainSetStatus, Status: phase.Status}, nil
		}
		decision, complete, err := d.advancePhase(ctx, run, phase)
		if err != nil {
			return ChainDecision{}, err
		}
		if !complete {
			return decision, nil
		}
		if _, err := d.store.CompletePhase(ctx, key); err != nil {
			return ChainDecision{}, err
		}
	}

	status := chain.DoneStatus
	if status == "" {
		status = "done"
	}
	return ChainDecision{Kind: ChainDone, Status: status}, nil
}

func (d *ChainDriver) advancePhase(ctx context.Context, run ChainRun, phase Phase) (ChainDecision, bool, error) {
	key := PhaseRunKey{IssueID: run.IssueID, WorkflowID: run.WorkflowID, PhaseID: phase.ID}
	steps, err := d.store.ListSteps(ctx, key)
	if err != nil {
		return ChainDecision{}, false, err
	}
	byBlock := make(map[string][]ChainStep, len(steps))
	for _, step := range steps {
		byBlock[step.BlockID] = append(byBlock[step.BlockID], step)
	}

	for _, block := range phase.Blocks {
		var step ChainStep
		exists := false
		for _, candidate := range byBlock[block.ID] {
			if candidate.Status == StepCompleted {
				continue
			}
			step, exists = candidate, true
			break
		}
		if !exists && len(byBlock[block.ID]) > 0 {
			// Every opened instance of this block completed. Only then may the
			// driver advance to the next block in the phase.
			continue
		}
		if exists {
			switch step.Status {
			case StepFailed:
				reason := fmt.Sprintf("block %s failed", block.ID)
				if _, err := d.store.FailPhase(ctx, key, reason); err != nil {
					return ChainDecision{}, false, err
				}
				return ChainDecision{Kind: ChainFailed, Step: step, Reason: reason}, false, nil
			case StepRunning, StepWaiting:
				return ChainDecision{Kind: ChainWait, Step: step}, false, nil
			}
		} else {
			step, _, err = d.store.OpenStep(ctx, StepRef{PhaseRunKey: key, BlockID: block.ID, Number: 1}, phase.Limits)
			if err != nil {
				if errors.Is(err, ErrPhaseLimitReached) {
					return ChainDecision{Kind: ChainFailed, Reason: err.Error()}, false, nil
				}
				return ChainDecision{}, false, err
			}
		}
		step, claimed, err := d.store.ClaimStep(ctx, step.StepRef)
		if err != nil {
			return ChainDecision{}, false, err
		}
		if !claimed {
			if step.Status == StepFailed {
				reason := fmt.Sprintf("block %s failed", block.ID)
				if _, err := d.store.FailPhase(ctx, key, reason); err != nil {
					return ChainDecision{}, false, err
				}
				return ChainDecision{Kind: ChainFailed, Step: step, Reason: reason}, false, nil
			}
			return ChainDecision{Kind: ChainWait, Step: step}, false, nil
		}

		result, err := d.dispatcher.DispatchBlock(ctx, BlockDispatch{Run: run, Phase: phase, Block: block, Step: step})
		if err != nil {
			outcome, _ := json.Marshal(map[string]any{"error": err.Error()})
			_ = d.store.RecordStepOutcome(ctx, step.StepRef, StepFailed, outcome)
			_, _ = d.store.FailPhase(ctx, key, fmt.Sprintf("block %s dispatch failed", block.ID))
			return ChainDecision{}, false, fmt.Errorf("dispatch block %s: %w", block.ID, err)
		}
		if result.Status == "" {
			result.Status = StepRunning
		}
		if err := d.store.RecordStepOutcome(ctx, step.StepRef, result.Status, result.Outcome); err != nil {
			return ChainDecision{}, false, err
		}
		step.Status, step.Outcome = result.Status, result.Outcome
		switch result.Status {
		case StepCompleted:
			continue
		case StepFailed:
			reason := fmt.Sprintf("block %s failed", block.ID)
			if _, err := d.store.FailPhase(ctx, key, reason); err != nil {
				return ChainDecision{}, false, err
			}
			return ChainDecision{Kind: ChainFailed, Step: step, Reason: reason}, false, nil
		case StepPending, StepRunning, StepWaiting:
			return ChainDecision{Kind: ChainWait, Step: step}, false, nil
		default:
			return ChainDecision{}, false, fmt.Errorf("dispatch block %s returned invalid status %q", block.ID, result.Status)
		}
	}

	return ChainDecision{}, true, nil
}
