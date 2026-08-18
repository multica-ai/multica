// Human-readable copy for the back-end task failure reason. Surfaced in the
// agent detail Recent Work list and the issue execution log — the two places
// the front-end exposes failure_reason directly to the user, and the
// destination the Usage page's Errors breakdown drills into.
//
// Lives next to the consuming tab (rather than in agents/presence) because
// failed tasks no longer have a top-level workload state; failure context
// is purely a detail-page concern now.
//
// Covers the canonical taxonomy in server/pkg/taskfailure — platform-side
// reasons plus 14 `agent_error.*` sub-reasons — and the pre-MUL-1949 coarse
// values still present on historical rows. This used to be a
// `Record<TaskFailureReason, string>` indexed with a cast, which silently
// resolved to `undefined` for every refined reason the backend has written
// since MUL-1949: a task that failed on a provider 401 rendered no reason
// at all.
const REASON_LABEL: Record<string, string> = {
  // Platform / scheduler side.
  queued_expired: "Expired in queue",
  runtime_offline: "Daemon offline",
  runtime_reconnect_timeout: "Daemon did not reconnect in time",
  runtime_recovery: "Daemon restarted",
  timeout: "Task timed out",
  iteration_limit: "Hit the iteration limit",
  agent_blocked: "Waiting on human input",
  api_invalid_request: "Rejected by the model API",
  skill_bundle_unavailable: "Couldn't download the agent's skills",

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

  // Provider-specific operational reasons, outside the canonical taxonomy.
  codex_resume_oversized: "Session too large to resume",
  local_directory_error: "Local directory error",

  // Pre-MUL-1949 coarse values, still present on historical rows.
  agent_error: "Agent execution error",
  codex_semantic_inactivity: "Codex semantic inactivity timeout",
  manual: "Cancelled by user",
};

/**
 * Label for a `failure_reason`, or `null` when there is nothing to show.
 *
 * `failure_reason` is an open string, not a closed enum: the taxonomy grows
 * as classifier rules land, and an installed desktop client will meet
 * reasons its build predates. Those fall back to the raw wire value rather
 * than to nothing — machine-y, but truthful, and it is the exact string an
 * operator would paste into a log search.
 */
export function failureReasonLabel(
  reason: string | null | undefined,
): string | null {
  if (!reason) return null;
  return REASON_LABEL[reason] ?? reason;
}

/**
 * Label for a cancelled task the SERVER cancelled for a persisted reason —
 * the worktree claim gate refusing a too-old daemon, or a cancel-ack carrying
 * a preserved-work error. Those are conditions only the user can fix, so the
 * row must explain itself like a failed row does.
 *
 * Returns `null` for a user-initiated cancel (no persisted reason): the user
 * knows why they clicked, and stamping a label on every ordinary cancel would
 * bury the rows that actually need attention.
 */
export function cancelReasonLabel(task: {
  status: string;
  error?: string | null;
  failure_reason?: string | null;
}): string | null {
  if (task.status !== "cancelled") return null;
  const reason = failureReasonLabel(task.failure_reason);
  if (reason) return reason;
  return task.error ? "Cancelled by the system" : null;
}
