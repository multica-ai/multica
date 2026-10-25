import type { AutopilotRun } from "@multica/core/types";

export type RunStatus = "issue_created" | "queued" | "running" | "skipped" | "completed" | "failed";

const KNOWN: ReadonlySet<string> = new Set(["issue_created", "running", "skipped", "completed", "failed"]);

// The server stores a run_only run as "running" as soon as its task is
// enqueued. Until that task is claimed — it may wait behind the agent's
// concurrency cap — the run is shown as "queued", derived from the task's own
// status. Unknown statuses from a newer server fall back to issue_created.
export function runDisplayStatus(run: Pick<AutopilotRun, "status" | "task_status">): RunStatus {
  if (run.status === "running" && run.task_status === "queued") return "queued";
  return KNOWN.has(run.status) ? (run.status as RunStatus) : "issue_created";
}
