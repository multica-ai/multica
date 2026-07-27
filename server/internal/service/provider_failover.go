package service

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"strings"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgtype"

	"github.com/multica-ai/multica/server/internal/featureflags"
	"github.com/multica-ai/multica/server/internal/util"
	db "github.com/multica-ai/multica/server/pkg/db/generated"
	"github.com/multica-ai/multica/server/pkg/protocol"
	"github.com/multica-ai/multica/server/pkg/providerfailover"
	"github.com/multica-ai/multica/server/pkg/taskfailure"
)

// failoverMode resolves the current operational posture (off/shadow/active) of
// the provider-failover subsystem from the feature flags. A nil FeatureFlags
// resolves to Off.
func (s *TaskService) failoverMode(ctx context.Context) providerfailover.Mode {
	return featureflags.ProviderFailoverMode(ctx, s.FeatureFlags)
}

// EvaluateFailover runs the GPT->Claude usage/rate-limit failover policy for a
// task that FailTask just marked failed, and records the auditable outcome.
//
// It is called best-effort AFTER the fail transaction commits: a failover
// decision must never roll back or otherwise perturb the fail itself. Every
// error here is logged and swallowed. In shadow mode (the default when the
// feature is on at all) it only records what it would do; in active mode it
// claims chain ownership, dispatches a Claude fallback, or records an explicit
// failure when Claude is unavailable.
//
// The fast path — the overwhelmingly common case — returns before any DB work:
// the failure is not a usage/rate-limit trigger, or the feature is off.
func (s *TaskService) EvaluateFailover(ctx context.Context, task db.AgentTaskQueue, failureReason string, evidence *providerfailover.SideEffectEvidence) {
	reason := taskfailure.Reason(failureReason)
	if !providerfailover.IsFailoverTrigger(reason) {
		return
	}
	mode := s.failoverMode(ctx)
	if mode == providerfailover.ModeOff {
		return
	}
	// Chain-less tasks have nowhere to surface a handoff. (Orchestrator-tier
	// runs — autopilot / squad-leader — are no longer skipped here: coverage is
	// extended to them and a real active handoff is gated on control-plane
	// idempotency in the policy, td-836aa9.)
	if !task.IssueID.Valid && !task.ChatSessionID.Valid {
		return
	}

	agent, err := s.Queries.GetAgent(ctx, task.AgentID)
	if err != nil {
		slog.Warn("failover eval: load agent failed",
			"task_id", util.UUIDToString(task.ID), "error", err)
		return
	}

	chainRoot := chainRootForTask(task)
	sourceProvider := s.providerForRuntime(ctx, task.RuntimeID)
	// Bidirectional (td-836aa9): the target provider is derived from the source
	// (codex->claude or claude->codex), not hardcoded to Claude.
	targetProvider := providerfailover.PrimaryTargetFor(sourceProvider)
	in := providerfailover.Input{
		FailureReason:          reason,
		SourceProvider:         sourceProvider,
		Mode:                   mode,
		AuthoritySensitive:     agentAuthoritySensitive(agent, task),
		IsAlreadyFallback:      s.taskIsFallback(ctx, task.ID),
		ChainHasOwningHandoff:  s.chainHasOwningHandoff(ctx, chainRoot, task.ID),
		SideEffects:            s.gatherFailoverSideEffects(ctx, task, evidence),
		SideEffectsComplete:    failoverSideEffectsComplete(evidence),
		OrchestratorTier:       isOrchestratorTier(task),
		ControlPlaneIdempotent: s.controlPlaneIdempotentForChain(ctx, chainRoot),
	}

	// Resolve target availability only when active mode would otherwise proceed
	// (eligible AND side effects proven AND, for orchestrator runs, idempotent) —
	// the probe is a DB round-trip we skip on the common decline path, on the
	// safety holds, and entirely in shadow (which records "would fail over"
	// independent of availability).
	var target *db.Agent
	if mode == providerfailover.ModeActive {
		probe := in
		probe.ClaudeAvailable = true // isolate the availability factor
		if providerfailover.Decide(probe).Outcome == providerfailover.OutcomeProceed {
			target, in.ClaudeAvailable = s.resolveFailoverTarget(ctx, agent.WorkspaceID, task.AgentID, targetProvider)
		}
	}

	decision := providerfailover.Decide(in)
	s.applyFailoverDecision(ctx, task, agent, chainRoot, in, target, decision)
}

