package service

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"

	"github.com/multica-ai/multica/server/internal/automation"
	"github.com/multica-ai/multica/server/internal/domainevent"
	"github.com/multica-ai/multica/server/internal/util"
	db "github.com/multica-ai/multica/server/pkg/db/generated"
)

// The Event Hooks executor (MUL-4332 PR3 §7.2). It leases `queued` executions the
// matcher produced and runs their actions in order, resuming from
// current_action_index so a restart never re-runs work that already committed.
//
// EVERY action is anchored by a durable effect row keyed on (execution, action
// index). For a platform DB action the effect, the target write and the resulting
// domain event all commit in ONE transaction (§4), so the crash window between
// "action happened" and "we recorded that it happened" does not exist: either all
// three are durable or none are. A retry that finds a succeeded effect skips the
// action and reuses its recorded output rather than repeating it.
//
// OWNERSHIP is one predicate — right token, still `running`, and not expired under
// DATABASE clock time — asserted before any action write and re-applied to every
// status write. A worker whose lease was reclaimed, or whose own lease elapsed
// mid-action, commits nothing and can never write terminal state (§7.3).
//
// Actions are NOT collectively atomic, by design: action 1 succeeding and action 2
// finally failing is an explicit partial execution and action 1 is not rolled back
// (§7.2). The action cursor advances in the same transaction as the action, so the
// retry resumes precisely at the action that failed.
//
// This slice implements `set_issue_status`. `trigger_agent` needs the task enqueue
// path to become transaction-aware first, so its effect can bind in the same
// transaction as the enqueue (§4: "task / comment / inbox 可在同一事务内绑定
// effect"); until then it is left unimplemented rather than run without that
// guarantee. The executor loop is gated on the default-off automation_event_hooks
// flag, so production behaviour is unchanged.

const (
	hookExecRunning   = "running"
	hookExecSucceeded = "succeeded"

	// Terminal, non-retryable skip reasons (§7.3).
	skipTargetUnavailable = "target_unavailable"
	skipPrincipalInvalid  = "principal_invalid"
	skipActionUnsupported = "action_unsupported"

	// errCodeInfra marks an execution that exhausted its infrastructure retries.
	errCodeInfra = "infra"

	// hookDisabledPrincipalInvalid is recorded on a hook paused because its stored
	// authorization principal is no longer a workspace member.
	hookDisabledPrincipalInvalid = "principal_invalid"

	// ExecutorBatchSize bounds how many executions one executor tick runs.
	ExecutorBatchSize = 20
)

// ExecutorLeaseTTL bounds how long one execution may hold its lease. It is a var so
// tests can drive the expired-lease path deterministically.
var ExecutorLeaseTTL = 2 * time.Minute

// executorBackoff is the infrastructure-failure retry ladder (§7.3). An execution
// that exhausts it is marked failed.
var executorBackoff = []time.Duration{time.Minute, 5 * time.Minute, 15 * time.Minute}

// errExecutionLeaseLost means this worker no longer owns the execution — reclaimed,
// already finalized, or its own lease expired. It always aborts the action
// transaction, so a non-owner commits nothing and writes no terminal state.
var errExecutionLeaseLost = errors.New("hook executor: execution lease lost")

// actionSkip is a terminal, non-retryable action outcome: the rule cannot run as
// written and retrying would fail identically (§7.3). It finalizes the execution as
// skipped rather than consuming the infrastructure retry ladder.
type actionSkip struct {
	reason string
	detail string
}

func (e *actionSkip) Error() string { return e.reason + ": " + e.detail }

func skipAction(reason, format string, args ...any) *actionSkip {
	return &actionSkip{reason: reason, detail: fmt.Sprintf(format, args...)}
}

// effectKeyFor is the durable idempotency key of one action of one execution.
func effectKeyFor(executionID pgtype.UUID, index int) string {
	return fmt.Sprintf("%s:%d", util.UUIDToString(executionID), index)
}

// ClaimAndRun leases and runs up to batchSize executions, returning how many reached
// a terminal state in this tick.
func (s *HookService) ClaimAndRun(ctx context.Context, batchSize int32) (int, error) {
	finished := 0
	for i := int32(0); i < batchSize; i++ {
		claimed, done, err := s.claimAndRunOne(ctx)
		if err != nil {
			slog.Warn("hook executor: execution failed", "error", err)
			return finished, nil
		}
		if !claimed {
			break // queue drained
		}
		if done {
			finished++
		}
	}
	return finished, nil
}

