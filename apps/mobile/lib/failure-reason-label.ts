/**
 * Mirror of `packages/views/agents/components/tabs/task-failure.ts:failureReasonLabel`.
 *
 * Why mirror: mobile cannot import from packages/views per the apps/mobile
 * CLAUDE.md sharing rule. The enum itself comes from packages/core/types
 * (type-only import is fine); only the human copy is mobile-owned.
 *
 * Used by the destructive chat bubble. The default branch handles enum
 * drift — unknown values render a generic "Failed" rather than crashing
 * or rendering the raw enum string, matching the root CLAUDE.md "Enum
 * drift downgrades, not crashes" rule.
 */
import type { TaskFailureReason } from "@multica/core/types";

const LABELS: Record<TaskFailureReason, string> = {
  agent_error: "Agent execution error",
  timeout: "Task timed out",
  codex_semantic_inactivity: "Codex semantic inactivity timeout",
  runtime_offline: "Daemon offline",
  runtime_recovery: "Daemon restarted",
  manual: "Cancelled by user",
  runtime_paused: "Runtime paused",
  rate_limit: "Usage or rate limit",
  auth_error: "Authentication failed",
  queued_expired: "Queue expired",
  idle_watchdog: "No activity timeout",
  iteration_limit: "Iteration limit reached",
  agent_fallback_message: "Fallback response",
  api_invalid_request: "Invalid provider request",
};

export function failureReasonLabel(
  reason: TaskFailureReason | string | null | undefined,
): string {
  if (!reason) return "Failed";
  if (reason in LABELS) {
    return LABELS[reason as TaskFailureReason];
  }
  return "Failed";
}