// applyFailoverDecision persists the ledger row for a decision and, in active
// mode, performs the corresponding action (dispatch a Claude fallback or record
// an explicit failure with a user-visible reference).
func (s *TaskService) applyFailoverDecision(
	ctx context.Context,
	task db.AgentTaskQueue,
	agent db.Agent,
	chainRoot pgtype.UUID,
	in providerfailover.Input,
	target *db.Agent,
	decision providerfailover.Decision,
) {
	sideEffectsJSON, err := json.Marshal(in.SideEffects)
	if err != nil {
		// A snapshot that will not marshal is not a reason to skip the audit
		// trail; record an empty object rather than dropping the row.
		sideEffectsJSON = []byte("{}")
	}

	// Bidirectional (td-836aa9): record the direction the policy chose. Fall
	// back to the legacy Claude default only when the source is not a failover
	// participant (target ""), so the NOT NULL column stays meaningful.
	targetProvider := decision.TargetProvider
	if targetProvider == "" {
		targetProvider = providerfailover.TargetProvider
	}
	params := db.RecordFailoverHandoffParams{
		WorkspaceID:     agent.WorkspaceID,
		OriginalTaskID:  task.ID,
		ChainRootTaskID: chainRoot,
		IssueID:         task.IssueID,
		ChatSessionID:   task.ChatSessionID,
		SourceAgentID:   task.AgentID,
		SourceProvider:  in.SourceProvider,
		TargetProvider:  targetProvider,
		TriggerReason:   string(in.FailureReason),
		State:           string(decision.State),
		Mode:            string(in.Mode),
		WouldFailOver:   decision.WouldFailOver,
		DeclineReason:   pgtype.Text{String: decision.Reason, Valid: decision.Reason != ""},
		SideEffects:     sideEffectsJSON,
	}

	// The proceed path is atomic: the owning ledger row, the Claude fallback
	// task, and the PENDING->DISPATCHED linkage all commit together (or none
	// do), so a crash can never leave a queued child with no persisted linkage
	// or an owning row with no child.
	if decision.Outcome == providerfailover.OutcomeProceed {
		s.dispatchFailoverFallback(ctx, task, params, target)
		return
	}

	// Non-proceed outcomes are a single, self-contained ledger insert.
	rec, err := s.Queries.RecordFailoverHandoff(ctx, params)
	if err != nil {
		s.logFailoverRecordError(task, chainRoot, err)
		return
	}
	s.logFailoverEvaluated(task, rec, in, decision)

	if decision.Outcome == providerfailover.OutcomeUnavailable {
		s.surfaceFailoverUnavailable(ctx, task, rec)
	}
	// OutcomeShadow / OutcomeDeclined: the ledger row is the whole action.
}