// claimAndRunOne leases one execution and runs its remaining actions. It reports
// whether an execution was claimed and whether it reached a terminal state.
func (s *HookService) claimAndRunOne(ctx context.Context) (claimed bool, finished bool, err error) {
	lease := util.NewUUID()
	exec, err := s.Queries.ClaimOneHookExecution(ctx, db.ClaimOneHookExecutionParams{
		LeaseToken:      lease,
		LeaseTtlSeconds: ExecutorLeaseTTL.Seconds(),
	})
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return false, false, nil
		}
		return false, false, err
	}

	actions, err := s.executionActions(ctx, exec)
	if err != nil {
		// The revision cannot be read or parsed: deterministic, so retrying is
		// pointless. Record it terminally instead of burning the retry ladder.
		return true, s.finalizeSkippedAction(ctx, exec, lease, automation.ActionSpec{},
			int(exec.CurrentActionIndex), skipAction(skipActionUnsupported, "%v", err)), nil
	}

	for idx := int(exec.CurrentActionIndex); idx < len(actions); idx++ {
		// Renew before each action so a live worker running a long chain keeps its
		// claim, while an ABANDONED claim still expires on schedule.
		if _, err := s.Queries.HeartbeatHookExecution(ctx, db.HeartbeatHookExecutionParams{
			ID: exec.ID, LeaseToken: lease, LeaseTtlSeconds: ExecutorLeaseTTL.Seconds(),
		}); err != nil {
			return true, s.rescheduleOrFail(ctx, exec, lease, actions[idx], idx, err), nil
		}

		runErr := s.runAction(ctx, exec, lease, actions[idx], idx)
		if runErr == nil {
			continue
		}
		var skip *actionSkip
		switch {
		case errors.Is(runErr, errExecutionLeaseLost):
			// We wrote nothing. If the row still carries OUR token the lease simply
			// elapsed under us, so back it off: leaving it untouched keeps it at the
			// head of the queue, where the next claim selects it again immediately and
			// starves everything behind it. If another worker has reclaimed it the
			// token differs, nothing is written, and the new owner is left alone.
			return true, false, s.deferExpiredExecution(ctx, exec, lease)
		case errors.As(runErr, &skip):
			return true, s.finalizeSkippedAction(ctx, exec, lease, actions[idx], idx, skip), nil
		default:
			// Infrastructure failure: back off and resume at THIS action. Every
			// action already committed stays committed.
			return true, s.rescheduleOrFail(ctx, exec, lease, actions[idx], idx, runErr), nil
		}
	}

	rows, err := s.Queries.MarkHookExecutionSucceeded(ctx, db.MarkHookExecutionSucceededParams{
		ID: exec.ID, LeaseToken: lease,
	})
	if err != nil {
		return true, false, err
	}
	return true, rows == 1, nil
}

// executionActions loads the actions of the revision this execution was pinned to at
// match time. A later edit to the hook never changes what a created execution runs.
func (s *HookService) executionActions(ctx context.Context, exec db.HookExecution) ([]automation.ActionSpec, error) {
	rev, err := s.Queries.GetHookRevision(ctx, exec.HookRevisionID)
	if err != nil {
		return nil, err
	}
	var actions []automation.ActionSpec
	if len(rev.Actions) > 0 {
		if err := json.Unmarshal(rev.Actions, &actions); err != nil {
			return nil, fmt.Errorf("%w: parse stored actions: %v", automation.ErrInvalidConfig, err)
		}
	}
	return actions, nil
}

