import type { AgentTask, TimelineEntry } from "@multica/core/types";

export interface CommentRun {
  task: AgentTask;
  commentId?: string;
  /** Stable trigger location; absent for issue-level runs such as assignment. */
  anchorCommentId?: string;
  /** The comment already contains this run's reply; only append its activity. */
  hasReply: boolean;
}

export const EMPTY_COMMENT_RUNS: CommentRun[] = [];

/** Use the daemon's deliverable, never guess a final answer from progress text. */
export function commentRunOutput(task: AgentTask): string | null {
  if (task.status !== "completed" || !task.result || typeof task.result !== "object") return null;
  return "comment" in task.result && typeof task.result.comment === "string" && task.result.comment.trim()
    ? task.result.comment : null;
}

export function isActiveCommentRun(task: AgentTask): boolean {
  return ["queued", "dispatched", "waiting_local_directory", "running"].includes(task.status);
}

/** Invalidated queued input is history, not an agent response to the new text. */
function isObsoleteCommentRun(task: AgentTask): boolean {
  return task.status === "cancelled" && task.cancelled_by_comment_change === true
    && !task.dispatched_at && !task.started_at && !task.delivered_comment_ids?.length;
}

/** Keep each run at its trigger; associate replies by task identity, never arrival order. */
export function groupCommentRuns(
  tasks: readonly AgentTask[],
  timeline: readonly TimelineEntry[],
  previous = new Map<string, CommentRun[]>(),
): Map<string, CommentRun[]> {
  const comments = new Map(timeline.filter((entry) => entry.type === "comment").map((entry) => [entry.id, entry]));
  const replies = new Map<string, TimelineEntry>();
  for (const entry of comments.values()) {
    if (!entry.source_task_id || entry.actor_type !== "agent") continue;
    const prior = replies.get(entry.source_task_id);
    if (!prior || entry.created_at > prior.created_at || (entry.created_at === prior.created_at && entry.id > prior.id)) {
      replies.set(entry.source_task_id, entry);
    }
  }
  const byTask = new Map(tasks.map((task) => [task.id, task]));
  const grouped = new Map<string, CommentRun[]>();
  for (const task of [...tasks].sort((a, b) => a.created_at.localeCompare(b.created_at) || a.id.localeCompare(b.id))) {
    const reply = replies.get(task.id);
    if (isObsoleteCommentRun(task) && !reply) continue;
    let anchor: TimelineEntry | undefined;
    let source: AgentTask | undefined = task;
    const visited = new Set<string>();
    while (!anchor && source && !visited.has(source.id)) {
      visited.add(source.id);
      // Before claim, the receipt is empty (or belongs to a previous claim).
      // Keep that planned anchor if the run terminates before dispatch, too.
      const usesPlannedCoverage = source.status === "queued"
        || ((source.status === "cancelled" || source.status === "failed")
          && !source.dispatched_at && !source.started_at);
      const ids = !usesPlannedCoverage && source.delivered_comment_ids !== undefined
        ? source.delivered_comment_ids
        : [source.trigger_comment_id, ...(source.coalesced_comment_ids ?? [])];
      const candidates = ids.flatMap((id) => id && comments.has(id) ? [comments.get(id)!] : []);
      anchor = candidates.find((entry) => entry.id === source?.trigger_comment_id)
        ?? candidates.sort((a, b) => b.created_at.localeCompare(a.created_at))[0];
      source = source.parent_task_id ? byTask.get(source.parent_task_id) : undefined;
    }
    const location = anchor ?? reply;
    if (!location) continue;
    let root = location;
    const ancestors = new Set([root.id]);
    while (root.parent_id && comments.has(root.parent_id) && !ancestors.has(root.parent_id)) {
      root = comments.get(root.parent_id)!;
      ancestors.add(root.id);
    }
    const rows = grouped.get(root.id) ?? [];
    rows.push({ task, commentId: reply?.id ?? anchor?.id, anchorCommentId: anchor?.id, hasReply: !!reply });
    grouped.set(root.id, rows);
  }
  // Preserve memoized comment cards when a different thread receives an event.
  for (const [root, rows] of grouped) {
    const prior = previous.get(root);
    if (prior?.length === rows.length && rows.every((row, i) =>
      row.task === prior[i]!.task && row.commentId === prior[i]!.commentId && row.anchorCommentId === prior[i]!.anchorCommentId && row.hasReply === prior[i]!.hasReply)) {
      grouped.set(root, prior);
    }
  }
  return grouped;
}

/** Runs without a comment anchor still appear as their own agent activity. */
export function standaloneCommentRuns(
  tasks: readonly AgentTask[],
  grouped: ReadonlyMap<string, readonly CommentRun[]>,
): CommentRun[] {
  const known = new Map([...grouped.values()].flatMap((runs) => runs.map((run) => [run.task.id, run] as const)));
  return tasks.filter((task) => (!isObsoleteCommentRun(task) || known.get(task.id)?.hasReply === true)
    && !known.get(task.id)?.anchorCommentId)
    .map((task) => known.get(task.id) ?? { task, hasReply: false });
}
