/**
 * Short badge copy for a run's `failure_reason`, shown inline on the agent-runs
 * row next to the status word and a timestamp.
 *
 * Deliberately terser than `lib/failure-reason-label.ts`, which backs a
 * full-width chat bubble; this one shares a single line. The copy lives in the
 * `issues` namespace under `runs.failure_badge.*`.
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
 *
 * Must stay in lockstep with `runs.failure_badge` in every locale bundle; the
 * drift test in `run-failure-badge.test.ts` enforces it.
 */
export const FAILURE_BADGE_REASONS = new Set([
  "queued_expired",
  "runtime_offline",
  "runtime_access_revoked",
  "runtime_recovery",
  "timeout",
  "iteration_limit",
  "agent_blocked",
  "api_invalid_request",
  "skill_bundle_unavailable",
  "runtime_cli_timeout",
  "environment_prepare_failed",
  "runtime_access_denied",
  "local_directory_missing",

  "agent_error.provider_auth_or_access",
  "agent_error.provider_quota_limit",
  "agent_error.provider_capacity_or_rate_limit",
  "agent_error.provider_server_error",
  "agent_error.provider_network",
  "agent_error.process_failure",
  "agent_error.empty_or_unparseable_output",
  "agent_error.agent_timeout",
  "agent_error.context_overflow",
  "agent_error.missing_config",
  "agent_error.model_not_found_or_unavailable",
  "agent_error.runtime_version_unsupported",
  "agent_error.runtime_missing_executable",
  "agent_error.unknown",

  "agent_error",
  "codex_semantic_inactivity",
  "manual",
]);

/**
 * i18next treats `.` as a nesting separator, so refined wire reasons
 * (`agent_error.provider_network`) are stored as flat snake_case keys, the
 * same convention as `failure_reason.*` in the chat bundle.
 */
export function failureBadgeKey(reason: string): string {
  return reason.replaceAll(".", "_");
}

export function runFailureBadgeLabel(
  reason: string | null | undefined,
  t: (key: string) => string,
): string | undefined {
  if (!reason || !FAILURE_BADGE_REASONS.has(reason)) return undefined;
  return t(`runs.failure_badge.${failureBadgeKey(reason)}`);
}