// dispatchFailoverFallback atomically records the owning handoff, creates the
// Claude fallback task, and advances the ledger PENDING -> DISPATCHED, all in
// one transaction. Post-commit it surfaces the new attempt (broadcast/notify/
// reconcile) and a user-visible reference comment.
func (s *TaskService) dispatchFailoverFallback(ctx context.Context, task db.AgentTaskQueue, params db.RecordFailoverHandoffParams, target *db.Agent) {
	if target == nil {
		// OutcomeProceed implies a resolved target; guard defensively rather
		// than dispatch to nil.
		slog.Warn("failover dispatch: no target for proceed outcome",
			"task_id", util.UUIDToString(task.ID))
		return
	}
	params.TargetAgentID = target.ID

	var rec db.ProviderFailoverHandoff
	var child db.AgentTaskQueue
	err := s.runInTx(ctx, func(qtx *db.Queries) error {
		r, err := qtx.RecordFailoverHandoff(ctx, params)
		if err != nil {
			return err // ErrNoRows (already recorded) or chain-owner 23505
		}
		c, err := qtx.CreateFailoverTask(ctx, db.CreateFailoverTaskParams{
			TargetAgentID:   target.ID,
			TargetRuntimeID: target.RuntimeID,
			OriginalTaskID:  task.ID,
		})
		if err != nil {
			return fmt.Errorf("create fallback task: %w", err)
		}
		r2, err := qtx.SetFailoverHandoffState(ctx, db.SetFailoverHandoffStateParams{
			ID:             r.ID,
			ExpectedState:  string(providerfailover.StatePending),
			NewState:       string(providerfailover.StateDispatched),
			TargetAgentID:  target.ID,
			FallbackTaskID: pgtype.UUID{Bytes: c.ID.Bytes, Valid: true},
		})
		if err != nil {
			return fmt.Errorf("advance handoff to dispatched: %w", err)
		}
		rec, child = r2, c
		return nil
	})
	if err != nil {
		s.logFailoverRecordError(task, params.ChainRootTaskID, err)
		return
	}

	slog.Info("provider failover dispatched",
		"handoff_id", util.UUIDToString(rec.ID),
		"original_task_id", util.UUIDToString(task.ID),
		"fallback_task_id", util.UUIDToString(child.ID),
		"target_agent_id", util.UUIDToString(target.ID),
	)

	// Surface the new attempt exactly like the auto-retry path: broadcast queued
	// first, then wake the daemon.
	if child.Status == "queued" {
		s.broadcastTaskEvent(ctx, protocol.EventTaskQueued, child)
		s.NotifyTaskEnqueued(ctx, child)
	}
	s.ReconcileAgentStatus(ctx, target.ID)

	// User-visible reference: an info comment on the issue so the handoff is
	// observable in the product, not just the ledger/logs.
	if task.IssueID.Valid {
		body := fmt.Sprintf("Provider failover: the %s run could not continue, handing this off to %s (%s). Ledger ref %s.",
			rec.SourceProvider, target.Name, rec.TargetProvider, util.UUIDToString(rec.ID))
		s.createAgentComment(ctx, task.IssueID, task.AgentID, body, "system", task.TriggerCommentID, task.ID)
	}
}

// logFailoverRecordError classifies a failed RecordFailoverHandoff (raw or
// inside the dispatch tx) into the three expected outcomes: idempotent re-entry,
// a lost ownership race, or a genuine error.
func (s *TaskService) logFailoverRecordError(task db.AgentTaskQueue, chainRoot pgtype.UUID, err error) {
	switch {
	case errors.Is(err, pgx.ErrNoRows):
		// ON CONFLICT (original_task_id) DO NOTHING: already evaluated. Idempotent.
	case isFailoverChainOwnerConflict(err):
		// The chain-owner partial unique index rejected a concurrent claim:
		// another handoff won ownership of this chain first. This is exactly the
		// at-most-one-per-chain guarantee firing under a race. The whole dispatch
		// tx rolled back, so no orphan fallback task was created.
		slog.Info("failover eval: chain already owned by a concurrent handoff",
			"task_id", util.UUIDToString(task.ID),
			"chain_root", util.UUIDToString(chainRoot))
	default:
		slog.Warn("failover eval: record handoff failed",
			"task_id", util.UUIDToString(task.ID), "error", err)
	}
}

// logFailoverEvaluated emits the audit log line for a recorded decision.
func (s *TaskService) logFailoverEvaluated(task db.AgentTaskQueue, rec db.ProviderFailoverHandoff, in providerfailover.Input, decision providerfailover.Decision) {
	slog.Info("provider failover evaluated",
		"handoff_id", util.UUIDToString(rec.ID),
		"task_id", util.UUIDToString(task.ID),
		"mode", in.Mode,
		"outcome", decision.Outcome,
		"state", decision.State,
		"would_fail_over", decision.WouldFailOver,
		"reason", decision.Reason,
		"source_provider", in.SourceProvider,
	)
}

