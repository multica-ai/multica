// Package dispatch holds the canonical, cross-layer vocabulary for execution
// admission outcomes (MUL-4525). It is a leaf package (no internal deps) so both
// the service layer — which MAKES the admission/skip decision and therefore owns
// the reason at its source — and the handler layer — which serializes it to the
// wire — share one enum and can never drift.
//
// A ReasonCode is decided at the branch that blocks/skips a run and carried
// through to the response verbatim; it is never reverse-engineered from a
// human-readable failure string. Codes are stable, localizable by clients, and
// enumeration-safe: a code never reveals whether a private agent exists, its
// name, or its owner.
package dispatch

// ReasonCode is a stable, client-localizable admission/dispatch reason.
type ReasonCode string

const (
	// ReasonQueued / ReasonCoalesced / ReasonDeferred are the success-path codes.
	ReasonQueued    ReasonCode = "queued"
	ReasonCoalesced ReasonCode = "coalesced"
	ReasonDeferred  ReasonCode = "deferred"

	// ReasonInvocationNotAllowed: the acting principal may not trigger this
	// target under the invocation-permission model. Deliberately generic — it
	// does not distinguish "target is private" from "target does not exist".
	ReasonInvocationNotAllowed ReasonCode = "invocation_not_allowed"
	// ReasonTargetUnavailable: the target cannot run (archived agent, deleted /
	// archived squad, unresolvable leader, or no assignee).
	ReasonTargetUnavailable ReasonCode = "target_unavailable"
	// ReasonRuntimeOffline: the target is permitted but its runtime is not bound
	// / not online at dispatch time.
	ReasonRuntimeOffline ReasonCode = "runtime_offline"
	// ReasonAttributionBlocked: a fail-closed workspace could not resolve a
	// responsible human for the run, so it was refused.
	ReasonAttributionBlocked ReasonCode = "attribution_blocked"
	// ReasonAlreadyActive: a run is already active/pending for this target and
	// this trigger did not coalesce.
	ReasonAlreadyActive ReasonCode = "already_active"
	// ReasonSelfTriggerSuppressed: the target was intentionally not (re-)triggered
	// because doing so would be a self-trigger the guard suppresses, and no active
	// run remains to cover it — e.g. a squad leader's own @mention of its squad
	// whose latest task is already terminal. Not a permission block, but NOT
	// success: nothing new runs. (Named to avoid implying the NEW comment was
	// already processed.)
	ReasonSelfTriggerSuppressed ReasonCode = "self_trigger_suppressed"
	// ReasonInternalError: an unexpected server error prevented a clean decision.
	ReasonInternalError ReasonCode = "internal_error"
)
