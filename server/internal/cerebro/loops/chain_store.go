package loops

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"
)

// ErrPhaseLimitReached is returned after a phase limit has atomically marked
// the phase run failed. Callers must stop dispatching the chain.
var ErrPhaseLimitReached = errors.New("phase limit reached")

// PhaseLimitError identifies the limit that failed a phase run.
type PhaseLimitError struct {
	Reason string
}

func (e *PhaseLimitError) Error() string { return e.Reason }
func (e *PhaseLimitError) Unwrap() error { return ErrPhaseLimitReached }

// StepStatus is the persisted lifecycle of one runtime instance of a block.
type StepStatus string

const (
	StepPending   StepStatus = "pending"
	StepRunning   StepStatus = "running"
	StepWaiting   StepStatus = "waiting"
	StepCompleted StepStatus = "completed"
	StepFailed    StepStatus = "failed"
)

// PhaseStatus is the persisted lifecycle of one phase in a chain run.
type PhaseStatus string

const (
	PhaseRunning   PhaseStatus = "running"
	PhaseCompleted PhaseStatus = "completed"
	PhaseFailed    PhaseStatus = "failed"
)

// PhaseRunKey identifies one phase execution for one issue and workflow.
type PhaseRunKey struct {
	IssueID    pgtype.UUID
	WorkflowID pgtype.UUID
	PhaseID    string
}

// StepRef identifies one runtime instance of a block.
type StepRef struct {
	PhaseRunKey
	BlockID string
	Number  int32
}

// ChainStep is the persisted status and outcome of one block step.
type ChainStep struct {
	StepRef
	Status    StepStatus
	Outcome   json.RawMessage
	CreatedAt time.Time
}

// PhaseRunState holds the independent counters that bound a phase. A limit
// failure is durable: once Status is failed, later calls cannot reopen it.
type PhaseRunState struct {
	PhaseRunKey
	StepsOpened          int32
	RoundsUsed           int32
	ConsecutiveStalls    int32
	LastOutcomeSignature string
	Status               PhaseStatus
	FailureReason        string
}

// OpenStep atomically opens a block step and consumes one phase step slot.
// Retrying the same StepRef is idempotent and does not consume another slot.
func (s *Store) OpenStep(ctx context.Context, ref StepRef, limits PhaseLimits) (ChainStep, PhaseRunState, error) {
	if err := validateStepRef(ref); err != nil {
		return ChainStep{}, PhaseRunState{}, err
	}
	if err := validateStoredLimits(limits); err != nil {
		return ChainStep{}, PhaseRunState{}, err
	}

	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return ChainStep{}, PhaseRunState{}, fmt.Errorf("begin open loop step: %w", err)
	}
	defer func() { _ = tx.Rollback(ctx) }()

	state, err := ensureLockedPhaseRun(ctx, tx, ref.PhaseRunKey)
	if err != nil {
		return ChainStep{}, PhaseRunState{}, err
	}
	if state.Status == PhaseFailed {
		if err := tx.Commit(ctx); err != nil {
			return ChainStep{}, PhaseRunState{}, fmt.Errorf("commit failed loop phase: %w", err)
		}
		return ChainStep{}, state, &PhaseLimitError{Reason: state.FailureReason}
	}

	existing, err := loadStepTx(ctx, tx, ref)
	if err == nil {
		if err := tx.Commit(ctx); err != nil {
			return ChainStep{}, PhaseRunState{}, fmt.Errorf("commit existing loop step: %w", err)
		}
		return existing, state, nil
	}
	if !errors.Is(err, pgx.ErrNoRows) {
		return ChainStep{}, PhaseRunState{}, err
	}

	nextSteps := state.StepsOpened + 1
	if nextSteps > int32(limits.MaxSteps) {
		reason := fmt.Sprintf("max_steps exceeded (%d)", limits.MaxSteps)
		state, err = failPhaseRun(ctx, tx, state, reason)
		if err != nil {
			return ChainStep{}, PhaseRunState{}, err
		}
		if err := tx.Commit(ctx); err != nil {
			return ChainStep{}, PhaseRunState{}, fmt.Errorf("commit max_steps failure: %w", err)
		}
		return ChainStep{}, state, &PhaseLimitError{Reason: reason}
	}

	step := ChainStep{StepRef: ref, Status: StepPending, Outcome: json.RawMessage(`{}`)}
	if err := tx.QueryRow(ctx,
		`INSERT INTO cerebro_loop_step
		 (issue_id, workflow_id, phase_id, block_id, step_number, status, outcome)
		 VALUES ($1, $2, $3, $4, $5, $6, $7)
		 RETURNING created_at`,
		ref.IssueID, ref.WorkflowID, ref.PhaseID, ref.BlockID, ref.Number,
		step.Status, []byte(step.Outcome)).Scan(&step.CreatedAt); err != nil {
		return ChainStep{}, PhaseRunState{}, fmt.Errorf("insert loop step: %w", err)
	}
	state.StepsOpened = nextSteps
	if _, err := tx.Exec(ctx,
		`UPDATE cerebro_loop_phase_state
		 SET steps_opened = $4, updated_at = now()
		 WHERE issue_id = $1 AND workflow_id = $2 AND phase_id = $3`,
		ref.IssueID, ref.WorkflowID, ref.PhaseID, state.StepsOpened); err != nil {
		return ChainStep{}, PhaseRunState{}, fmt.Errorf("count opened loop step: %w", err)
	}
	if err := tx.Commit(ctx); err != nil {
		return ChainStep{}, PhaseRunState{}, fmt.Errorf("commit open loop step: %w", err)
	}
	return step, state, nil
}

