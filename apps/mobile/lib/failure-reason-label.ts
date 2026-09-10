/**
 * Mirror of `packages/views/agents/components/tabs/task-failure.ts:REASON_LABEL`.
 *
 * Why mirror: mobile cannot import from packages/views per the apps/mobile
 * CLAUDE.md sharing rule. Only the human copy is mobile-owned.
 *
 * Keyed by the raw wire value rather than a closed enum, same as the web map:
 * `failure_reason` is an open string that grows as classifier rules land, and
 * an installed build will meet reasons it predates. Before MUL-5370 this was a
 * `Record<TaskFailureReason, string>` holding only the six pre-MUL-1949 coarse
 * values, so every refined `agent_error.*` the backend has written since
 * missed the lookup and rendered a bare "Failed".
 *
 * Divergence from web, deliberate: the web helper falls back to the raw wire
 * value, which is machine-y but searchable — right for an operator reading the
 * execution log. This one backs a chat bubble read by the person who just sent
 * a message, so an unrecognised reason degrades to a plain "Failed" instead of
 * leaking an enum string at them.
 */
const LABELS: Record<string, string> = {
  // Platform / scheduler side.
  queued_expired: "Expired in queue",
  runtime_offline: "Daemon offline",
  runtime_recovery: "Daemon restarted",
  timeout: "Task timed out",
  iteration_limit: "Hit the iteration limit",
  agent_blocked: "Waiting on human input",
  api_invalid_request: "Rejected by the model API",
  skill_bundle_unavailable: "Couldn't download the agent's skills",
  runtime_cli_timeout: "Local runtime CLI timed out",
  environment_prepare_failed: "Couldn't prepare the execution environment",

  // Agent process side — provider.
  "agent_error.provider_auth_or_access": "Provider auth failed",
  "agent_error.provider_quota_limit": "Provider quota exhausted",
  "agent_error.provider_capacity_or_rate_limit": "Rate limited by provider",
  "agent_error.provider_server_error": "Provider server error",
  "agent_error.provider_network": "Network error reaching provider",

  // Agent process side — agent / runner.
  "agent_error.process_failure": "Agent process crashed",
  "agent_error.empty_or_unparseable_output": "Agent returned no usable output",
  "agent_error.agent_timeout": "Agent timed out",
  "agent_error.context_overflow": "Context window exceeded",
  "agent_error.missing_config": "Missing API key or configuration",
  "agent_error.model_not_found_or_unavailable": "Model unavailable",
  "agent_error.runtime_version_unsupported": "Runner CLI version unsupported",
  "agent_error.runtime_missing_executable": "Runner CLI not installed",
  "agent_error.unknown": "Agent execution error",

  // Pre-MUL-1949 coarse values, still present on historical rows.
  agent_error: "Agent execution error",
  codex_semantic_inactivity: "Codex semantic inactivity timeout",
  manual: "Cancelled by user",
};

export function failureReasonLabel(reason: string | null | undefined): string {
  if (!reason) return "Failed";
  return LABELS[reason] ?? "Failed";
}
