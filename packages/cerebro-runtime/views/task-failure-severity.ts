// Classifies a task `failure_reason` as a transient *interruption* — the
// runtime or provider stopped the run and the platform auto-retries it —
// versus a genuine failure the user should act on.
//
// Why this exists: when a daemon is restarted (e.g. a binary auto-update)
// or a runtime is paused/rate-limited mid-run, every in-flight task is
// stamped status='failed' (see SuspendActiveTasksForRuntime /
// RecoverOrphanedTasksForRuntime). The work itself did not fail — it is
// resumed via CreateRetryTask, which carries the agent's session forward.
// Rendering those rows as a red "Failed" makes a run that actually
// delivered look broken. The execution log and the agent Recent Work tab
// use this predicate to render interruptions as an amber "Interrupted"
// state instead, so only real failures read as red.
//
// FIR-3782: the previous set keyed on "runtime_paused" and "rate_limit".
// Neither string is emitted any more — the in-flight classifier
// (server/pkg/taskfailure/classify.go) resolves a provider stall to one of
// the agent_error.* sub-reasons below, and Classify never returns a
// platform-side reason. Keying on the dead strings meant an auto-retried
// rate limit rendered as a hard red failure with no explanation.
//
// Keep this list in sync with the failure reasons the resume path treats
// as transient (resume_scope / ListResumableTasksForRuntime category 2).
const INTERRUPTION_REASONS: ReadonlySet<string> = new Set<string>([
  "runtime_recovery", // daemon restarted mid-run (e.g. binary auto-update)
  "runtime_offline", // daemon went offline mid-run
  "agent_error.provider_capacity_or_rate_limit", // provider rate-limited or at capacity
  "agent_error.provider_server_error", // provider 5xx; retried when it clears
  "agent_error.provider_network", // stream disconnected mid-run
]);

// True when a failed task ended because the runtime/provider interrupted
// it (and it is auto-retried), not because the agent's work failed.
// Accepts a plain string: the wire shape is "" (omitempty) on non-failed
// tasks, and a reason the front-end type does not know about must not
// break the call site.
export function isInterruptionReason(
  reason: string | null | undefined,
): boolean {
  return reason != null && reason !== "" && INTERRUPTION_REASONS.has(reason);
}