// surfaceFailoverUnavailable records the user-visible outcome when a handoff was
// warranted but no Claude target was available. The original task stays failed;
// the ledger row is already HANDOFF_FAILED. We add an explicit comment carrying
// the ledger reference so the failure is auditable and actionable.
func (s *TaskService) surfaceFailoverUnavailable(ctx context.Context, task db.AgentTaskQueue, rec db.ProviderFailoverHandoff) {
	slog.Warn("provider failover unavailable: no Claude target",
		"handoff_id", util.UUIDToString(rec.ID),
		"task_id", util.UUIDToString(task.ID))
	if task.IssueID.Valid {
		body := fmt.Sprintf("Provider failover: the %s run could not continue, but no %s runtime was available to take over — this run has FAILED. Ledger ref %s.",
			rec.SourceProvider, rec.TargetProvider, util.UUIDToString(rec.ID))
		s.createAgentComment(ctx, task.IssueID, task.AgentID, body, "system", task.TriggerCommentID, task.ID)
	}
}

// OriginalTaskSuperseded reports whether a failed GPT primary task's chain has
// been taken over by a failover handoff. CompleteTask consults it so a late
// primary completion callback is discarded rather than posting a duplicate
// outcome or resurrecting the chain the Claude fallback now owns.
//
// It is fail-CLOSED on uncertainty. Three cases:
//   - no handoff row (pgx.ErrNoRows)      -> (false, nil): safe to complete.
//   - a handoff row in an owning state     -> (true, nil):  discard the completion.
//   - any other lookup error               -> (false, err): ownership is UNKNOWN,
//     so the caller must NOT complete now; it propagates the error and the
//     completion callback is retried later rather than risking a duplicate
//     outcome against a chain a Claude fallback may already own.
//
// Only meaningful in active mode; callers gate on that to keep the query off the
// hot path when the feature is disabled.
func (s *TaskService) OriginalTaskSuperseded(ctx context.Context, taskID pgtype.UUID) (bool, error) {
	rec, err := s.Queries.GetFailoverHandoffByOriginalTask(ctx, taskID)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return false, nil // no handoff owns this chain
		}
		return false, fmt.Errorf("failover supersede lookup: %w", err)
	}
	return providerfailover.HandoffState(rec.State).IsOwning(), nil
}

// FinalizeFailoverForFallbackOutcome transitions the owning handoff whose Claude
// fallback task just reached a terminal outcome (HANDOFF_DISPATCHED ->
// HANDOFF_COMPLETED on complete, -> HANDOFF_FAILED on fail) so the ledger and
// its state semantics stay accurate. Best-effort and post-commit: it is called
// from CompleteTask/FailTask for the FALLBACK task (keyed by fallback_task_id),
// which is a different concern from EvaluateFailover (the primary-side trigger).
// A no-op for any task that is not a dispatched fallback.
func (s *TaskService) FinalizeFailoverForFallbackOutcome(ctx context.Context, taskID pgtype.UUID, terminal providerfailover.HandoffState) {
	rec, err := s.Queries.FinalizeFailoverHandoffByFallbackTask(ctx, db.FinalizeFailoverHandoffByFallbackTaskParams{
		FallbackTaskID: taskID,
		NewState:       string(terminal),
	})
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return // not a dispatched fallback (the common case), or already terminal
		}
		slog.Warn("failover: finalize fallback handoff failed",
			"fallback_task_id", util.UUIDToString(taskID),
			"terminal_state", terminal, "error", err)
		return
	}
	slog.Info("provider failover fallback finalized",
		"handoff_id", util.UUIDToString(rec.ID),
		"fallback_task_id", util.UUIDToString(taskID),
		"state", terminal)
}

// --- helpers -------------------------------------------------------------

// chainRootForTask returns the stable identifier of the task's chain, used for
// at-most-one-per-chain enforcement. Chat chains share chat_input_task_id; an
// issue task with no chat input is its own root.
func chainRootForTask(task db.AgentTaskQueue) pgtype.UUID {
	if task.ChatInputTaskID.Valid {
		return task.ChatInputTaskID
	}
	return task.ID
}

