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
 */
const FAILURE_REASON_I18N_KEY: Record<string, string> = {
  queued_expired: "queued_expired",
  runtime_offline: "runtime_offline",
  runtime_access_revoked: "runtime_access_revoked",
  runtime_recovery: "runtime_recovery",
  timeout: "timeout",
  iteration_limit: "iteration_limit",
  agent_blocked: "agent_blocked",
  api_invalid_request: "api_invalid_request",
  skill_bundle_unavailable: "skill_bundle_unavailable",
  runtime_cli_timeout: "runtime_cli_timeout",
  environment_prepare_failed: "environment_prepare_failed",
  runtime_access_denied: "runtime_access_denied",
  local_directory_missing: "local_directory_missing",

  "agent_error.provider_auth_or_access": "agent_error_provider_auth_or_access",
  "agent_error.provider_quota_limit": "agent_error_provider_quota_limit",
  "agent_error.provider_capacity_or_rate_limit":
    "agent_error_provider_capacity_or_rate_limit",
  "agent_error.provider_server_error": "agent_error_provider_server_error",
  "agent_error.provider_network": "agent_error_provider_network",
  "agent_error.process_failure": "agent_error_process_failure",
  "agent_error.empty_or_unparseable_output":
    "agent_error_empty_or_unparseable_output",
  "agent_error.agent_timeout": "agent_error_agent_timeout",
  "agent_error.context_overflow": "agent_error_context_overflow",
  "agent_error.missing_config": "agent_error_missing_config",
  "agent_error.model_not_found_or_unavailable":
    "agent_error_model_not_found_or_unavailable",
  "agent_error.runtime_version_unsupported":
    "agent_error_runtime_version_unsupported",
  "agent_error.runtime_missing_executable":
    "agent_error_runtime_missing_executable",
  "agent_error.unknown": "agent_error_unknown",

  agent_error: "agent_error",
  codex_semantic_inactivity: "codex_semantic_inactivity",
  manual: "manual",
};

export const KNOWN_FAILURE_REASONS = Object.keys(FAILURE_REASON_I18N_KEY);

export function runFailureBadgeLabel(
  reason: string | null | undefined,
  t: (key: string) => string,
): string | undefined {
  if (!reason) return undefined;
  const key = FAILURE_REASON_I18N_KEY[reason];
  if (!key) return undefined;
  return t(`runs.failure_badge.${key}`);
}
