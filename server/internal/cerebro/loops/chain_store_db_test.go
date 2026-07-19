package loops

import (
	"context"
	"encoding/json"
	"errors"
	"reflect"
	"strings"
	"testing"

	"github.com/jackc/pgx/v5/pgtype"
)

func seedLoopWorkflow(t *testing.T, issueID pgtype.UUID) pgtype.UUID {
	t.Helper()
	ctx := context.Background()
	var workflowID pgtype.UUID
	if err := loopTestPool.QueryRow(ctx,
		`INSERT INTO cerebro_workflow (
			workspace_id, name, trigger_type, trigger_config, conditions,
			action_type, action_config, created_by_id, created_by_type
		 )
		 SELECT workspace_id, 'Block chain state', 'status_changed', '{}'::jsonb,
		        '[]'::jsonb, 'set_status', '{}'::jsonb, gen_random_uuid(), 'member'
		 FROM issue WHERE id = $1
		 RETURNING id`, issueID).Scan(&workflowID); err != nil {
		t.Fatalf("create workflow: %v", err)
	}
	return workflowID
}

func TestChainStore_StepRoundTrip(t *testing.T) {
	if loopTestPool == nil {
		t.Skip("no test DB")
	}
	ctx := context.Background()
	issueID := seedIssue(t)
	workflowID := seedLoopWorkflow(t, issueID)
	store := NewStore(loopTestPool)
	ref := StepRef{
		PhaseRunKey: PhaseRunKey{IssueID: issueID, WorkflowID: workflowID, PhaseID: "build"},
		BlockID:     "compile",
		Number:      1,
	}
	limits := PhaseLimits{MaxSteps: 3, MaxRounds: 3, NoProgressStalls: 2}

	step, phase, err := store.OpenStep(ctx, ref, limits)
	if err != nil {
		t.Fatalf("open step: %v", err)
	}
	if step.Status != StepPending || phase.Status != PhaseRunning || phase.StepsOpened != 1 {
		t.Fatalf("unexpected initial state: step=%+v phase=%+v", step, phase)
	}

	outcome := json.RawMessage(`{"summary":"compiled","exit_code":0}`)
	if err := store.RecordStepOutcome(ctx, ref, StepCompleted, outcome); err != nil {
		t.Fatalf("record outcome: %v", err)
	}
	steps, err := store.ListSteps(ctx, ref.PhaseRunKey)
	if err != nil {
		t.Fatalf("list steps: %v", err)
	}
	var gotOutcome, wantOutcome any
	if len(steps) == 1 {
		_ = json.Unmarshal(steps[0].Outcome, &gotOutcome)
	}
	_ = json.Unmarshal(outcome, &wantOutcome)
	if len(steps) != 1 || steps[0].Status != StepCompleted || !reflect.DeepEqual(gotOutcome, wantOutcome) {
		t.Fatalf("unexpected persisted step: %+v", steps)
	}

	// Retrying the same open is idempotent: it returns the existing step and
	// never consumes another slot from the phase limit.
	_, phase, err = store.OpenStep(ctx, ref, limits)
	if err != nil {
		t.Fatalf("re-open step: %v", err)
	}
	if phase.StepsOpened != 1 {
		t.Fatalf("idempotent re-open counted twice: %+v", phase)
	}
}