// runAction performs one action, records its effect, and advances the cursor — all
// in a single transaction, so the action and the record that it happened are never
// separable.
func (s *HookService) runAction(ctx context.Context, exec db.HookExecution, lease pgtype.UUID, action automation.ActionSpec, index int) error {
	effectKey := effectKeyFor(exec.ID, index)

	return domainevent.WriteInTx(ctx, s.TxStarter, s.Queries, func(qtx *db.Queries) ([]domainevent.Event, error) {
		// Ownership, fail-closed, before any write.
		owned, err := qtx.GetOwnedHookExecution(ctx, db.GetOwnedHookExecutionParams{ID: exec.ID, LeaseToken: lease})
		if err != nil {
			if errors.Is(err, pgx.ErrNoRows) {
				return nil, errExecutionLeaseLost
			}
			return nil, err
		}

		// An effect that already succeeded means this action is durably done; skip it
		// and just carry the cursor forward.
		existing, err := qtx.GetHookActionEffect(ctx, effectKey)
		switch {
		case err == nil && existing.Status == hookExecSucceeded:
			return nil, advanceCursor(ctx, qtx, owned, lease, index)
		case err != nil && !errors.Is(err, pgx.ErrNoRows):
			return nil, err
		}
		// A non-succeeded anchor is a previous terminal attempt (skipped/failed). This
		// retry re-attempts the action and the write below updates that same row.
		retryOfTerminal := err == nil

		// The principal's authority is re-checked for EVERY action, against live
		// membership, not the snapshot taken when the hook was saved (§8).
		if err := s.requireExecutionPrincipal(ctx, qtx, owned); err != nil {
			return nil, err
		}

		resolved, err := json.Marshal(action)
		if err != nil {
			return nil, err
		}
		// Claim the anchor. Losing the race means a concurrent attempt owns this
		// action; ownership above makes that a lost lease, so stop rather than run it
		// a second time.
		if _, err := qtx.CreateHookActionEffect(ctx, db.CreateHookActionEffectParams{
			ID:            util.NewUUID(),
			EffectKey:     effectKey,
			ExecutionID:   owned.ID,
			ActionIndex:   int32(index),
			ActionType:    action.Type,
			ResolvedInput: resolved,
		}); err != nil {
			if !errors.Is(err, pgx.ErrNoRows) {
				return nil, err
			}
			// ON CONFLICT DO NOTHING returned nothing. If we already saw a terminal
			// anchor this is our own retry and we continue; otherwise the anchor
			// appeared between our read and our write, so a concurrent attempt owns
			// this action and we must not run it a second time.
			if !retryOfTerminal {
				return nil, errExecutionLeaseLost
			}
		}

		events, outputType, outputID, err := s.performAction(ctx, qtx, owned, action, index)
		if err != nil {
			return nil, err
		}

		if _, err := qtx.MarkHookActionEffectSucceeded(ctx, db.MarkHookActionEffectSucceededParams{
			EffectKey:  effectKey,
			OutputType: pgtype.Text{String: outputType, Valid: outputType != ""},
			OutputID:   outputID,
		}); err != nil {
			return nil, err
		}
		if err := advanceCursor(ctx, qtx, owned, lease, index); err != nil {
			return nil, err
		}
		return events, nil
	})
}

func advanceCursor(ctx context.Context, qtx *db.Queries, exec db.HookExecution, lease pgtype.UUID, index int) error {
	rows, err := qtx.AdvanceHookExecutionAction(ctx, db.AdvanceHookExecutionActionParams{
		ID: exec.ID, LeaseToken: lease, NextActionIndex: int32(index + 1),
	})
	if err != nil {
		return err
	}
	if rows != 1 {
		return errExecutionLeaseLost
	}
	return nil
}

// requireExecutionPrincipal re-asserts that the hook's stored authorization
// principal is still a workspace member. A departed principal is terminal: the hook
// is paused so it stops producing work under authority nobody holds any more (§7.3).
func (s *HookService) requireExecutionPrincipal(ctx context.Context, qtx *db.Queries, exec db.HookExecution) error {
	hook, err := qtx.GetHookInWorkspace(ctx, db.GetHookInWorkspaceParams{ID: exec.HookID, WorkspaceID: exec.WorkspaceID})
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return skipAction(skipTargetUnavailable, "hook %s no longer exists", util.UUIDToString(exec.HookID))
		}
		return err
	}
	if !hook.AuthorizationPrincipalUserID.Valid {
		return skipAction(skipPrincipalInvalid, "hook %s has no authorization principal", util.UUIDToString(hook.ID))
	}
	if _, err := s.requireLivePrincipal(ctx, qtx, exec.WorkspaceID, hook.AuthorizationPrincipalUserID); err != nil {
		if errors.Is(err, ErrHookPrincipalDeparted) {
			// The pause itself is applied by the caller, AFTER this transaction rolls
			// back: writing it here would be undone by the very error that reports it.
			return skipAction(skipPrincipalInvalid,
				"hook %s authorization principal is no longer a workspace member", util.UUIDToString(hook.ID))
		}
		return err
	}
	return nil
}

