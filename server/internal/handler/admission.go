package handler

import (
	"net/http"

	"github.com/multica-ai/multica/server/internal/dispatch"
)

// Unified execution-admission contract (MUL-4525).
//
// Every synchronous enqueue entry point (comment mention, autopilot manual
// "run now", issue assign / promotion / batch, manual rerun, direct chat) needs
// to answer the SAME question in the SAME shape: given a user who explicitly
// named an execution target, did the run get `queued`, `coalesced` onto an
// existing task, `deferred`, or `blocked`? A silent no-op is never acceptable.
//
// Two invariants this contract exists to protect:
//
//  1. Whether the business object was written (comment saved, issue updated)
//     and whether the agent was actually triggered are DIFFERENT facts. A
//     comment can persist while one of its mentions is blocked — callers must
//     be able to express partial success.
//  2. The reason a target was blocked is exposed ONLY as a stable, localizable,
//     enumeration-safe code. It must never leak whether a private agent exists,
//     its name, or its owner to a caller who cannot see the target. The precise
//     cause goes to restricted server logs, not the wire.

// DispatchStatus is the domain-level outcome of one admission/enqueue attempt.
type DispatchStatus string

const (
	// DispatchQueued: a new run was enqueued.
	DispatchQueued DispatchStatus = "queued"
	// DispatchCoalesced: the trigger merged into an already-pending task for
	// the same target instead of creating a duplicate run.
	DispatchCoalesced DispatchStatus = "coalesced"
	// DispatchDeferred: admitted but intentionally not started yet (e.g. a
	// backlog issue parked until promotion, or suppress_run).
	DispatchDeferred DispatchStatus = "deferred"
	// DispatchBlocked: the run was refused. ReasonCode carries why.
	DispatchBlocked DispatchStatus = "blocked"
)

// DispatchReasonCode is the wire-facing admission reason. It aliases the
// canonical, cross-layer enum in the dispatch package so the service (which
// decides the reason at its source) and the handler (which serializes it) can
// never drift. New codes may be added; clients must treat an unknown code as a
// generic failure (they switch with a default branch). A code NEVER encodes the
// existence, name, or owner of a target the caller is not allowed to see.
type DispatchReasonCode = dispatch.ReasonCode

const (
	ReasonQueued                = dispatch.ReasonQueued
	ReasonCoalesced             = dispatch.ReasonCoalesced
	ReasonDeferred              = dispatch.ReasonDeferred
	ReasonInvocationNotAllowed  = dispatch.ReasonInvocationNotAllowed
	ReasonTargetUnavailable     = dispatch.ReasonTargetUnavailable
	ReasonRuntimeOffline        = dispatch.ReasonRuntimeOffline
	ReasonAttributionBlocked    = dispatch.ReasonAttributionBlocked
	ReasonAlreadyActive         = dispatch.ReasonAlreadyActive
	ReasonSelfTriggerSuppressed = dispatch.ReasonSelfTriggerSuppressed
	ReasonInternalError         = dispatch.ReasonInternalError
)

// DispatchTarget is the caller-visible reference to an execution target. Name
// is populated ONLY when the caller is allowed to see the target; a blocked
// private-agent invoke returns Type/ID (already known to the caller from their
// own request) but never a Name they were not otherwise entitled to.
type DispatchTarget struct {
	Type string `json:"type"` // "agent" | "squad"
	ID   string `json:"id"`
	Name string `json:"name,omitempty"`
}

// DispatchOutcome is the unified per-target result returned by every sync
// enqueue entry point. It is additive on the wire: old clients that ignore it
// keep working. TaskID / RunID are set when a run/task was produced.
type DispatchOutcome struct {
	Status     DispatchStatus     `json:"status"`
	ReasonCode DispatchReasonCode `json:"reason_code"`
	Target     *DispatchTarget    `json:"target,omitempty"`
	TaskID     *string            `json:"task_id,omitempty"`
	RunID      *string            `json:"run_id,omitempty"`
}

// dispatchBlockedResponse is the structured body of a blocked synchronous
// enqueue (403/409). `error` is a generic, non-enumerating English fallback for
// old clients that only read the legacy field; `reason_code` is the stable
// machine-readable code new clients localize. Neither field leaks private
// target details.
type dispatchBlockedResponse struct {
	Error      string             `json:"error"`
	ReasonCode DispatchReasonCode `json:"reason_code"`
}

// writeDispatchBlocked writes a structured blocked-admission error. The HTTP
// status conveys the class (403 permission, 409 conflict); reason_code conveys
// the stable cause. Use this for any sync trigger the caller explicitly asked
// for that is refused before mutation.
func (h *Handler) writeDispatchBlocked(w http.ResponseWriter, status int, code DispatchReasonCode) {
	writeJSON(w, status, dispatchBlockedResponse{
		Error:      dispatchBlockedFallbackMessage(code),
		ReasonCode: code,
	})
}

// dispatchBlockedFallbackMessage is the legacy `error` string paired with a
// reason code. It is intentionally generic and non-enumerating: it must be safe
// to show to a caller who is not allowed to know whether the target exists.
func dispatchBlockedFallbackMessage(code DispatchReasonCode) string {
	switch code {
	case ReasonInvocationNotAllowed:
		return "you don't have permission to use this target"
	case ReasonTargetUnavailable:
		return "the target is unavailable"
	case ReasonRuntimeOffline:
		return "the target's runtime is offline"
	case ReasonAttributionBlocked:
		return "the run couldn't be attributed to a responsible member"
	case ReasonAlreadyActive:
		return "a run is already active for this target"
	default:
		return "the run was blocked"
	}
}
