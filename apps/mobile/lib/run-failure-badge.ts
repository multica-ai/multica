/**
 * Short badge copy for a run's `failure_reason`, shown inline on the agent-runs
 * row next to the status word and a timestamp.
 *
 * Deliberately terser than `lib/failure-reason-label.ts`, which backs a
 * full-width chat bubble; this one shares a single line.
 *
 * Keyed by the raw wire value, not a closed enum: `failure_reason` is an open
 * string that grows as classifier rules land, and an installed build will meet
 * reasons it predates. An unrecognised reason returns undefined so the row
 * falls back to a bare status word — a compact badge is the one place where
 * web's raw-wire-value fallback would overflow the row.
 *
 * Lives in lib/ rather than inside run-row.tsx so the lookup is covered by
 * mobile's node-only vitest lane: the map spent from MUL-5370 to #7913 holding
 * copy that no run could reach, because AgentTaskSchema was erasing every
 * refined reason before the row ever looked one up. A map no test can see is
 * how that goes unnoticed.
 */
const FAILURE_REASON_BADGE: Record<string, string> = {
  queued_expired: "Queue expired",
  runtime_offline: "Runtime offline",
  runtime_recovery: "Runtime recovery",
  timeout: "Timeout",
  iteration_limit: "Iteration limit",
  agent_blocked: "Needs input",
  api_invalid_request: "Request rejected",
  skill_bundle_unavailable: "Skill download failed",
  runtime_cli_timeout: "Runtime CLI timeout",
  environment_prepare_failed: "Environment setup failed",

  "agent_error.provider_auth_or_access": "Auth failed",
  "agent_error.provider_quota_limit": "Quota exhausted",
  "agent_error.provider_capacity_or_rate_limit": "Rate limited",
  "agent_error.provider_server_error": "Provider error",
  "agent_error.provider_network": "Network error",
  "agent_error.process_failure": "Process crashed",
  "agent_error.empty_or_unparseable_output": "No usable output",
  "agent_error.agent_timeout": "Agent timeout",
  "agent_error.context_overflow": "Context overflow",
  "agent_error.missing_config": "Config missing",
  "agent_error.model_not_found_or_unavailable": "Model unavailable",
  "agent_error.runtime_version_unsupported": "CLI unsupported",
  "agent_error.runtime_missing_executable": "CLI not installed",
  "agent_error.unknown": "Agent error",

  agent_error: "Agent error",
  codex_semantic_inactivity: "Codex inactivity",
  manual: "Manual",
};

export function runFailureBadgeLabel(
  reason: string | null | undefined,
): string | undefined {
  if (!reason) return undefined;
  return FAILURE_REASON_BADGE[reason];
}