// pauseHookForInvalidPrincipal disables a hook whose stored authorization principal
// has left the workspace, so it stops producing work under authority nobody holds any
// more (§7.3), and notifies the workspace owners/admins who can act on it. It runs in
// the CALLER's transaction: the pause must be durable in the same commit as the
// terminal skip, or the rule would keep firing while the execution reads as handled.
func (s *HookService) pauseHookForInvalidPrincipal(ctx context.Context, qtx *db.Queries, exec db.HookExecution) error {
	hook, err := qtx.SetHookEnabled(ctx, db.SetHookEnabledParams{
		ID: exec.HookID, WorkspaceID: exec.WorkspaceID, Enabled: false,
		DisabledReason: pgtype.Text{String: hookDisabledPrincipalInvalid, Valid: true},
	})
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil // already archived; nothing left to pause
		}
		return err
	}
	return s.notifyAdminsHookPaused(ctx, qtx, hook)
}

// notifyAdminsHookPaused tells the people who can re-arm the rule that it stopped.
// A paused hook is silent by nature, so without this the rule simply stops working
// with nothing surfaced to anyone (§7.3).
func (s *HookService) notifyAdminsHookPaused(ctx context.Context, qtx *db.Queries, hook db.Hook) error {
	admins, err := qtx.ListWorkspaceMembersByRoles(ctx, db.ListWorkspaceMembersByRolesParams{
		WorkspaceID: hook.WorkspaceID, Roles: []string{"owner", "admin"},
	})
	if err != nil {
		return err
	}
	details, err := json.Marshal(map[string]any{
		"hook_id": util.UUIDToString(hook.ID),
		"reason":  hookDisabledPrincipalInvalid,
	})
	if err != nil {
		return err
	}
	for _, admin := range admins {
		if _, err := qtx.CreateInboxItem(ctx, db.CreateInboxItemParams{
			WorkspaceID:   hook.WorkspaceID,
			RecipientType: "member",
			RecipientID:   admin.UserID,
			Type:          "hook_paused",
			Severity:      "action_required",
			Title:         "Automation paused: " + hook.Name,
			Body:          pgtype.Text{String: "Its authorization principal is no longer a workspace member, so the rule was disabled.", Valid: true},
			ActorType:     pgtype.Text{String: "system", Valid: true},
			Details:       details,
		}); err != nil {
			return err
		}
	}
	return nil
}

// performAction dispatches one action. It returns the domain events the action
// produced, which the caller commits in the same transaction, plus the effect output.
func (s *HookService) performAction(ctx context.Context, qtx *db.Queries, exec db.HookExecution, action automation.ActionSpec, index int) ([]domainevent.Event, string, pgtype.UUID, error) {
	switch action.Type {
	case automation.ActionSetIssueStatus:
		return s.actionSetIssueStatus(ctx, qtx, exec, action, index)
	default:
		// Reached only for an action this slice does not implement yet. Terminal
		// rather than retried, so it cannot loop on the backoff ladder.
		return nil, "", pgtype.UUID{}, skipAction(skipActionUnsupported,
			"action %q is not implemented by this executor slice", action.Type)
	}
}

