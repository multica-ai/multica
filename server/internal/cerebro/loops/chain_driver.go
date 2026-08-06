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
	Run           ChainRun
	Phase         Phase
	Block         Block
	Step          ChainStep
	PreviousSteps []ChainStep
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

type chainStepLifecycle interface {
	AfterStepCompleted(context.Context, BlockDispatch, ChainStep) error
}

// ChainDriver advances a chain until it must wait, change issue status, fail,
// or finish. Re-entering Advance is safe because completed steps and phases
// are read from the durable store instead of dispatched again.
type ChainDriver struct {
	store      *Store
	dispatcher BlockDispatcher
	lifecycle  chainStepLifecycle
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

	previousSteps := make([]ChainStep, 0)
	for _, phase := range chain.Phases {
		key := PhaseRunKey{IssueID: run.IssueID, WorkflowID: run.WorkflowID, PhaseID: phase.ID}
		state, err := d.store.LoadPhaseRun(ctx, key)
		switch {
		case err == nil && state.Status == PhaseCompleted:
			steps, listErr := d.store.ListSteps(ctx, key)
			if listErr != nil {
				return ChainDecision{}, listErr
			}
			previousSteps = append(previousSteps, steps...)
			continue
		case err == nil && state.Status == PhaseFailed:
			return ChainDecision{Kind: ChainFailed, Reason: state.FailureReason}, nil
		case err != nil && !errors.Is(err, pgx.ErrNoRows):
			return ChainDecision{}, err
		}

		decision, complete, err := d.advancePhase(ctx, run, phase, previousSteps)
		if err != nil {
			return ChainDecision{}, err
		}
		if !complete {
			return decision, nil
		}
		if _, err := d.store.CompletePhase(ctx, key); err != nil {
			return ChainDecision{}, err
		}
		steps, err := d.store.ListSteps(ctx, key)
		if err != nil {
			return ChainDecision{}, err
		}
		previousSteps = append(previousSteps, steps...)
	}

	status := chain.DoneStatus
	if status == "" {
		status = "done"
	}
	return ChainDecision{Kind: ChainDone, Status: status}, nil
}

func (d *ChainDriver) advancePhase(ctx context.Context, run ChainRun, phase Phase, previousPhaseSteps []ChainStep) (ChainDecision, bool, error) {
	key := PhaseRunKey{IssueID: run.IssueID, WorkflowID: run.WorkflowID, PhaseID: phase.ID}
	steps, err := d.store.ListSteps(ctx, key)
	if err != nil {
		return ChainDecision{}, false, err
	}
	byBlock := make(map[string][]ChainStep, len(steps))
	for _, step := range steps {
		byBlock[step.BlockID] = append(byBlock[step.BlockID], step)
	}

	// pendingDone carries the finished block's StatusOnDone forward to the
	// next block boundary. Statuses are only ever applied where no step is
	// open, which is what makes them idempotent: re-entering Advance walks the
	// same boundary and skips the change once the issue already holds it.
	pendingDone := ""

	for index, block := range phase.Blocks {
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
			if block.StatusOnDone != "" {
				pendingDone = block.StatusOnDone
			}
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
			// No step of this block is open yet, so this is the boundary where
			// the previous block's exit status and this block's entry status
			// are applied — one status change per Advance, in that order.
			if pendingDone != "" && pendingDone != run.IssueStatus {
				return ChainDecision{Kind: ChainSetStatus, Status: pendingDone}, false, nil
			}
			// The phase status is the first block's entry status when that block
			// does not name one of its own. Keeping it to one decision is what
			// makes it terminate: two competing entry statuses at the same
			// boundary would each undo the other on every Advance.
			entry := block.StatusOnStart
			if entry == "" && index == 0 {
				entry = phase.Status
			}
			if entry != "" && entry != run.IssueStatus {
				return ChainDecision{Kind: ChainSetStatus, Status: entry}, false, nil
			}
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

		previousSteps := make([]ChainStep, 0, len(previousPhaseSteps)+len(steps))
		previousSteps = append(previousSteps, previousPhaseSteps...)
		previousSteps = append(previousSteps, steps...)
		dispatch := BlockDispatch{Run: run, Phase: phase, Block: block, Step: step, PreviousSteps: previousSteps}
		result, err := d.dispatcher.DispatchBlock(ctx, dispatch)
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
			if d.lifecycle != nil {
				if err := d.lifecycle.AfterStepCompleted(ctx, dispatch, step); err != nil {
					return ChainDecision{}, false, err
				}
			}
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

	// The last block's exit status still has to land before the phase counts as
	// complete, otherwise it would be swallowed by the next phase's own status.
	if pendingDone != "" && pendingDone != run.IssueStatus {
		return ChainDecision{Kind: ChainSetStatus, Status: pendingDone}, false, nil
	}

	return ChainDecision{}, true, nil
}
