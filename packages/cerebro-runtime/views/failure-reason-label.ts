// Human copy for every canonical task failure reason.
//
// The backend defines 21 reasons in server/pkg/taskfailure/failure.go and
// persists their string form on agent_task_queue.failure_reason. The upstream
// map in packages/views/agents/components/tabs/task-failure.ts predates the
// agent_error.* split: of its 14 entries only 6 are still emitted, so 15
// reasons — including every agent_error.* one, the most common failure class —
// resolved to undefined and rendered blank.
//
// This resolver takes a plain string rather than the TaskFailureReason union
// so a backend that grows a 22nd reason degrades to readable copy instead of
// rendering nothing, and so no upstream type has to change.
const REASON_LABELS: Readonly<Record<string, string>> = {
  // Platform / scheduler side.
  queued_expired: "Waited in the queue too long",
  runtime_offline: "The computer running the agent went offline",
  runtime_recovery: "The agent runner restarted mid-run",
  timeout: "The run hit its time limit",
  iteration_limit: "The agent hit its step limit",
  agent_blocked: "The agent stopped and marked itself blocked",
  api_invalid_request: "The model provider rejected the request",
  // Agent side.
  "agent_error.provider_auth_or_access": "Not signed in to the model provider",
  "agent_error.provider_quota_limit": "Usage quota is used up",
  "agent_error.provider_capacity_or_rate_limit":
    "The model provider was rate-limiting or at capacity",
  "agent_error.provider_server_error": "The model provider had a server error",
  "agent_error.provider_network": "The connection to the model provider dropped",
  "agent_error.process_failure": "The agent process crashed",
  "agent_error.empty_or_unparseable_output": "The agent returned nothing usable",
  "agent_error.agent_timeout": "The agent timed out internally",
  "agent_error.context_overflow": "The conversation grew past the model's limit",
  "agent_error.missing_config": "The agent is missing required configuration",
  "agent_error.model_not_found_or_unavailable": "The requested model is unavailable",
  "agent_error.runtime_version_unsupported": "The agent runner version is too old",
  "agent_error.runtime_missing_executable":
    "The agent program is not installed on that computer",
  "agent_error.unknown": "The agent failed for an unrecognised reason",
  workflow_gate_rejected: "Stopped by Workflow hook",
};

const AGENT_ERROR_PREFIX = "agent_error.";

/** Turn a snake_case token into spaced words: "brand_new_thing" -> "brand new thing". */
function humanizeToken(token: string): string {
  return token.replace(/_/g, " ").trim();
}

/**
 * Human copy for a task failure reason. Returns null when the task has no
 * reason — the wire shape is "" on non-failed tasks.
 */
export function resolveFailureReasonLabel(
  reason: string | null | undefined,
): string | null {
  if (reason == null || reason === "") return null;

  const known = REASON_LABELS[reason];
  if (known !== undefined) return known;

  if (reason.startsWith(AGENT_ERROR_PREFIX)) {
    const sub = humanizeToken(reason.slice(AGENT_ERROR_PREFIX.length));
    return sub.length > 0 ? `Agent error: ${sub}` : "Agent error";
  }

  const humanized = humanizeToken(reason);
  if (humanized.length === 0) return null;
  return humanized.charAt(0).toUpperCase() + humanized.slice(1);
}

/** A completed run can carry a non-failing Workflow hook warning in result. */
export function resolveWorkflowGateWarningLabel(result: unknown): string | null {
  if (result == null || typeof result !== "object") return null;
  const warning = (result as { completion_warning?: unknown }).completion_warning;
  if (warning == null || typeof warning !== "object") return null;
  const record = warning as { code?: unknown; hook_name?: unknown };
  if (record.code !== "workflow_gate_rejected") return null;
  const hookName = typeof record.hook_name === "string" ? record.hook_name.trim() : "";
  return hookName ? `Stopped by hook: ${hookName}` : "Stopped by Workflow hook";
}
