package providerfailover

import "github.com/multica-ai/multica/server/pkg/taskfailure"

// Mode is the operational posture of the failover subsystem.
type Mode string

const (
	// ModeOff disables failover entirely — Decide is never called.
	ModeOff Mode = "off"
	// ModeShadow is the safe default: the full policy verdict is computed and
	// recorded, but no task outcome, agent binding, or dispatch changes.
	ModeShadow Mode = "shadow"
	// ModeActive performs real handoffs: claim chain ownership, dispatch a
	// Claude fallback (or fail explicitly when Claude is unavailable).
	ModeActive Mode = "active"
)

// Outcome is the top-level action Decide directs the caller to take.
type Outcome string

const (
	// OutcomeProceed: active mode, policy approves — claim ownership and dispatch.
	OutcomeProceed Outcome = "proceed"
	// OutcomeUnavailable: active mode, policy approves but Claude is unavailable —
	// record an explicit failure with a user-visible ledger reference.
	OutcomeUnavailable Outcome = "unavailable"
	// OutcomeDeclined: active mode, policy declines — record and take no action.
	OutcomeDeclined Outcome = "declined"
	// OutcomeShadow: shadow mode — record the verdict only, change nothing.
	OutcomeShadow Outcome = "shadow"
)

// Decline reason codes. Stable strings: persisted in the ledger's decline_reason
// column and surfaced in the observability API.
const (
	ReasonNotTrigger         = "not_a_failover_trigger"
	ReasonSourceIneligible   = "source_provider_not_failover_eligible"
	ReasonAlreadyFallback    = "loop_prevented_already_fallback"
	ReasonMaxOnePerChain     = "max_one_handoff_per_chain"
	ReasonAuthoritySensitive = "authority_sensitive_excluded"
	ReasonCancelled          = "cancelled"
	ReasonSideEffects        = "side_effects_present"
	// ReasonSideEffectsUnproven is an ACTIVE-mode-only safety hold: the run is
	// otherwise eligible, but the server cannot prove the failed run's full
	// side-effect surface is empty (in-run tool calls, head-SHA movement, and
	// partial streamed output are not plumbed to the server fail path). A real
	// handoff would risk duplicating those effects, so active declines. Shadow
	// still evaluates on the observable subset — see SideEffectsComplete.
	ReasonSideEffectsUnproven = "side_effect_completeness_unproven"
	// ReasonOrchestratorIdempotencyUnproven is an ACTIVE-mode-only safety hold
	// for ORCHESTRATOR-tier runs (td-836aa9). An orchestrator dispatches
	// control-plane effects (child task-spawns, stage promotions); handing one
	// off mid-orchestration risks the fallback re-dispatching them
	// (double-spawn / double-promote). Active mode therefore refuses to hand off
	// an orchestrator-tier run unless the caller proves those effects route
	// through the idempotency ledger (Input.ControlPlaneIdempotent). Shadow
	// still records the would-fail-over verdict so orchestrator coverage is
	// observable before it is enabled.
	ReasonOrchestratorIdempotencyUnproven = "orchestrator_idempotency_unproven"
	ReasonClaudeUnavailable               = "claude_unavailable"
	ReasonEligible                        = "eligible"
)

// Input is everything the pure policy needs. The caller (service layer)
// gathers it from the failed task, its agent, the chain state, and the
// side-effect snapshot.
type Input struct {
	// FailureReason is the refined reason the primary run failed with.
	FailureReason taskfailure.Reason
	// SourceProvider is the failed run's runtime provider. Only the OpenAI/GPT
	// runtime provider ("codex") is in scope; see IsFailoverSource.
	SourceProvider string
	// Mode is the current operational posture (shadow/active).
	Mode Mode

	// AuthoritySensitive is true for agents that must never be silently handed
	// off. The caller sets this for system-kind agents and for user agents with
	// runtime_config.provider_failover_protected=true. The exact legacy identity
	// "Protected Reviewer" is retained as a compatibility exclusion; other names
	// and system_key substrings are deliberately ignored — see
	// agentAuthoritySensitive.
	AuthoritySensitive bool

	// IsAlreadyFallback is true when the FAILED task is itself a Claude fallback
	// produced by a prior handoff. Such a task is never handed off again — this
	// is the loop guard.
	IsAlreadyFallback bool
	// ChainHasOwningHandoff is true when another handoff already owns this task
	// chain (PENDING/DISPATCHED/COMPLETED). Enforces at-most-one per chain in
	// the policy, complementing the DB unique partial index that enforces it
	// atomically under concurrency.
	ChainHasOwningHandoff bool

	// Cancelled is true when the run ended by cancellation rather than a genuine
	// provider limit. Cancelled runs never reach FailTask in practice, but the
	// policy declines defensively.
	Cancelled bool

	// SideEffects is the pre-fallback ledger snapshot.
	SideEffects SideEffects
	// SideEffectsComplete is true only when the caller can PROVE the snapshot
	// above captures the failed run's entire side-effect surface. It is an
	// active-mode safety requirement, not part of the mode-independent
	// eligibility verdict: shadow evaluates the observable subset regardless,
	// but active refuses to hand off unless completeness is proven (see
	// ReasonSideEffectsUnproven). The server cannot currently prove this, so
	// active handoffs are held closed until the daemon plumbs in-run tool-call,
	// head-SHA, and partial-output signals.
	SideEffectsComplete bool

	// OrchestratorTier is true when the failed run is an ORCHESTRATOR-tier task
	// (it coordinates other work — e.g. an autopilot run, or a parent issue that
	// spawns children and promotes stages) rather than a leaf ACTOR that only
	// executes. Orchestrator-tier runs were originally excluded from failover
	// entirely; coverage is now extended to them (td-836aa9), but a REAL handoff
	// of one is gated on ControlPlaneIdempotent below. Mode-independent: shadow
	// records the would-fail-over verdict for orchestrator runs regardless.
	OrchestratorTier bool
	// ControlPlaneIdempotent asserts that this run's control-plane effects
	// (child task-spawns, stage promotions) are guarded by the idempotency
	// ledger, so a handed-off fallback that re-plans cannot double-dispatch
	// them. Only consulted for OrchestratorTier runs in ACTIVE mode: an
	// orchestrator handoff is held closed (ReasonOrchestratorIdempotencyUnproven)
	// until this is proven. Ignored for actor-tier runs and in shadow.
	ControlPlaneIdempotent bool

	// ClaudeAvailable is true when an eligible Claude target agent/runtime
	// exists for this workspace. Only consulted once the run is otherwise
	// eligible, so the availability probe is skipped on the common decline path.
	ClaudeAvailable bool
}

