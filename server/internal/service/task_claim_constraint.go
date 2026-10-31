package service

import (
	"context"
	"log/slog"

	"github.com/jackc/pgx/v5/pgtype"

	"github.com/multica-ai/multica/server/internal/util"
)

// Claim constraint policy SPI (#6650 admission/preflight; direct follow-up to
// the #9184 claim decision funnel). This file defines an optional evaluation
// seam at the candidate stage of the runtime and batch claim polls: before
// claimTask is invoked for an agent's head candidate, a policy gets a snapshot
// of what is about to be attempted and returns a decision.
//
// The SPI is shadow-only by construction. Nothing in this file or its call
// sites consults the returned Action to gate, delay, or reorder anything —
// decisions are recorded as a metric and a debug log and then discarded. A nil
// TaskService.ClaimConstraints disables evaluation entirely, which is the
// default for every deployment that has not opted in through router.go.
//
// The naming follows #6650's "Execution Policy" vocabulary rather than the
// evaluator/checker terms this repository has no precedent for.

// Actions a policy may return. Closed enum: the metric funnel normalizes
// anything else to "allow" (see normalizeClaimConstraintAction).
const (
	claimConstraintAllow = "allow"
	claimConstraintWait  = "wait"
	claimConstraintDeny  = "deny"
)

// Reasons a policy may attach to an action. Closed enum: an unknown reason is
// filed under "other" rather than minting a new series — a value nobody
// classified is not evidence of a classified path.
const (
	claimConstraintReasonAllow  = "allowed"
	claimConstraintReasonCanary = "canary"
	claimConstraintReasonError  = "error"
	claimConstraintReasonOther  = "other"
)

// ClaimConstraintRequest is a candidate-stage snapshot of the claim attempt
// that is about to happen: the head candidate row's identity fields, the poll
// path making the attempt (claimPathRuntime / claimPathBatch, reusing the
// #9184 claim_path semantics), and how many candidate rows this poll saw for
// the agent. It deliberately carries no post-commit state — the evaluation
// happens before claimTask, so nothing about the outcome exists yet. UUIDs are
// pgtype.UUID because the snapshot is copied straight out of the
// db.AgentTaskQueue candidate rows the claim loop already holds; converting to
// another UUID flavor would be a pure tax on the (opt-in) hot path.
type ClaimConstraintRequest struct {
	TaskID     pgtype.UUID
	AgentID    pgtype.UUID
	RuntimeID  pgtype.UUID
	ClaimPath  string
	Candidates int
}

// ClaimConstraintDecision is the policy's verdict: plain Action/Reason
// strings, no error, no structured payload, so the shadow funnel can record
// it without interpretation. It mirrors the entitlement Provider *contract*
// (single method, no error return, fail-open) rather than entitlement's typed
// Decision fields; the enums are closed at the funnel's normalizers, like the
// #9184 outcome constants.
type ClaimConstraintDecision struct {
	Action string
	Reason string
}

// ClaimConstraintPolicy evaluates one candidate-stage claim attempt. It
// deliberately has no error return: every failure is represented by a
// fail-open ClaimConstraintDecision{Action: allow, Reason: error}, mirroring
// the entitlement Provider contract. Implementations must never block, and
// their decision is never enforced — the claim path records it and proceeds.
type ClaimConstraintPolicy interface {
	EvaluateClaimConstraint(ctx context.Context, req ClaimConstraintRequest) ClaimConstraintDecision
}

// ClaimConstraintCanaryPolicy is the built-in pipeline-validation policy: it
// unconditionally returns {allow, canary}. It exists to prove the SPI works
// end to end — request plumbing, metric funnel, shadow discipline — and
// carries no scheduling semantics whatsoever.
type ClaimConstraintCanaryPolicy struct{}

// EvaluateClaimConstraint implements ClaimConstraintPolicy.
func (ClaimConstraintCanaryPolicy) EvaluateClaimConstraint(context.Context, ClaimConstraintRequest) ClaimConstraintDecision {
	return ClaimConstraintDecision{Action: claimConstraintAllow, Reason: claimConstraintReasonCanary}
}

// normalizeClaimConstraintAction keeps the metric's decision label a closed
// enum. An unrecognized action folds to "allow", not "other": in shadow mode
// the action never gates execution, only feeds a label, and folding to allow
// keeps the observable verdict consistent with the interface's fail-open
// contract — a policy that cannot produce a valid action also cannot produce
// an observable denial. The paired reason lands on "other".
func normalizeClaimConstraintAction(action string) string {
	switch action {
	case claimConstraintAllow, claimConstraintWait, claimConstraintDeny:
		return action
	default:
		return claimConstraintAllow
	}
}

// normalizeClaimConstraintReason keeps the metric's reason label a closed
// enum: values outside the four known reasons land on "other".
func normalizeClaimConstraintReason(reason string) string {
	switch reason {
	case claimConstraintReasonAllow, claimConstraintReasonCanary, claimConstraintReasonError, claimConstraintReasonOther:
		return reason
	default:
		return claimConstraintReasonOther
	}
}

// evaluateClaimConstraint is the single call site helper for both claim polls:
// it builds the candidate-stage request, runs the policy, and funnels the
// decision. It runs after the triedAgents mark and before claimTask, and its
// result is discarded — the caller proceeds to claimTask unconditionally. A
// nil ClaimConstraints returns before any work, so the default deployment pays
// one nil check per agent attempt. No recover is needed: the interface has no
// error return and implementations fold their own failures into a fail-open
// decision.
func (s *TaskService) evaluateClaimConstraint(ctx context.Context, taskID, agentID, runtimeID pgtype.UUID, claimPath string, candidates int) {
	if s.ClaimConstraints == nil {
		return
	}
	decision := s.ClaimConstraints.EvaluateClaimConstraint(ctx, ClaimConstraintRequest{
		TaskID:     taskID,
		AgentID:    agentID,
		RuntimeID:  runtimeID,
		ClaimPath:  claimPath,
		Candidates: candidates,
	})
	s.recordClaimConstraintDecision(taskID, agentID, runtimeID, decision, claimPath, candidates)
}

// recordClaimConstraintDecision is the single observability funnel for the
// constraint SPI: metric first, then log, so a logging problem can never lose
// the counter (same discipline as recordClaimDecision). Unlike the empty-poll
// outcomes of the claim decision funnel, this fires only when an evaluator is
// wired and a candidate exists, so the debug line is not a hot-path flood
// risk.
func (s *TaskService) recordClaimConstraintDecision(taskID, agentID, runtimeID pgtype.UUID, decision ClaimConstraintDecision, claimPath string, candidates int) {
	claimPath = normalizeClaimPath(claimPath)
	action := normalizeClaimConstraintAction(decision.Action)
	reason := normalizeClaimConstraintReason(decision.Reason)
	if s.Metrics != nil {
		s.Metrics.RecordClaimConstraintDecision(claimPath, action, reason)
	}
	slog.Debug("task claim constraint decision",
		"action", action,
		"reason", reason,
		"claim_path", claimPath,
		"task_id", util.UUIDToString(taskID),
		"agent_id", util.UUIDToString(agentID),
		"runtime_id", util.UUIDToString(runtimeID),
		"candidates", candidates,
	)
}