// actionSetIssueStatus writes the target status and emits the resulting
// issue.status_changed event in the caller's transaction, so the fact and its event
// commit together exactly as every other domain write does.
func (s *HookService) actionSetIssueStatus(ctx context.Context, qtx *db.Queries, exec db.HookExecution, action automation.ActionSpec, index int) ([]domainevent.Event, string, pgtype.UUID, error) {
	issueID, err := util.ParseUUID(action.IssueID)
	if err != nil {
		return nil, "", pgtype.UUID{}, skipAction(skipTargetUnavailable, "issue_id %q is not a uuid", action.IssueID)
	}
	// Workspace-scoped, so an action can never reach across tenants (§8).
	before, err := qtx.LockIssueRowForUpdate(ctx, db.LockIssueRowForUpdateParams{ID: issueID, WorkspaceID: exec.WorkspaceID})
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, "", pgtype.UUID{}, skipAction(skipTargetUnavailable,
				"issue %s is not in this workspace", action.IssueID)
		}
		return nil, "", pgtype.UUID{}, err
	}
	updated, err := qtx.UpdateIssueStatus(ctx, db.UpdateIssueStatusParams{
		ID: issueID, WorkspaceID: exec.WorkspaceID, Status: action.Status,
	})
	if err != nil {
		return nil, "", pgtype.UUID{}, err
	}
	// A no-op transition is a successful action that emits nothing, matching every
	// other status write in the codebase.
	if before.Status == updated.Status {
		return nil, "issue", issueID, nil
	}

	evt := domainevent.IssueStatusChanged(exec.WorkspaceID, issueID,
		domainevent.ActorFrom("hook", exec.HookID),
		domainevent.IssueStatusChangedPayload{From: before.Status, To: updated.Status})
	// The reaction stays in its originating chain and records what caused it, so the
	// depth guard can see how deep this chain has run.
	evt.CorrelationID = exec.CorrelationID
	evt.CausationExecutionID = exec.ID
	evt.CausationActionIndex = pgtype.Int4{Int32: int32(index), Valid: true}
	hop, err := s.sourceHopCount(ctx, qtx, exec.EventID)
	if err != nil {
		return nil, "", pgtype.UUID{}, err
	}
	evt.HopCount = hop + 1

	return []domainevent.Event{evt}, "issue", issueID, nil
}

// sourceHopCount reads the depth of the event that produced this execution, so the
// event this action emits sits one hop deeper in the same causal chain.
func (s *HookService) sourceHopCount(ctx context.Context, qtx *db.Queries, eventID pgtype.UUID) (int32, error) {
	src, err := qtx.GetDomainEvent(ctx, eventID)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return 0, nil // source aged out of retention; treat as a root
		}
		return 0, err
	}
	return src.HopCount, nil
}

// finalizeSkippedAction commits a terminal, non-retryable outcome: the action's
// durable effect row and the execution's skipped status, atomically. It reports
// whether this worker still owned the execution and therefore actually finalized it.
//
// A departed principal additionally pauses the hook, and that pause must be DURABLE
// before the skip is committed. Recording the skip while the hook stays enabled would
// be fail-open: the rule would keep producing executions under authority nobody
// holds. So the pause joins the same transaction, and if it cannot be written the
// whole thing is treated as a retryable infrastructure failure instead.
func (s *HookService) finalizeSkippedAction(ctx context.Context, exec db.HookExecution, lease pgtype.UUID, action automation.ActionSpec, index int, skip *actionSkip) bool {
	slog.Warn("hook executor: action skipped", "execution_id", util.UUIDToString(exec.ID),
		"action_index", index, "reason", skip.reason, "detail", skip.detail)

	err := s.inTx(ctx, func(qtx *db.Queries) error {
		if skip.reason == skipPrincipalInvalid {
			if err := s.pauseHookForInvalidPrincipal(ctx, qtx, exec); err != nil {
				return err
			}
		}
		if err := s.writeTerminalEffect(ctx, qtx, exec, action, index, hookExecSkipped, skip.reason, skip.detail); err != nil {
			return err
		}
		rows, err := qtx.MarkHookExecutionSkipped(ctx, db.MarkHookExecutionSkippedParams{
			ID: exec.ID, LeaseToken: lease, SkipReason: pgtype.Text{String: skip.reason, Valid: true},
		})
		if err != nil {
			return err
		}
		if rows != 1 {
			return errExecutionLeaseLost
		}
		return nil
	})
	switch {
	case err == nil:
		return true
	case errors.Is(err, errExecutionLeaseLost):
		return false
	default:
		// The terminal record could not be written. Do NOT leave the execution
		// half-finalized: re-queue it so the outcome is recorded on a later attempt.
		slog.Warn("hook executor: could not commit terminal skip, retrying",
			"execution_id", util.UUIDToString(exec.ID), "reason", skip.reason, "error", err)
		s.rescheduleOrFail(ctx, exec, lease, action, index, err)
		return false
	}
}

