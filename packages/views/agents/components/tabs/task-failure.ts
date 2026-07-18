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
  queued_expired: "Task expired in queue",
  timeout: "Task timed out",
  codex_semantic_inactivity: "Codex semantic inactivity timeout",
  runtime_offline: "Daemon offline",
  runtime_recovery: "Daemon restarted",
  iteration_limit: "Iteration limit reached",
  agent_blocked: "Agent blocked",
  api_invalid_request: "Provider rejected the request",
  "agent_error.provider_auth_or_access": "Provider authentication or access error",
  "agent_error.provider_quota_limit": "Provider quota exhausted",
  "agent_error.provider_capacity_or_rate_limit": "Provider capacity or rate limit reached",
  "agent_error.provider_server_error": "Provider server error",
  "agent_error.provider_network": "Provider network error",
  "agent_error.process_failure": "Agent process failed",
  "agent_error.empty_or_unparseable_output": "Agent returned invalid output",
  "agent_error.agent_timeout": "Agent execution timed out",
  "agent_error.context_overflow": "Model context limit exceeded",
  "agent_error.missing_config": "Runtime configuration missing",
  "agent_error.model_not_found_or_unavailable": "Model unavailable",
  "agent_error.runtime_version_unsupported": "Runtime version unsupported",
  "agent_error.runtime_missing_executable": "Runtime executable missing",
  "agent_error.unknown": "Unknown agent error",
  manual: "Cancelled by user",
};