// providerForRuntime resolves the provider string of a runtime, or "" when it
// cannot be determined. "" is safe: it is never equal to the Claude target, so
// it never spuriously trips the source==target decline.
func (s *TaskService) providerForRuntime(ctx context.Context, runtimeID pgtype.UUID) string {
	if !runtimeID.Valid {
		return ""
	}
	rt, err := s.Queries.GetAgentRuntime(ctx, runtimeID)
	if err != nil {
		return ""
	}
	return rt.Provider
}

// taskIsFallback reports whether the task is itself the Claude fallback of some
// prior handoff — the loop guard's positive signal.
func (s *TaskService) taskIsFallback(ctx context.Context, taskID pgtype.UUID) bool {
	_, err := s.Queries.GetFailoverHandoffByFallbackTask(ctx, taskID)
	return err == nil
}

// chainHasOwningHandoff reports whether another handoff already owns this chain
// (excluding the row for the task being evaluated). The DB's partial unique
// index is the atomic backstop; this pre-check lets the policy decline cleanly
// with the right reason before attempting an insert.
func (s *TaskService) chainHasOwningHandoff(ctx context.Context, chainRoot, excludeOriginal pgtype.UUID) bool {
	owned, err := s.Queries.ChainHasOwningHandoff(ctx, db.ChainHasOwningHandoffParams{
		ChainRootTaskID:       chainRoot,
		ExcludeOriginalTaskID: excludeOriginal,
	})
	if err != nil {
		// Fail closed: if we cannot prove the chain is free, decline the handoff.
		return true
	}
	return owned
}

// gatherFailoverSideEffects builds the pre-fallback side-effect snapshot from
// what the server persists about the failed run plus the daemon-observed
// evidence (td-836aa9). Any positive signal blocks the handoff (see
// providerfailover.SideEffects).
//
// Two independent sources feed the snapshot:
//   - server-persisted signals (delivered-comment receipts, an agent comment on
//     the issue) — always available and reliable.
//   - daemon-observed signals (in-run tool-call count, partial streamed output)
//     carried on the fail callback as evidence. Nil for older daemons and for
//     failures observed before the run streamed, in which case those fields stay
//     zero/false and active mode holds fail-closed via failoverSideEffectsComplete.
//
// Head-SHA movement needs no separate field: any head change requires a tool
// call (a write/shell/git tool), which the daemon counts, so ObservedToolCalls
// already covers it (see providerfailover.SideEffectEvidence).
func (s *TaskService) gatherFailoverSideEffects(ctx context.Context, task db.AgentTaskQueue, evidence *providerfailover.SideEffectEvidence) providerfailover.SideEffects {
	se := providerfailover.SideEffects{
		DeliveredCommentIDs: len(task.DeliveredCommentIds),
	}
	if evidence != nil {
		se.ObservedToolCalls = evidence.ObservedToolCalls
		se.PartialOutput = evidence.PartialUserOutput
	}
	if task.IssueID.Valid {
		commented, err := s.Queries.HasAgentCommentedSince(ctx, db.HasAgentCommentedSinceParams{
			IssueID:  task.IssueID,
			AuthorID: task.AgentID,
			Since:    task.StartedAt,
		})
		if err != nil {
			// Cannot confirm the run posted nothing -> assume it did (fail closed).
			se.AgentCommented = true
		} else {
			se.AgentCommented = commented
		}
	}
	return se
}

// resolveFailoverTarget finds an eligible target agent of the given provider in
// the workspace and reports whether one is available. Availability == at least
// one structurally-eligible, non-authority-sensitive agent of targetProvider on
// an online runtime. Bidirectional (td-836aa9): targetProvider is 'claude' for a
// GPT->Claude handoff and 'codex' for a Claude->GPT handoff. Returns the chosen
// target (oldest first, deterministic) or nil.
func (s *TaskService) resolveFailoverTarget(ctx context.Context, workspaceID, sourceAgentID pgtype.UUID, targetProvider string) (*db.Agent, bool) {
	if targetProvider == "" {
		return nil, false
	}
	candidates, err := s.Queries.ListFailoverTargets(ctx, db.ListFailoverTargetsParams{
		WorkspaceID:    workspaceID,
		ExcludeAgentID: sourceAgentID,
		TargetProvider: targetProvider,
	})
	if err != nil {
		slog.Warn("failover: list targets failed",
			"workspace_id", util.UUIDToString(workspaceID),
			"target_provider", targetProvider, "error", err)
		return nil, false
	}
	for i := range candidates {
		c := candidates[i]
		if agentAuthoritySensitive(c, db.AgentTaskQueue{}) {
			continue
		}
		return &c, true
	}
	return nil, false
}