// OpenNextStep lets the currently running instance of a steps-enabled block
// open exactly its successor. The block limit and the phase-wide step limit
// are checked under the same phase-row lock; crossing either one fails the
// phase durably. Repeating the same request returns the existing successor
// without consuming another slot.
func (s *Store) OpenNextStep(ctx context.Context, current StepRef, steps StepsConfig, limits PhaseLimits) (ChainStep, PhaseRunState, error) {
	if err := validateStepRef(current); err != nil {
		return ChainStep{}, PhaseRunState{}, err
	}
	if !steps.Allowed || steps.Max <= 0 {
		return ChainStep{}, PhaseRunState{}, errors.New("block does not allow additional steps")
	}
	if err := validateStoredLimits(limits); err != nil {
		return ChainStep{}, PhaseRunState{}, err
	}

	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return ChainStep{}, PhaseRunState{}, fmt.Errorf("begin open next loop step: %w", err)
	}
	defer func() { _ = tx.Rollback(ctx) }()

	state, err := ensureLockedPhaseRun(ctx, tx, current.PhaseRunKey)
	if err != nil {
		return ChainStep{}, PhaseRunState{}, err
	}
	if state.Status == PhaseFailed {
		if err := tx.Commit(ctx); err != nil {
			return ChainStep{}, PhaseRunState{}, fmt.Errorf("commit failed loop phase: %w", err)
		}
		return ChainStep{}, state, &PhaseLimitError{Reason: state.FailureReason}
	}
	if _, err := loadStepTx(ctx, tx, current); err != nil {
		return ChainStep{}, PhaseRunState{}, fmt.Errorf("load current loop step: %w", err)
	}

	nextRef := current
	nextRef.Number++
	if existing, err := loadStepTx(ctx, tx, nextRef); err == nil {
		if err := tx.Commit(ctx); err != nil {
			return ChainStep{}, PhaseRunState{}, fmt.Errorf("commit existing next loop step: %w", err)
		}
		return existing, state, nil
	} else if !errors.Is(err, pgx.ErrNoRows) {
		return ChainStep{}, PhaseRunState{}, err
	}

	reason := ""
	if nextRef.Number > int32(steps.Max) {
		reason = fmt.Sprintf("block %s steps.max exceeded (%d)", current.BlockID, steps.Max)
	} else if state.StepsOpened+1 > int32(limits.MaxSteps) {
		reason = fmt.Sprintf("max_steps exceeded (%d)", limits.MaxSteps)
	}
	if reason != "" {
		state, err = failPhaseRun(ctx, tx, state, reason)
		if err != nil {
			return ChainStep{}, PhaseRunState{}, err
		}
		if err := tx.Commit(ctx); err != nil {
			return ChainStep{}, PhaseRunState{}, fmt.Errorf("commit next step limit failure: %w", err)
		}
		return ChainStep{}, state, &PhaseLimitError{Reason: reason}
	}

	next := ChainStep{StepRef: nextRef, Status: StepPending, Outcome: json.RawMessage(`{}`)}
	if err := tx.QueryRow(ctx,
		`INSERT INTO cerebro_loop_step
		 (issue_id, workflow_id, phase_id, block_id, step_number, status, outcome)
		 VALUES ($1, $2, $3, $4, $5, $6, $7)
		 RETURNING created_at`,
		nextRef.IssueID, nextRef.WorkflowID, nextRef.PhaseID, nextRef.BlockID, nextRef.Number,
		next.Status, []byte(next.Outcome)).Scan(&next.CreatedAt); err != nil {
		return ChainStep{}, PhaseRunState{}, fmt.Errorf("insert next loop step: %w", err)
	}
	state.StepsOpened++
	if _, err := tx.Exec(ctx,
		`UPDATE cerebro_loop_phase_state
		 SET steps_opened = $4, updated_at = now()
		 WHERE issue_id = $1 AND workflow_id = $2 AND phase_id = $3`,
		current.IssueID, current.WorkflowID, current.PhaseID, state.StepsOpened); err != nil {
		return ChainStep{}, PhaseRunState{}, fmt.Errorf("count next loop step: %w", err)
	}
	if err := tx.Commit(ctx); err != nil {
		return ChainStep{}, PhaseRunState{}, fmt.Errorf("commit next loop step: %w", err)
	}
	return next, state, nil
}

