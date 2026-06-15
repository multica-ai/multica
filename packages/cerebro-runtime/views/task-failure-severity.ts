import type { TaskFailureReason } from "@multica/core/types";

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
// Keep this list in sync with the failure reasons the resume path treats
// as transient (resume_scope / ListResumableTasksForRuntime category 2):
// runtime_recovery, runtime_paused, runtime_offline, rate_limit.
const INTERRUPTION_REASONS: ReadonlySet<TaskFailureReason> = new Set<TaskFailureReason>([
  "runtime_recovery", // daemon restarted mid-run (e.g. binary auto-update)
  "runtime_paused", // runtime paused (manual or auto); resumes on unpause
  "runtime_offline", // daemon went offline mid-run
  "rate_limit", // provider usage/rate limit; auto-retried when it clears
]);

// True when a failed task ended because the runtime/provider interrupted
// it (and it is auto-retried), not because the agent's work failed.
// Accepts the wire shape of `failure_reason`, which the back-end emits as
// "" (omitempty) on non-failed tasks.
export function isInterruptionReason(
  reason: TaskFailureReason | "" | undefined | null,
): boolean {
  return reason != null && reason !== "" && INTERRUPTION_REASONS.has(reason);
}
