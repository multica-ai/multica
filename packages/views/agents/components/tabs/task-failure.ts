import type { TaskFailureReason } from "@multica/core/types";

// Human-readable copy for the back-end task failure reason enum. Surfaced
// in the agent detail Recent Work tab when a task ended in failure — the
// only place the front-end exposes failure_reason directly to the user.
//
// Lives next to the consuming tab (rather than in agents/presence) because
// failed tasks no longer have a top-level workload state; failure context
// is purely a detail-page concern now.
export const failureReasonLabel: Record<TaskFailureReason, string> = {
  agent_error: "Agent execution error",
  cancelled: "Cancelled",
  timeout: "Task timed out",
  rate_limited: "Rate limited",
  parse_error: "Agent output error",
  upstream_failure: "Upstream API failure",
  runtime_offline: "Daemon offline",
  runtime_recovery: "Daemon restarted",
  queued_expired: "Queue expired",
  unknown: "Unknown error",
  manual: "Cancelled by user",
};