// RecordStepOutcome updates one existing step. Outcome must be a JSON object,
// array, or primitive; its type is owned by the block that produced it.
func (s *Store) RecordStepOutcome(ctx context.Context, ref StepRef, status StepStatus, outcome json.RawMessage) error {
	if err := validateStepRef(ref); err != nil {
		return err
	}
	if !validStepStatus(status) {
		return fmt.Errorf("invalid loop step status %q", status)
	}
	if len(outcome) == 0 {
		outcome = json.RawMessage(`{}`)
	}
	if !json.Valid(outcome) {
		return errors.New("loop step outcome must be valid JSON")
	}
	result, err := s.pool.Exec(ctx,
		`UPDATE cerebro_loop_step
		 SET status = $6, outcome = $7, updated_at = now()
		 WHERE issue_id = $1 AND workflow_id = $2 AND phase_id = $3
		   AND block_id = $4 AND step_number = $5`,
		ref.IssueID, ref.WorkflowID, ref.PhaseID, ref.BlockID, ref.Number,
		status, []byte(outcome))
	if err != nil {
		return fmt.Errorf("record loop step outcome: %w", err)
	}
	if result.RowsAffected() == 0 {
		return fmt.Errorf("record loop step outcome: %w", pgx.ErrNoRows)
	}
	return nil
}

