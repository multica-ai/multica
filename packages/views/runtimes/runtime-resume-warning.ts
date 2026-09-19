/**
 * One structured runtime continuity warning, as recorded by the daemon in
 * `agent_runtime.metadata.resume_warning` (see the daemon
 * ReportRuntimeResumeWarning endpoint).
 *
 * `task_id` is deliberately not rendered by default — it is there for support
 * and tooltips; the runtime detail shows the human-readable sentence only.
 */
export type RuntimeResumeWarning = {
  code: "prior_session_resume_unavailable";
  task_id: string;
  occurred_at: string;
};

export const RUNTIME_RESUME_WARNING_CODE = "prior_session_resume_unavailable";

/**
 * Reads the warning out of a runtime metadata blob, failing closed.
 *
 * Metadata is JSONB written by daemons of many versions, so anything malformed
 * (missing key, wrong code, missing fields, a non-object) must render nothing —
 * never throw, and never half-render. Absence of a warning is the normal case.
 */
export function readRuntimeResumeWarning(
  metadata: unknown,
): RuntimeResumeWarning | null {
  if (!metadata || typeof metadata !== "object") {
    return null;
  }
  const raw = (metadata as Record<string, unknown>).resume_warning;
  if (!raw || typeof raw !== "object") {
    return null;
  }
  const entry = raw as Record<string, unknown>;
  if (entry.code !== RUNTIME_RESUME_WARNING_CODE) {
    return null;
  }
  const taskID = typeof entry.task_id === "string" ? entry.task_id.trim() : "";
  const occurredAt =
    typeof entry.occurred_at === "string" ? entry.occurred_at.trim() : "";
  if (taskID === "" || occurredAt === "") {
    return null;
  }
  return {
    code: RUNTIME_RESUME_WARNING_CODE,
    task_id: taskID,
    occurred_at: occurredAt,
  };
}

