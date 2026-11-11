import type { AgentTask } from "@multica/core/types/agent";
import type { TimelineEntry } from "@multica/core/types/activity";
import type { GitHubPullRequest } from "@multica/core/types/github";

/** A comment this run posted, as the overview lists it. */
export interface RunComment {
  id: string;
  content: string;
  createdAt: string;
}

/**
 * Comments the run itself posted. The server stamps `source_task_id` on every
 * comment an agent writes from inside a task, so this is a lookup, not a guess
 * from command text.
 */
export function matchRunComments(timeline: readonly TimelineEntry[] | undefined, taskId: string): RunComment[] {
  if (!timeline) return [];
  return timeline
    .filter((entry) => entry.type === "comment" && entry.source_task_id === taskId)
    .map((entry) => ({ id: entry.id, content: entry.content ?? "", createdAt: entry.created_at }));
}

/**
 * Pull requests on the run's issue that this run most plausibly opened.
 *
 * Nothing records which task opened a PR, so this matches on the two facts we
 * do have: a worktree run's delivered branch, and the PR's creation time
 * falling inside the run. The overview words it as "opened during this run",
 * which is exactly what the time match proves.
 */
export function matchRunPullRequests(
  pullRequests: readonly GitHubPullRequest[] | undefined,
  task: Pick<AgentTask, "branch_name" | "started_at" | "dispatched_at" | "completed_at">,
  now: number = Date.now(),
): GitHubPullRequest[] {
  if (!pullRequests || pullRequests.length === 0) return [];
  const start = Date.parse(task.started_at ?? task.dispatched_at ?? "");
  const end = task.completed_at ? Date.parse(task.completed_at) : now;
  return pullRequests.filter((pr) => {
    if (task.branch_name && pr.branch === task.branch_name) return true;
    const created = Date.parse(pr.pr_created_at);
    return Number.isFinite(start) && Number.isFinite(created) && created >= start && created <= end;
  });
}