// ClaimStep atomically moves one pending step to running. Only the caller that
// receives claimed=true may dispatch it; concurrent Advance calls observe the
// running row and wait instead of enqueueing duplicate work.
func (s *Store) ClaimStep(ctx context.Context, ref StepRef) (step ChainStep, claimed bool, err error) {
	if err := validateStepRef(ref); err != nil {
		return ChainStep{}, false, err
	}
	step = ChainStep{StepRef: ref}
	var outcome []byte
	err = s.pool.QueryRow(ctx,
		`UPDATE cerebro_loop_step
		 SET status = $6, updated_at = now()
		 WHERE issue_id = $1 AND workflow_id = $2 AND phase_id = $3
		   AND block_id = $4 AND step_number = $5 AND status = $7
		 RETURNING status, outcome, created_at`,
		ref.IssueID, ref.WorkflowID, ref.PhaseID, ref.BlockID, ref.Number,
		StepRunning, StepPending).Scan(&step.Status, &outcome, &step.CreatedAt)
	if err == nil {
		step.Outcome = json.RawMessage(outcome)
		return step, true, nil
	}
	if !errors.Is(err, pgx.ErrNoRows) {
		return ChainStep{}, false, fmt.Errorf("claim loop step: %w", err)
	}
	err = s.pool.QueryRow(ctx,
		`SELECT status, outcome, created_at FROM cerebro_loop_step
		 WHERE issue_id = $1 AND workflow_id = $2 AND phase_id = $3
		   AND block_id = $4 AND step_number = $5`,
		ref.IssueID, ref.WorkflowID, ref.PhaseID, ref.BlockID, ref.Number).
		Scan(&step.Status, &outcome, &step.CreatedAt)
	if err != nil {
		return ChainStep{}, false, fmt.Errorf("load claimed loop step: %w", err)
	}
	step.Outcome = json.RawMessage(outcome)
	return step, false, nil
}

// ListSteps returns all persisted steps for a phase run in creation order.
func (s *Store) ListSteps(ctx context.Context, key PhaseRunKey) ([]ChainStep, error) {
	if err := validatePhaseRunKey(key); err != nil {
		return nil, err
	}
	rows, err := s.pool.Query(ctx,
		`SELECT block_id, step_number, status, outcome, created_at
		 FROM cerebro_loop_step
		 WHERE issue_id = $1 AND workflow_id = $2 AND phase_id = $3
		 ORDER BY created_at ASC, block_id ASC, step_number ASC`,
		key.IssueID, key.WorkflowID, key.PhaseID)
	if err != nil {
		return nil, fmt.Errorf("list loop steps: %w", err)
	}
	defer rows.Close()

	steps := []ChainStep{}
	for rows.Next() {
		step := ChainStep{StepRef: StepRef{PhaseRunKey: key}}
		var outcome []byte
		if err := rows.Scan(&step.BlockID, &step.Number, &step.Status, &outcome, &step.CreatedAt); err != nil {
			return nil, fmt.Errorf("scan loop step: %w", err)
		}
		step.Outcome = json.RawMessage(outcome)
		steps = append(steps, step)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate loop steps: %w", err)
	}
	return steps, nil
}

// RecordPhaseRound consumes one rework round and updates no-progress state.
// The counters are independent: exhausting either one fails the phase run.
func (s *Store) RecordPhaseRound(ctx context.Context, key PhaseRunKey, signature string, limits PhaseLimits) (PhaseRunState, error) {
	if err := validatePhaseRunKey(key); err != nil {
		return PhaseRunState{}, err
	}
	if err := validateStoredLimits(limits); err != nil {
		return PhaseRunState{}, err
	}
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return PhaseRunState{}, fmt.Errorf("begin loop phase round: %w", err)
	}
	defer func() { _ = tx.Rollback(ctx) }()

	state, err := ensureLockedPhaseRun(ctx, tx, key)
	if err != nil {
		return PhaseRunState{}, err
	}
	if state.Status == PhaseFailed {
		if err := tx.Commit(ctx); err != nil {
			return PhaseRunState{}, fmt.Errorf("commit failed loop phase: %w", err)
		}
		return state, &PhaseLimitError{Reason: state.FailureReason}
	}

	state.RoundsUsed++
	if signature != "" && signature == state.LastOutcomeSignature {
		state.ConsecutiveStalls++
	} else {
		state.ConsecutiveStalls = 0
	}
	state.LastOutcomeSignature = signature

	reason := ""
	switch {
	case state.RoundsUsed > int32(limits.MaxRounds):
		reason = fmt.Sprintf("max_rounds exceeded (%d)", limits.MaxRounds)
	case state.ConsecutiveStalls >= int32(limits.NoProgressStalls):
		reason = fmt.Sprintf("no_progress_stalls reached (%d)", limits.NoProgressStalls)
	}
	if reason != "" {
		state.Status = PhaseFailed
		state.FailureReason = reason
	}
	if _, err := tx.Exec(ctx,
		`UPDATE cerebro_loop_phase_state
		 SET rounds_used = $4, consecutive_stalls = $5,
		     last_outcome_signature = $6, status = $7, failure_reason = $8,
		     updated_at = now()
		 WHERE issue_id = $1 AND workflow_id = $2 AND phase_id = $3`,
		key.IssueID, key.WorkflowID, key.PhaseID, state.RoundsUsed,
		state.ConsecutiveStalls, state.LastOutcomeSignature, state.Status,
		state.FailureReason); err != nil {
		return PhaseRunState{}, fmt.Errorf("record loop phase round: %w", err)
	}
	if err := tx.Commit(ctx); err != nil {
		return PhaseRunState{}, fmt.Errorf("commit loop phase round: %w", err)
	}
	if reason != "" {
		return state, &PhaseLimitError{Reason: reason}
	}
	return state, nil
}

