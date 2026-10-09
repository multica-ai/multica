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
/**
 * Must stay in lockstep with the `failure_reason` keys in
 * `locales/en/chat.json`; the drift test in `failure-reason-label.test.ts`
 * enforces both directions.
 */
export const REASONS = new Set([
  // Platform / scheduler side.
  "queued_expired",
  "runtime_offline",
  "runtime_reconnect_timeout",
  "runtime_recovery",
  "timeout",
  "iteration_limit",
  "agent_blocked",
  "api_invalid_request",
  "skill_bundle_unavailable",
  "runtime_cli_timeout",
  "environment_prepare_failed",
  "invalid_task_identity",
  "runtime_access_denied",

  // Agent process side — provider.
  "agent_error.provider_auth_or_access",
  "agent_error.provider_quota_limit",
  "agent_error.provider_capacity_or_rate_limit",
  "agent_error.provider_server_error",
  "agent_error.provider_network",

  // Agent process side — agent / runner.
  "agent_error.process_failure",
  "agent_error.empty_or_unparseable_output",
  "agent_error.agent_timeout",
  "agent_error.context_overflow",
  "agent_error.missing_config",
  "agent_error.model_not_found_or_unavailable",
  "agent_error.runtime_version_unsupported",
  "agent_error.runtime_missing_executable",
  "agent_error.unknown",

  // Daemon operational reasons, outside the canonical taxonomy.
  "agent_fallback_message",
  "codex_resume_oversized",
  "idle_watchdog",
  "local_directory_error",
  "cancelled",

  // Coarse values, still present on historical rows.
  "agent_error",
  "codex_semantic_inactivity",
  "manual",
  "user_cancelled",
]);

export function failureReasonKey(reason: string | null | undefined): string {
  if (!reason || !REASONS.has(reason)) return "failure_reason.default";
  // Refined wire reasons use dots (`agent_error.provider_auth_or_access`);
  // the mobile bundle deliberately stores those as flat snake_case keys.
  return `failure_reason.${reason.replaceAll(".", "_")}`;
}
