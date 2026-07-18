import type { TimelineEntry } from "@multica/core/types";
import { sortTimelineEntriesAsc } from "@multica/core/issues/timeline-sort";

/**
 * Walks the parent_id graph rooted at `rootId` and returns every descendant in
 * CHRONOLOGICAL order (created_at ASC, id tie-break). Shared between
 * CommentCard (which renders the expanded thread) and ResolvedThreadBar
 * (which displays the collapsed count + author list) so the two views stay in
 * sync — direct-children-only counts diverge once nested replies exist (see
 * Emacs review on PR #2300).
 *
 * Chronological, not depth-first: agent replies are forced to nest under the
 * comment that triggered them, so a depth-first walk lets a slow agent's late
 * reply render BEFORE earlier sibling replies (#3691). The server's --thread
 * output the agent reads is already chronological (ListThreadCommentsForIssue
 * in comment.sql); this keeps the UI on the same order.
 */
export function collectThreadReplies(
  rootId: string,
  repliesByParent: Map<string, TimelineEntry[]>,
): TimelineEntry[] {
  const out: TimelineEntry[] = [];
  const walk = (id: string) => {
    const children = repliesByParent.get(id) ?? [];
    for (const child of children) {
      out.push(child);
      walk(child.id);
    }
  };
  walk(rootId);
  return sortTimelineEntriesAsc(out);
}

/**
 * A thread's resolution, derived purely from `resolved_at`. Two user actions
 * write the same field:
 *   - "Resolve thread" sets resolved_at on the ROOT → whole thread folds.
 *   - "Resolve thread with comment" sets resolved_at on a REPLY → that reply is
 *     the resolution; the others fold around it.
 *
 * The derivation is total so the UI never shows two resolutions and never
 * crashes on any combination (older / concurrent writes can resolve more than
 * one): root wins; otherwise the reply with the latest resolved_at is THE
 * resolution. No write-side "clear the others" is needed — display picks one.
 */
export type ThreadResolution =
  | { kind: "none" }
  | { kind: "root" }
  | { kind: "reply"; resolutionId: string };

export function deriveThreadResolution(
  root: TimelineEntry,
  replies: TimelineEntry[],
): ThreadResolution {
  if (root.resolved_at) return { kind: "root" };
  let chosen: TimelineEntry | null = null;
  for (const reply of replies) {
    if (!reply.resolved_at) continue;
    if (!chosen || reply.resolved_at > chosen.resolved_at!) chosen = reply;
  }
  return chosen ? { kind: "reply", resolutionId: chosen.id } : { kind: "none" };
}

/**
 * IDs of every thread root (top-level comment) in a timeline — the units the
 * per-comment collapse store folds. Same root/reply split as issue-detail's
 * `timelineView` grouping: a comment is a root iff it has no `parent_id`.
 */
export function rootCommentIds(entries: readonly TimelineEntry[]): string[] {
  return entries
    .filter((e) => e.type === "comment" && !e.parent_id)
    .map((e) => e.id);
}

/**
 * IDs of thread roots that carry a resolution (on the root itself or on a
 * reply) — the threads that render folded behind a bar until expanded via
 * `useResolvedExpandStore`. Unresolved roots are excluded on purpose: seeding
 * them into the expand set would keep them expanded through a later resolve.
 */
export function resolvedThreadRootIds(entries: readonly TimelineEntry[]): string[] {
  const roots: TimelineEntry[] = [];
  const repliesByParent = new Map<string, TimelineEntry[]>();
  for (const e of entries) {
    if (e.type !== "comment") continue;
    if (!e.parent_id) {
      roots.push(e);
    } else {
      const list = repliesByParent.get(e.parent_id) ?? [];
      list.push(e);
      repliesByParent.set(e.parent_id, list);
    }
  }
  return roots
    .filter(
      (root) =>
        deriveThreadResolution(root, collectThreadReplies(root.id, repliesByParent)).kind !==
        "none",
    )
    .map((root) => root.id);
}