// LoadPhaseRun returns the durable counters and terminal state for one phase.
func (s *Store) LoadPhaseRun(ctx context.Context, key PhaseRunKey) (PhaseRunState, error) {
	if err := validatePhaseRunKey(key); err != nil {
		return PhaseRunState{}, err
	}
	state, err := scanPhaseRun(s.pool.QueryRow(ctx,
		`SELECT steps_opened, rounds_used, consecutive_stalls,
		        last_outcome_signature, status, failure_reason
		 FROM cerebro_loop_phase_state
		 WHERE issue_id = $1 AND workflow_id = $2 AND phase_id = $3`,
		key.IssueID, key.WorkflowID, key.PhaseID), key)
	if err != nil {
		return PhaseRunState{}, fmt.Errorf("load loop phase run: %w", err)
	}
	return state, nil
}

// CompletePhase marks a running phase completed. A failed phase is terminal
// and cannot be completed by a retry.
func (s *Store) CompletePhase(ctx context.Context, key PhaseRunKey) (PhaseRunState, error) {
	return s.setPhaseTerminal(ctx, key, PhaseCompleted, "")
}

// FailPhase marks a running phase failed with a durable reason.
func (s *Store) FailPhase(ctx context.Context, key PhaseRunKey, reason string) (PhaseRunState, error) {
	if reason == "" {
		return PhaseRunState{}, errors.New("failed loop phase needs a reason")
	}
	return s.setPhaseTerminal(ctx, key, PhaseFailed, reason)
}

func (s *Store) setPhaseTerminal(ctx context.Context, key PhaseRunKey, status PhaseStatus, reason string) (PhaseRunState, error) {
	if err := validatePhaseRunKey(key); err != nil {
		return PhaseRunState{}, err
	}
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return PhaseRunState{}, fmt.Errorf("begin terminal loop phase: %w", err)
	}
	defer func() { _ = tx.Rollback(ctx) }()
	state, err := ensureLockedPhaseRun(ctx, tx, key)
	if err != nil {
		return PhaseRunState{}, err
	}
	if state.Status == PhaseFailed && status != PhaseFailed {
		return PhaseRunState{}, &PhaseLimitError{Reason: state.FailureReason}
	}
	state.Status, state.FailureReason = status, reason
	if _, err := tx.Exec(ctx,
		`UPDATE cerebro_loop_phase_state
		 SET status = $4, failure_reason = $5, updated_at = now()
		 WHERE issue_id = $1 AND workflow_id = $2 AND phase_id = $3`,
		key.IssueID, key.WorkflowID, key.PhaseID, status, reason); err != nil {
		return PhaseRunState{}, fmt.Errorf("set terminal loop phase: %w", err)
	}
	if err := tx.Commit(ctx); err != nil {
		return PhaseRunState{}, fmt.Errorf("commit terminal loop phase: %w", err)
	}
	return state, nil
}

