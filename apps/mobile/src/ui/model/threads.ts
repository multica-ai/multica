import type { AgentTask, TimelineEntry } from "@multica/core/types";

export type TimelineNode =
  | { kind: "activity"; entry: TimelineEntry }
  | { kind: "thread"; root: TimelineEntry; replies: TimelineEntry[]; run: AgentTask | null; resolved: boolean }
  | { kind: "run"; task: AgentTask };

const ACTIVE = new Set<AgentTask["status"]>(["queued", "dispatched", "waiting_local_directory", "running"]);

/**
 * Runs worth a block: ones still going, and ones that failed. A finished run's answer is the
 * agent's comment, and a cancelled one has nothing to show.
 */
function showsRun(task: AgentTask): boolean {
  return ACTIVE.has(task.status) || task.status === "failed";
}

/**
 * The issue's timeline as the page shows it: activity, and comment threads with their replies in
 * order. A run sits in the thread of the comment that started it; one started some other way, by
 * assignment for one, takes its place in time.
 */
export function buildTimeline(entries: readonly TimelineEntry[], tasks: readonly AgentTask[]): TimelineNode[] {
  const comments = new Map(entries.filter((entry) => entry.type === "comment").map((entry) => [entry.id, entry]));
  const rootOf = (id: string): string | undefined => {
    const seen = new Set<string>();
    let entry = comments.get(id);
    while (entry?.parent_id && comments.has(entry.parent_id) && !seen.has(entry.id)) {
      seen.add(entry.id);
      entry = comments.get(entry.parent_id);
    }
    return entry?.id;
  };

  const replies = new Map<string, TimelineEntry[]>();
  for (const entry of comments.values()) {
    const root = rootOf(entry.id);
    if (!root || root === entry.id) continue;
    const list = replies.get(root) ?? [];
    list.push(entry);
    replies.set(root, list);
  }

  const threadRun = new Map<string, AgentTask>();
  const loose: AgentTask[] = [];
  const byTime = [...tasks].filter((task) => task.kind !== "quick_create").sort((a, b) => a.created_at.localeCompare(b.created_at));
  for (const task of byTime) {
    const anchor = task.trigger_comment_id ? rootOf(task.trigger_comment_id) : undefined;
    if (anchor) threadRun.set(anchor, task);
    else if (showsRun(task)) loose.push(task);
  }

  const nodes: TimelineNode[] = [];
  for (const entry of entries) {
    if (entry.type === "activity") {
      nodes.push({ kind: "activity", entry });
      continue;
    }
    if (rootOf(entry.id) !== entry.id) continue;
    const thread = [...(replies.get(entry.id) ?? [])].sort((a, b) => a.created_at.localeCompare(b.created_at));
    const run = threadRun.get(entry.id) ?? null;
    nodes.push({
      kind: "thread",
      root: entry,
      replies: thread,
      run: run && showsRun(run) ? run : null,
      resolved: [entry, ...thread].some((comment) => Boolean(comment.resolved_at)),
    });
  }

  if (!loose.length) return nodes;
  const merged: TimelineNode[] = [];
  let next = 0;
  for (const node of nodes) {
    const at = node.kind === "thread" ? node.root.created_at : node.kind === "activity" ? node.entry.created_at : node.task.created_at;
    while (next < loose.length && loose[next]!.created_at < at) merged.push({ kind: "run", task: loose[next++]! });
    merged.push(node);
  }
  while (next < loose.length) merged.push({ kind: "run", task: loose[next++]! });
  return merged;
}
