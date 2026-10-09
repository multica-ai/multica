import type { AgentTask, TimelineEntry } from "@multica/core/types";
import { isDeletedComment } from "@multica/core/issues/comment-deletion";
import { commentSupplementReceipts } from "@multica/core/issues/run-steering";

export interface CommentRun {
  task: AgentTask;
  commentId?: string;
  /** Last input this run covers; absent for issue-level runs such as assignment. */
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

/**
 * The comment the platform posts for a run that ended in failure: the raw
 * error, authored as the agent. It restates how the run ended, so the run
 * block renders in its place instead of a second, differently worded copy
 * (MUL-7692).
 */
export function isRunFailureNotice(entry: TimelineEntry, task: AgentTask): boolean {
  return entry.actor_type === "agent" && entry.comment_type === "system"
    && entry.source_task_id === task.id
    && (task.status === "failed" || task.status === "cancelled");
}

/** Published replies own their log entry even while the agent finishes its run. */
export function showCommentRunInHeader(run: CommentRun): boolean {
  return run.hasReply && (isActiveCommentRun(run.task) || run.task.status === "completed");
}

/** Invalidated queued input is history, not an agent response to the new text. */
function isObsoleteCommentRun(task: AgentTask): boolean {
  return task.status === "cancelled" && task.cancelled_by_comment_change === true
    && !task.dispatched_at && !task.started_at && !task.delivered_comment_ids?.length;
}

/** Anchor each run on its latest covered input; associate replies by task identity. */
export function buildCommentRunView(
  tasks: readonly AgentTask[],
  timeline: readonly TimelineEntry[],
  previous = new Map<string, CommentRun[]>(),
): { timeline: readonly TimelineEntry[]; runs: Map<string, CommentRun[]>; standaloneRuns: CommentRun[] } {
  // Quick create owns the issue's creation, not a turn inside the issue. The
  // backend links that task to the issue after success so it remains available
  // in Execution history, but it must not become an unanchored Activity block.
  const inlineTasks = tasks.filter((task) => task.kind !== "quick_create");
  const comments = new Map(timeline.filter((entry) => entry.type === "comment").map((entry) => [entry.id, entry]));
  const supplementalByTask = new Map<string, string[]>();
  for (const entry of comments.values()) {
    // One message can steer several turns; it is input to each of them.
    for (const receipt of commentSupplementReceipts(entry)) {
      const ids = supplementalByTask.get(receipt.task_id) ?? [];
      ids.push(entry.id);
      supplementalByTask.set(receipt.task_id, ids);
    }
  }
  const timelineOrder = new Map(timeline.map((entry, index) => [entry.id, index]));
  const threadRoot = (id: string): string | undefined => {
    const seen = new Set<string>();
    let entry = comments.get(id);
    while (entry?.parent_id) {
      if (seen.has(entry.id)) return undefined;
      seen.add(entry.id);
      entry = comments.get(entry.parent_id);
    }
    return entry?.id;
  };
  const replies = new Map<string, TimelineEntry>();
  for (const entry of comments.values()) {
    if (!entry.source_task_id || entry.actor_type !== "agent") continue;
    const prior = replies.get(entry.source_task_id);
    if (!prior || entry.created_at > prior.created_at || (entry.created_at === prior.created_at && entry.id > prior.id)) {
      replies.set(entry.source_task_id, entry);
    }
  }
  const topLevelOutputs = new Map<string, TimelineEntry[]>();
  for (const entry of comments.values()) {
    if (!entry.source_task_id || entry.actor_type !== "agent" || entry.parent_id) continue;
    const outputs = topLevelOutputs.get(entry.source_task_id) ?? [];
    outputs.push(entry);
    topLevelOutputs.set(entry.source_task_id, outputs);
  }
  const byTask = new Map(inlineTasks.map((task) => [task.id, task]));
  const priorAnchors = new Map([...previous.values()].flatMap((runs) => runs.map((run) => [run.task.id, run.anchorCommentId] as const)));
  const placements: CommentRun[] = [];
  for (const task of [...inlineTasks].sort((a, b) => a.created_at.localeCompare(b.created_at) || a.id.localeCompare(b.id))) {
    const reply = replies.get(task.id);
    if (isObsoleteCommentRun(task) && !reply) continue;
    let anchorId: string | undefined;
    let source: AgentTask | undefined = task;
    const visited = new Set<string>();
    while (!anchorId && source && !visited.has(source.id)) {
      visited.add(source.id);
      // Before claim, the receipt is empty (or belongs to a previous claim).
      // Dispatch can precede receipt persistence; retain the planned anchor
      // until delivery is known, or if the run terminates before dispatch.
      const usesPlannedCoverage = source.status === "queued"
        || (source.status === "dispatched" && !source.delivered_comment_ids?.length)
        || ((source.status === "cancelled" || source.status === "failed")
          && !source.dispatched_at && !source.started_at);
      const baseIds = !usesPlannedCoverage && source.delivered_comment_ids !== undefined
        ? source.delivered_comment_ids
        : [source.trigger_comment_id, ...(source.coalesced_comment_ids ?? [])];
      const ids = [...new Set([
        ...baseIds,
        ...(source.supplement_comment_ids ?? []),
        ...(supplementalByTask.get(source.id) ?? []),
      ])];
      const candidates = ids.flatMap((id) => id && comments.has(id) ? [comments.get(id)!] : []);
      const latestCandidateId = () => [...candidates].sort((a, b) => b.created_at.localeCompare(a.created_at)
        || (timelineOrder.get(b.id) ?? -1) - (timelineOrder.get(a.id) ?? -1))[0]?.id;
      // Task events can arrive before their comments. Preserve the intended
      // anchor even when it cannot be rendered yet; it is not an assignment.
      anchorId = source.trigger_comment_id && ids.includes(source.trigger_comment_id)
        ? source.trigger_comment_id
        : latestCandidateId() ?? ids.find((id) => !!id);
      // A same-thread batch answers its latest covered input: the comment its
      // reply hangs under and quotes when the two are apart (MUL-7628). Before
      // claim, a newly coalesced trigger moves the anchor down. After claim,
      // `ids` comes from delivered_comment_ids and freezes the anchor at the
      // actual delivery boundary. Wait for missing comments instead of guessing
      // their thread; historical batches spanning several threads retain their
      // trigger.
      const root = candidates[0] && threadRoot(candidates[0].id);
      if (root && candidates.length === ids.filter(Boolean).length
        && candidates.every((entry) => threadRoot(entry.id) === root)) {
        anchorId = latestCandidateId();
      }
      const priorAnchor = priorAnchors.get(source.id);
      if (priorAnchor && ids.includes(priorAnchor) && candidates.length < ids.filter(Boolean).length) {
        // Realtime task metadata can arrive before a new trigger comment; keep
        // the existing visible slot in that window. If the prior slot itself
        // disappeared, the comment was deleted, so fall back to the latest
        // remaining covered input instead of hiding the run and its reply.
        anchorId = comments.has(priorAnchor) ? priorAnchor : latestCandidateId() ?? priorAnchor;
      }
      source = source.parent_task_id ? byTask.get(source.parent_task_id) : undefined;
    }
    placements.push({ task, commentId: reply?.id ?? anchorId, anchorCommentId: anchorId, hasReply: !!reply });
  }
  // Project every task-owned answer first, then use that same tree for run
  // grouping, replies, resolution, and navigation. Assignment answers become
  // roots even if the agent originally posted them inside an existing thread.
  // The run's other top-level comments follow its reply: moving only the
  // latest one would render it above the progress posted before it (MUL-7548).
  // Comments the agent placed in a thread stay where it put them.
  const parents = new Map<string, string | undefined>();
  for (const run of placements) {
    if (run.hasReply && run.commentId && run.commentId !== run.anchorCommentId) {
      for (const output of topLevelOutputs.get(run.task.id) ?? []) parents.set(output.id, run.anchorCommentId);
      parents.set(run.commentId, run.anchorCommentId);
    }
  }
  const cyclic = new Set<string>();
  for (const id of parents.keys()) {
    const seen = new Set([id]);
    let parent = parents.get(id);
    while (parent) {
      if (seen.has(parent)) { cyclic.add(id); break; }
      seen.add(parent);
      parent = parents.has(parent) ? parents.get(parent) : comments.get(parent)?.parent_id ?? undefined;
    }
  }
  for (const id of cyclic) parents.delete(id);
  const projected = timeline.map((entry) => {
    const parent = parents.get(entry.id);
    return parents.has(entry.id) && (entry.parent_id ?? undefined) !== parent
      ? { ...entry, parent_id: parent } : entry;
  });
  const projectedComments = new Map(projected.filter((entry) => entry.type === "comment").map((entry) => [entry.id, entry]));
  const grouped = new Map<string, CommentRun[]>();
  for (const run of placements) {
    let root = projectedComments.get(run.anchorCommentId ?? run.commentId ?? "");
    if (!root) continue;
    const ancestors = new Set([root.id]);
    while (root.parent_id && projectedComments.has(root.parent_id) && !ancestors.has(root.parent_id)) {
      root = projectedComments.get(root.parent_id)!;
      ancestors.add(root.id);
    }
    const rows = grouped.get(root.id) ?? [];
    rows.push(run);
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
  // Missing comment data must not turn a thread-owned run into a root block.
  return {
    timeline: projected,
    runs: grouped,
    standaloneRuns: placements.filter((run) => !run.anchorCommentId),
  };
}

/**
 * Where a run block sits among comments — in the top-level timeline, or among
 * one thread's replies.
 *
 * Once a run publishes a reply, the reply's own time owns the slot (MUL-7211).
 * This keeps the card's timestamp in chronological order even after a long
 * queue wait or execution, and in a thread it lets two agents asked in a row
 * read in the order they replied (MUL-7628). A run that ended without a reply
 * sorts when it ended.
 *
 * Until then, an active run keeps its enqueue-time slot (MUL-7632). Parking it
 * at the live end would insert every later comment above an already visible
 * assignment block, including a mention that starts another agent's run.
 */
function publishedReply(run: CommentRun, entryById: ReadonlyMap<string, TimelineEntry>): TimelineEntry | undefined {
  return run.hasReply && run.commentId ? entryById.get(run.commentId) : undefined;
}

function runSortTime(run: CommentRun, entryById: ReadonlyMap<string, TimelineEntry>): number {
  const reply = publishedReply(run, entryById);
  if (reply) return Date.parse(reply.created_at);
  if (isActiveCommentRun(run.task)) return Date.parse(run.task.created_at);
  return Date.parse(run.task.completed_at ?? run.task.created_at);
}

/**
 * Merge runs into a list of comments in reading order: standalone runs into
 * the top-level timeline, or a thread's runs into its replies.
 *
 * A run that published a reply REPLACES that comment's own slot — the block
 * renders the comment, so leaving both would double it.
 */
export function orderTimelineWithRuns(
  entries: readonly TimelineEntry[],
  runs: readonly CommentRun[],
  entryById: ReadonlyMap<string, TimelineEntry>,
): (TimelineEntry | CommentRun)[] {
  const slotted = new Set(runs.filter((run) => run.hasReply).map((run) => run.commentId));
  const sortTime = (item: TimelineEntry | CommentRun): number =>
    "task" in item ? runSortTime(item, entryById) : Date.parse(item.created_at);
  // Ties are ordinary, not exotic: the API serializes timestamps to whole
  // seconds (util.TimestampToString), so a reply and the comment next to it
  // routinely share one. Break them on the row that OWNS the slot — the reply
  // for a run that published one, never the task behind it — so equal
  // timestamps keep the server's `created_at ASC, id ASC` order, the same
  // invariant sortTimelineEntriesAsc holds for the flat cache.
  const slotRow = (item: TimelineEntry | CommentRun): { created_at: string; id: string } =>
    "task" in item ? publishedReply(item, entryById) ?? item.task : item;
  return [...entries.filter((entry) => !slotted.has(entry.id)), ...runs].sort((a, b) => {
    const left = sortTime(a), right = sortTime(b);
    if (left !== right) return left < right ? -1 : 1;
    const leftRow = slotRow(a), rightRow = slotRow(b);
    return leftRow.created_at.localeCompare(rightRow.created_at) || leftRow.id.localeCompare(rightRow.id);
  });
}

/** A run's slot in a thread. */
export interface ThreadRunSlot {
  run: CommentRun;
  /** The run's latest comment, which the slot renders once it exists. */
  reply?: TimelineEntry;
  /** The input the run answers, when other rows separate the two. */
  replyTo?: TimelineEntry;
}

/**
 * A thread's replies and runs in reading order. Every posted comment sits at
 * its own time, a run's included (MUL-7211): two agents asked in a row read
 * in the order they answered, and a run's progress reads before a comment
 * written after it (MUL-7628). A run's slot is its latest comment. A run
 * still working with nothing posted stays right after the input it answers
 * — the latest one it covers — the way an active standalone run keeps its
 * enqueue slot (MUL-7632). A run whose reply is the root heads the thread
 * instead.
 */
export function orderThreadWithRuns(
  root: TimelineEntry,
  replies: readonly TimelineEntry[],
  runs: readonly CommentRun[],
): (TimelineEntry | ThreadRunSlot)[] {
  const entryById = new Map([root, ...replies].map((entry) => [entry.id, entry]));
  const threadRuns = runs.filter((run) => run.anchorCommentId && !(run.hasReply && run.commentId === root.id));
  const working = new Set(threadRuns.filter((run) => !run.hasReply && isActiveCommentRun(run.task)));
  const rows = orderTimelineWithRuns(replies, threadRuns.filter((run) => !working.has(run)), entryById);
  // `runs` arrive in enqueue order: each working run goes after the row that
  // shows its input (none for the root), after earlier runs on that input,
  // and after anything that happened before it was enqueued. A retry answers
  // the same input as the attempt it retries but starts after that attempt
  // ended, so it reads below it (MUL-7692).
  for (const run of working) {
    const enqueuedAt = Date.parse(run.task.created_at);
    let index = rows.findIndex((item) => ("task" in item ? publishedReply(item, entryById)?.id : item.id) === run.anchorCommentId) + 1;
    while (index < rows.length) {
      const row = rows[index]!;
      const earlierRun = working.has(row as CommentRun) && (row as CommentRun).anchorCommentId === run.anchorCommentId;
      const earlierRow = !working.has(row as CommentRun)
        && ("task" in row ? runSortTime(row, entryById) : Date.parse(row.created_at)) <= enqueuedAt;
      if (!earlierRun && !earlierRow) break;
      index += 1;
    }
    rows.splice(index, 0, run);
  }
  // The comment directly above a run, past the run's own earlier comments. A
  // tombstone renders nothing; a run without a visible reply renders its
  // activity block, which no input can be.
  const commentAbove = (index: number, run: CommentRun): string | undefined => {
    for (const item of rows.slice(0, index).reverse()) {
      if ("task" in item) {
        const reply = publishedReply(item, entryById);
        return reply && !isDeletedComment(reply) ? reply.id : undefined;
      }
      if (!isDeletedComment(item) && item.source_task_id !== run.task.id) return item.id;
    }
    return root.id;
  };
  return rows.map((item, index) => {
    if (!("task" in item)) return item;
    const reply = publishedReply(item, entryById);
    const anchor = item.anchorCommentId ? entryById.get(item.anchorCommentId) : undefined;
    const replyTo = anchor && anchor.id !== reply?.id && !isDeletedComment(anchor)
      && anchor.id !== commentAbove(index, item) ? anchor : undefined;
    return { run: item, reply, replyTo };
  });
}
