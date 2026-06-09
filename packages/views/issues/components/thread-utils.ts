import type { TimelineEntry } from "@multica/core/types";

/**
 * Walks the parent_id graph rooted at `rootId` and returns every descendant in
 * traversal order. Shared between CommentCard (which renders the expanded
 * thread) and ResolvedThreadBar (which displays the collapsed count + author
 * list) so the two views stay in sync — direct-children-only counts diverge
 * once nested replies exist (see Emacs review on PR #2300).
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
  return out;
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