// isOrchestratorTier reports whether a failed run is orchestrator-tier — it
// coordinates other work rather than only executing it. Two structural signals:
// an autopilot run (the scheduled orchestration driver) and a squad leader task
// (drives its members). Everything else is an actor-tier leaf. Orchestrator-tier
// runs are now COVERED by failover (they used to be skipped outright), but a real
// active handoff of one is gated on control-plane idempotency in the policy
// (ReasonOrchestratorIdempotencyUnproven) so a re-planning fallback cannot
// double-spawn children or double-promote stages (td-836aa9).
func isOrchestratorTier(task db.AgentTaskQueue) bool {
	return task.AutopilotRunID.Valid || task.IsLeaderTask
}

// controlPlaneIdempotentForChain reports whether this chain's control-plane
// effects (child task-spawns, stage promotions) are guarded by the idempotency
// ledger, so a handed-off fallback cannot double-dispatch them. It gates active
// orchestrator handoffs (providerfailover.Input.ControlPlaneIdempotent).
//
// Returns false today (unconditionally): the orchestration effect sites — the
// agent-driven `multica issue create --parent` (task-spawn) and stage-promotion
// paths — do not yet wrap their effects in ClaimControlPlaneEffectOnce, so the
// server cannot prove idempotency for an arbitrary chain. Active orchestrator
// handoffs are therefore held closed, exactly mirroring failoverSideEffectsComplete's
// fail-closed posture, while shadow still records orchestrator coverage. Flip
// this to a real per-chain ledger check once those call sites adopt the claim.
func (s *TaskService) controlPlaneIdempotentForChain(_ context.Context, _ pgtype.UUID) bool {
	return false
}

// ClaimControlPlaneEffectOnce durably claims an orchestrator control-plane effect
// so it happens at most once across the original run AND any failover fallback
// (td-836aa9). It returns claimed=true when THIS caller won the claim (proceed
// with the effect) and claimed=false when the effect was already recorded (skip —
// a prior run, possibly the pre-handoff primary, already did it). An unkeyable
// effect (empty chain/target) is fail-closed to claimed=false: the caller must
// not perform an effect it cannot dedup under a possible handoff.
//
// This is the primitive the task-spawn and stage-promotion call sites wrap their
// effect in to make ControlPlaneIdempotent provable for a chain.
func (s *TaskService) ClaimControlPlaneEffectOnce(ctx context.Context, workspaceID, chainRoot pgtype.UUID, effect providerfailover.ControlPlaneEffect, targetRef string) (bool, error) {
	key := providerfailover.EffectKey(util.UUIDToString(chainRoot), effect, targetRef)
	if key == "" {
		return false, nil
	}
	_, err := s.Queries.ClaimControlPlaneEffect(ctx, db.ClaimControlPlaneEffectParams{
		WorkspaceID:     workspaceID,
		ChainRootTaskID: chainRoot,
		EffectType:      string(effect),
		EffectKey:       key,
		TargetRef:       targetRef,
	})
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return false, nil // already claimed by a prior run — skip (idempotent)
		}
		return false, fmt.Errorf("claim control-plane effect: %w", err)
	}
	return true, nil
}

