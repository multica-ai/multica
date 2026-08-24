import type { useT } from "../../../i18n";

type AgentsT = ReturnType<typeof useT<"agents">>["t"];

// Stable translation keys for the canonical server taxonomy, provider-specific
// operational reasons, and coarse values still present on historical rows.
// The wire value remains an open string: clients can meet a newer reason than
// they know, and failureReasonLabel deliberately falls back to that raw value.
export const FAILURE_REASON_I18N_KEYS = {
  // Platform / scheduler side.
  queued_expired: "queued_expired",
  runtime_offline: "runtime_offline",
  runtime_reconnect_timeout: "runtime_reconnect_timeout",
  runtime_recovery: "runtime_recovery",
  timeout: "timeout",
  iteration_limit: "iteration_limit",
  agent_blocked: "agent_blocked",
  api_invalid_request: "api_invalid_request",
  skill_bundle_unavailable: "skill_bundle_unavailable",
  runtime_cli_timeout: "runtime_cli_timeout",
  invalid_task_identity: "invalid_task_identity",
  issue_window_restricted: "issue_window_restricted",

  // Agent process side — provider.
  "agent_error.provider_auth_or_access":
    "agent_error_provider_auth_or_access",
  "agent_error.provider_quota_limit": "agent_error_provider_quota_limit",
  "agent_error.provider_capacity_or_rate_limit":
    "agent_error_provider_capacity_or_rate_limit",
  "agent_error.provider_server_error": "agent_error_provider_server_error",
  "agent_error.provider_network": "agent_error_provider_network",

  // Agent process side — agent / runner.
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

  // Daemon operational reasons, outside the canonical taxonomy.
  agent_fallback_message: "agent_fallback_message",
  codex_semantic_inactivity: "codex_semantic_inactivity",
  codex_resume_oversized: "codex_resume_oversized",
  idle_watchdog: "idle_watchdog",
  local_directory_error: "local_directory_error",
  cancelled: "cancelled",

  // Pre-MUL-1949 coarse values, still present on historical rows.
  agent_error: "agent_error",
  manual: "manual",
  user_cancelled: "user_cancelled",
} as const;

type KnownFailureReason = keyof typeof FAILURE_REASON_I18N_KEYS;

/**
 * Localized label for a `failure_reason`, or `null` when there is nothing to
 * show. Unknown values remain raw so an older installed client degrades
 * truthfully when a newer backend adds a reason.
 */
export function failureReasonLabel(
  reason: string | null | undefined,
  t: AgentsT,
): string | null {
  if (!reason) return null;
  if (!Object.prototype.hasOwnProperty.call(FAILURE_REASON_I18N_KEYS, reason)) {
    return reason;
  }
  const key = FAILURE_REASON_I18N_KEYS[reason as KnownFailureReason];
  return t(($) => $.task_failure.reasons[key]);
}

/**
 * Label for a cancelled task the server cancelled for a persisted reason.
 * User-initiated cancellation has no reason and stays unlabeled.
 */
export function cancelReasonLabel(
  task: {
    status: string;
    error?: string | null;
    failure_reason?: string | null;
  },
  t: AgentsT,
): string | null {
  if (task.status !== "cancelled") return null;
  const reason = failureReasonLabel(task.failure_reason, t);
  if (reason) return reason;
  return task.error ? t(($) => $.task_failure.cancelled_by_system) : null;
}