// Decision is the auditable output of the policy.
type Decision struct {
	// Outcome is the action the caller should take.
	Outcome Outcome
	// State is the ledger state to persist for this decision.
	State HandoffState
	// WouldFailOver reports whether the run is eligible for a real handoff,
	// independent of mode. In shadow mode this is the whole point of the record:
	// "active mode would / would not have handed off, and why".
	WouldFailOver bool
	// Reason is the machine-readable decline/eligibility code (a Reason* const).
	Reason string
	// TargetProvider is the intended handoff direction — the most-preferred
	// eligible target provider for this source (PrimaryTargetFor). Recorded on
	// every decision (including shadow and declines) so the ledger shows which
	// way a handoff would go; "" when the source is not a failover participant.
	TargetProvider string
}

// eligibility computes the mode-independent verdict: would a real handoff be
// warranted, and if not, why. It does NOT consult ClaudeAvailable — that is an
// operational condition applied per-mode by Decide, so shadow records "would
// fail over" even when Claude happens to be down.
func eligibility(in Input) (would bool, reason string) {
	if !IsFailoverTrigger(in.FailureReason) {
		return false, ReasonNotTrigger
	}
	if !IsFailoverSource(in.SourceProvider) {
		return false, ReasonSourceIneligible
	}
	if in.IsAlreadyFallback {
		return false, ReasonAlreadyFallback
	}
	if in.ChainHasOwningHandoff {
		return false, ReasonMaxOnePerChain
	}
	if in.AuthoritySensitive {
		return false, ReasonAuthoritySensitive
	}
	if in.Cancelled {
		return false, ReasonCancelled
	}
	if in.SideEffects.HasObservableSideEffects() {
		return false, ReasonSideEffects
	}
	return true, ReasonEligible
}

// Decide is the pure policy. It is fail-closed: any condition it cannot
// affirmatively clear resolves to "do not hand off".
//
// Shadow mode always yields OutcomeShadow and records the full eligibility
// verdict (WouldFailOver + Reason) without acting — this is the safe default
// and the mechanism by which a rollout is evaluated before enabling active
// handoffs.
//
// Active mode maps the eligibility verdict onto real actions:
//
//   - not eligible                              → OutcomeDeclined    (HANDOFF_DECLINED)
//   - eligible but side effects unproven         → OutcomeDeclined    (HANDOFF_DECLINED)
//   - eligible orchestrator, effects not idempotent → OutcomeDeclined (HANDOFF_DECLINED)
//   - eligible but target unavailable             → OutcomeUnavailable (HANDOFF_FAILED)
//   - eligible and target available               → OutcomeProceed     (HANDOFF_PENDING)
func Decide(in Input) Decision {
	would, reason := eligibility(in)
	// The intended handoff direction, recorded on every decision (including
	// shadow/declines) for auditability; "" when the source is ineligible.
	target := PrimaryTargetFor(in.SourceProvider)

	if in.Mode == ModeShadow {
		return Decision{
			Outcome:        OutcomeShadow,
			State:          StateShadow,
			WouldFailOver:  would,
			Reason:         reason,
			TargetProvider: target,
		}
	}

	// Active mode.
	if !would {
		return Decision{
			Outcome:        OutcomeDeclined,
			State:          StateDeclined,
			WouldFailOver:  false,
			Reason:         reason,
			TargetProvider: target,
		}
	}
	// Active-mode safety hold: never hand off unless the run's full side-effect
	// surface is provably empty. Shadow already recorded WouldFailOver on the
	// observable subset; active must not act on an incomplete picture.
	if !in.SideEffectsComplete {
		return Decision{
			Outcome:        OutcomeDeclined,
			State:          StateDeclined,
			WouldFailOver:  false,
			Reason:         ReasonSideEffectsUnproven,
			TargetProvider: target,
		}
	}
	// Active-mode safety hold: never hand off an ORCHESTRATOR-tier run unless its
	// control-plane effects are idempotency-guarded, or the fallback could
	// re-spawn children / re-promote stages the primary already dispatched.
	// Actor-tier runs skip this gate. Shadow already recorded the coverage.
	if in.OrchestratorTier && !in.ControlPlaneIdempotent {
		return Decision{
			Outcome:        OutcomeDeclined,
			State:          StateDeclined,
			WouldFailOver:  false,
			Reason:         ReasonOrchestratorIdempotencyUnproven,
			TargetProvider: target,
		}
	}
	if !in.ClaudeAvailable {
		return Decision{
			Outcome:        OutcomeUnavailable,
			State:          StateFailed,
			WouldFailOver:  true,
			Reason:         ReasonClaudeUnavailable,
			TargetProvider: target,
		}
	}
	return Decision{
		Outcome:        OutcomeProceed,
		State:          StatePending,
		WouldFailOver:  true,
		Reason:         ReasonEligible,
		TargetProvider: target,
	}
}