func ensureLockedPhaseRun(ctx context.Context, tx pgx.Tx, key PhaseRunKey) (PhaseRunState, error) {
	if _, err := tx.Exec(ctx,
		`INSERT INTO cerebro_loop_phase_state (issue_id, workflow_id, phase_id)
		 VALUES ($1, $2, $3)
		 ON CONFLICT (issue_id, workflow_id, phase_id) DO NOTHING`,
		key.IssueID, key.WorkflowID, key.PhaseID); err != nil {
		return PhaseRunState{}, fmt.Errorf("init loop phase run: %w", err)
	}
	state, err := scanPhaseRun(tx.QueryRow(ctx,
		`SELECT steps_opened, rounds_used, consecutive_stalls,
		        last_outcome_signature, status, failure_reason
		 FROM cerebro_loop_phase_state
		 WHERE issue_id = $1 AND workflow_id = $2 AND phase_id = $3
		 FOR UPDATE`, key.IssueID, key.WorkflowID, key.PhaseID), key)
	if err != nil {
		return PhaseRunState{}, fmt.Errorf("lock loop phase run: %w", err)
	}
	return state, nil
}

func failPhaseRun(ctx context.Context, tx pgx.Tx, state PhaseRunState, reason string) (PhaseRunState, error) {
	state.Status = PhaseFailed
	state.FailureReason = reason
	if _, err := tx.Exec(ctx,
		`UPDATE cerebro_loop_phase_state
		 SET status = $4, failure_reason = $5, updated_at = now()
		 WHERE issue_id = $1 AND workflow_id = $2 AND phase_id = $3`,
		state.IssueID, state.WorkflowID, state.PhaseID, state.Status, reason); err != nil {
		return PhaseRunState{}, fmt.Errorf("fail loop phase run: %w", err)
	}
	return state, nil
}

func loadStepTx(ctx context.Context, tx pgx.Tx, ref StepRef) (ChainStep, error) {
	step := ChainStep{StepRef: ref}
	var outcome []byte
	if err := tx.QueryRow(ctx,
		`SELECT status, outcome, created_at FROM cerebro_loop_step
		 WHERE issue_id = $1 AND workflow_id = $2 AND phase_id = $3
		   AND block_id = $4 AND step_number = $5`,
		ref.IssueID, ref.WorkflowID, ref.PhaseID, ref.BlockID, ref.Number).
		Scan(&step.Status, &outcome, &step.CreatedAt); err != nil {
		return ChainStep{}, err
	}
	step.Outcome = json.RawMessage(outcome)
	return step, nil
}

type rowScanner interface {
	Scan(dest ...any) error
}

func scanPhaseRun(row rowScanner, key PhaseRunKey) (PhaseRunState, error) {
	state := PhaseRunState{PhaseRunKey: key}
	if err := row.Scan(&state.StepsOpened, &state.RoundsUsed,
		&state.ConsecutiveStalls, &state.LastOutcomeSignature,
		&state.Status, &state.FailureReason); err != nil {
		return PhaseRunState{}, err
	}
	return state, nil
}

func validateStepRef(ref StepRef) error {
	if err := validatePhaseRunKey(ref.PhaseRunKey); err != nil {
		return err
	}
	if ref.BlockID == "" {
		return errors.New("loop step block id is required")
	}
	if ref.Number <= 0 {
		return errors.New("loop step number must be positive")
	}
	return nil
}

func validatePhaseRunKey(key PhaseRunKey) error {
	if !key.IssueID.Valid {
		return errors.New("loop phase issue id is required")
	}
	if !key.WorkflowID.Valid {
		return errors.New("loop phase workflow id is required")
	}
	if key.PhaseID == "" {
		return errors.New("loop phase id is required")
	}
	return nil
}

func validateStoredLimits(limits PhaseLimits) error {
	if limits.MaxSteps <= 0 || limits.MaxRounds <= 0 || limits.NoProgressStalls <= 0 {
		return errors.New("loop phase limits must all be positive")
	}
	return nil
}

func validStepStatus(status StepStatus) bool {
	switch status {
	case StepPending, StepRunning, StepWaiting, StepCompleted, StepFailed:
		return true
	default:
		return false
	}
}