// EvaluateLivenessFailover runs the failover policy for tasks the stale-task
// sweeper just reaped as SILENT HANGS — running tasks whose owning runtime
// stopped proving liveness past the wall-clock deadline (td-836aa9). Before this,
// a hung run was failed with reason 'timeout' and never entered failover (plain
// timeout is structurally excluded); the liveness watchdog reclassifies the
// running-branch subset as ReasonProviderLivenessTimeout — a trigger — so a hang
// can hand off instead of silently stalling the task.
//
// Best-effort and post-commit, mirroring EvaluateFailover's contract; the fast
// path returns immediately when the feature is off. Only RUNNING-branch reaped
// rows (StartedAt set) are silent hangs; a dispatched-but-never-started timeout
// (StartedAt null) never ran, so it is skipped. Evidence is nil — the daemon is
// gone, so nothing was observed — which keeps active handoffs fail-closed on
// completeness while shadow records the would-fail-over verdict.
func (s *TaskService) EvaluateLivenessFailover(ctx context.Context, reaped []db.AgentTaskQueue) {
	if s.failoverMode(ctx) == providerfailover.ModeOff {
		return
	}
	for _, t := range reaped {
		if !t.StartedAt.Valid {
			continue // dispatched-timeout branch: never actually started running
		}
		s.EvaluateFailover(ctx, t, string(taskfailure.ReasonProviderLivenessTimeout), nil)
	}
}

// agentAuthoritySensitive reports whether an agent is authority-sensitive and
// therefore structurally excluded from silent failover.
//
// System-kind agents are always authority-sensitive. User-kind agents can opt
// into the same structural exclusion with
// runtime_config.provider_failover_protected=true. This gives protected
// reviewers a stable marker without guessing from mutable names or system_key
// substrings. The exact legacy identity "Protected Reviewer" is also excluded
// for compatibility with existing workspaces created before the marker existed.
// Malformed runtime config does not grant authority; the agent remains governed
// by the normal side-effect and eligibility gates.
//
// The task argument is accepted for call-site symmetry and future task-scoped
// signals; a zero-value task is valid (used when only the agent is known, e.g.
// target vetting).
func agentAuthoritySensitive(agent db.Agent, _ db.AgentTaskQueue) bool {
	if agent.Kind == "system" {
		return true
	}
	if strings.EqualFold(strings.TrimSpace(agent.Name), "Protected Reviewer") {
		return true
	}
	if len(agent.RuntimeConfig) == 0 {
		return false
	}
	var config struct {
		ProviderFailoverProtected bool `json:"provider_failover_protected"`
	}
	return json.Unmarshal(agent.RuntimeConfig, &config) == nil &&
		config.ProviderFailoverProtected
}

// failoverSideEffectsComplete reports whether the server can PROVE the failed
// run's entire side-effect surface is captured in the snapshot from
// gatherFailoverSideEffects. It gates active-mode handoffs (see
// providerfailover.Input.SideEffectsComplete).
//
// Completeness is true only when the daemon sent a completeness-marked evidence
// object (td-836aa9). Two cases keep it false and hold active handoffs
// fail-closed with ReasonSideEffectsUnproven:
//   - evidence == nil: an older daemon that does not send the object at all, or
//     a fail path (pre-execution error) that never observed the run.
//   - evidence.Complete == false: the daemon sent evidence but could not assert
//     it observed the run to a terminal end.
//
// When it IS complete, the proof is sound because every agent mutation — file
// writes, shell/git commands, comment posts, code pushes, and therefore any
// reviewed-head-SHA movement — happens through an observed tool call. So a
// complete object whose ObservedToolCalls is 0 with no partial user output
// proves the run changed nothing (the HasObservableSideEffects gate in the
// policy still independently declines any non-zero tool count / partial output).
// Shadow mode ignores this gate and always records the observable-subset verdict.
func failoverSideEffectsComplete(evidence *providerfailover.SideEffectEvidence) bool {
	return evidence != nil && evidence.Complete
}

// isFailoverChainOwnerConflict reports whether err is the chain-owner partial
// unique index rejecting a concurrent ownership claim (Postgres 23505 on that
// specific index).
func isFailoverChainOwnerConflict(err error) bool {
	var pgErr *pgconn.PgError
	if !errors.As(err, &pgErr) {
		return false
	}
	return pgErr.Code == "23505" &&
		strings.Contains(pgErr.ConstraintName, "provider_failover_chain_owner")
}