// writeTerminalEffect records the durable audit row for an action that ended
// terminally. The success path writes its effect inside the action transaction,
// which rolls back on failure, so without this a skipped or failed action would
// leave no trace and a partial execution would show only the actions that succeeded.
func (s *HookService) writeTerminalEffect(ctx context.Context, qtx *db.Queries, exec db.HookExecution, action automation.ActionSpec, index int, status, code, detail string) error {
	resolved, err := json.Marshal(action)
	if err != nil {
		return err
	}
	return qtx.UpsertTerminalHookActionEffect(ctx, db.UpsertTerminalHookActionEffectParams{
		ID:            util.NewUUID(),
		EffectKey:     effectKeyFor(exec.ID, index),
		ExecutionID:   exec.ID,
		ActionIndex:   int32(index),
		ActionType:    action.Type,
		Status:        status,
		ResolvedInput: resolved,
		ErrorCode:     pgtype.Text{String: code, Valid: code != ""},
		Error:         pgtype.Text{String: detail, Valid: detail != ""},
	})
}

// deferExpiredExecution backs off an execution whose lease elapsed while this worker
// was running it, so it cannot hold the head of the queue. The CAS fires only while
// the row still carries our token, so a row another worker has already reclaimed is
// left untouched.
func (s *HookService) deferExpiredExecution(ctx context.Context, exec db.HookExecution, lease pgtype.UUID) error {
	backoff := executorBackoff[0]
	rows, err := s.Queries.DeferExpiredHookExecution(ctx, db.DeferExpiredHookExecutionParams{
		ID: exec.ID, LeaseToken: lease, BackoffSeconds: int32(backoff.Seconds()),
	})
	if err != nil {
		return err
	}
	if rows == 1 {
		slog.Warn("hook executor: lease expired mid-execution, backed off",
			"execution_id", util.UUIDToString(exec.ID), "backoff", backoff)
	}
	return nil
}

// rescheduleOrFail applies the infrastructure retry ladder, or marks the execution
// failed once it is exhausted. It reports whether the execution reached a terminal
// state.
func (s *HookService) rescheduleOrFail(ctx context.Context, exec db.HookExecution, lease pgtype.UUID, action automation.ActionSpec, index int, cause error) bool {
	// attempts was incremented by the claim, so attempt N reads as attempts == N.
	if attempt := int(exec.Attempts); attempt <= len(executorBackoff) {
		backoff := executorBackoff[attempt-1]
		rows, err := s.Queries.RescheduleHookExecution(ctx, db.RescheduleHookExecutionParams{
			ID: exec.ID, LeaseToken: lease, BackoffSeconds: int32(backoff.Seconds()),
		})
		if err != nil {
			slog.Warn("hook executor: could not reschedule execution",
				"execution_id", util.UUIDToString(exec.ID), "error", err)
			return false
		}
		if rows == 1 {
			slog.Warn("hook executor: action failed, retrying after backoff",
				"execution_id", util.UUIDToString(exec.ID), "attempt", attempt,
				"backoff", backoff, "error", cause)
		}
		return false // re-queued, not terminal
	}

	// The ladder is spent. Record the action's terminal effect and the execution's
	// failure together, so an exhausted action leaves the same audit trail a
	// successful one does.
	var finalized bool
	if err := s.inTx(ctx, func(qtx *db.Queries) error {
		if err := s.writeTerminalEffect(ctx, qtx, exec, action, index, hookExecFailed, errCodeInfra, cause.Error()); err != nil {
			return err
		}
		rows, err := qtx.MarkHookExecutionFailed(ctx, db.MarkHookExecutionFailedParams{
			ID: exec.ID, LeaseToken: lease,
			ErrorCode: pgtype.Text{String: errCodeInfra, Valid: true},
			Error:     pgtype.Text{String: cause.Error(), Valid: true},
		})
		if err != nil {
			return err
		}
		finalized = rows == 1
		return nil
	}); err != nil {
		slog.Warn("hook executor: could not mark execution failed",
			"execution_id", util.UUIDToString(exec.ID), "error", err)
		return false
	}
	return finalized
}