func TestChainStore_OpenNextStepHonorsBlockAndPhaseLimits(t *testing.T) {
	if loopTestPool == nil {
		t.Skip("no test DB")
	}

	ctx := context.Background()
	issueID := seedIssue(t)
	workflowID := seedLoopWorkflow(t, issueID)
	store := NewStore(loopTestPool)
	limits := PhaseLimits{MaxSteps: 3, MaxRounds: 1, NoProgressStalls: 1}
	steps := StepsConfig{Allowed: true, Max: 2}
	firstRef := StepRef{
		PhaseRunKey: PhaseRunKey{IssueID: issueID, WorkflowID: workflowID, PhaseID: "build"},
		BlockID:     "build",
		Number:      1,
	}
	if _, _, err := store.OpenStep(ctx, firstRef, limits); err != nil {
		t.Fatalf("open first step: %v", err)
	}

	second, phase, err := store.OpenNextStep(ctx, firstRef, steps, limits)
	if err != nil {
		t.Fatalf("open next step: %v", err)
	}
	if second.Number != 2 || second.Status != StepPending || phase.StepsOpened != 2 {
		t.Fatalf("second step = %+v phase=%+v", second, phase)
	}
	// A repeated tool call is idempotent and does not consume another phase
	// slot before the agent reaches the next step.
	secondAgain, phase, err := store.OpenNextStep(ctx, firstRef, steps, limits)
	if err != nil || secondAgain.Number != 2 || phase.StepsOpened != 2 {
		t.Fatalf("repeat next step: step=%+v phase=%+v err=%v", secondAgain, phase, err)
	}

	_, failedPhase, err := store.OpenNextStep(ctx, second.StepRef, steps, limits)
	if !errors.Is(err, ErrPhaseLimitReached) {
		t.Fatalf("step over block max error = %v, want phase limit", err)
	}
	if failedPhase.Status != PhaseFailed || !strings.Contains(failedPhase.FailureReason, "steps.max") {
		t.Fatalf("phase did not fail durably at block max: %+v", failedPhase)
	}

	phaseIssueID := seedIssue(t)
	phaseWorkflowID := seedLoopWorkflow(t, phaseIssueID)
	phaseRef := StepRef{
		PhaseRunKey: PhaseRunKey{IssueID: phaseIssueID, WorkflowID: phaseWorkflowID, PhaseID: "build"},
		BlockID:     "build",
		Number:      1,
	}
	phaseLimits := PhaseLimits{MaxSteps: 1, MaxRounds: 1, NoProgressStalls: 1}
	if _, _, err := store.OpenStep(ctx, phaseRef, phaseLimits); err != nil {
		t.Fatalf("open phase-limited first step: %v", err)
	}
	_, failedPhase, err = store.OpenNextStep(ctx, phaseRef, StepsConfig{Allowed: true, Max: 3}, phaseLimits)
	if !errors.Is(err, ErrPhaseLimitReached) || !strings.Contains(failedPhase.FailureReason, "max_steps") {
		t.Fatalf("phase max was not enforced by next-step path: phase=%+v err=%v", failedPhase, err)
	}
}

func TestChainStore_PhaseLimitsFailIndependently(t *testing.T) {
	if loopTestPool == nil {
		t.Skip("no test DB")
	}

	tests := []struct {
		name       string
		limits     PhaseLimits
		exercise   func(context.Context, *Store, PhaseRunKey, PhaseLimits) error
		wantReason string
	}{
		{
			name:   "max steps",
			limits: PhaseLimits{MaxSteps: 1, MaxRounds: 10, NoProgressStalls: 10},
			exercise: func(ctx context.Context, store *Store, key PhaseRunKey, limits PhaseLimits) error {
				if _, _, err := store.OpenStep(ctx, StepRef{PhaseRunKey: key, BlockID: "build", Number: 1}, limits); err != nil {
					return err
				}
				_, _, err := store.OpenStep(ctx, StepRef{PhaseRunKey: key, BlockID: "build", Number: 2}, limits)
				return err
			},
			wantReason: "max_steps",
		},
		{
			name:   "max rounds",
			limits: PhaseLimits{MaxSteps: 10, MaxRounds: 1, NoProgressStalls: 10},
			exercise: func(ctx context.Context, store *Store, key PhaseRunKey, limits PhaseLimits) error {
				if _, err := store.RecordPhaseRound(ctx, key, "round-1", limits); err != nil {
					return err
				}
				_, err := store.RecordPhaseRound(ctx, key, "round-2", limits)
				return err
			},
			wantReason: "max_rounds",
		},
		{
			name:   "no progress stalls",
			limits: PhaseLimits{MaxSteps: 10, MaxRounds: 10, NoProgressStalls: 1},
			exercise: func(ctx context.Context, store *Store, key PhaseRunKey, limits PhaseLimits) error {
				if _, err := store.RecordPhaseRound(ctx, key, "unchanged", limits); err != nil {
					return err
				}
				_, err := store.RecordPhaseRound(ctx, key, "unchanged", limits)
				return err
			},
			wantReason: "no_progress_stalls",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			ctx := context.Background()
			issueID := seedIssue(t)
			key := PhaseRunKey{
				IssueID:    issueID,
				WorkflowID: seedLoopWorkflow(t, issueID),
				PhaseID:    strings.ReplaceAll(tt.name, " ", "-"),
			}
			store := NewStore(loopTestPool)

			err := tt.exercise(ctx, store, key, tt.limits)
			if !errors.Is(err, ErrPhaseLimitReached) {
				t.Fatalf("limit must fail the run with ErrPhaseLimitReached, got %v", err)
			}
			state, loadErr := store.LoadPhaseRun(ctx, key)
			if loadErr != nil {
				t.Fatalf("load failed phase: %v", loadErr)
			}
			if state.Status != PhaseFailed || !strings.Contains(state.FailureReason, tt.wantReason) {
				t.Fatalf("limit did not persist a failed run: %+v", state)
			}
		})
	}
}
