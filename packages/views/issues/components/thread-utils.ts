import type { TimelineEntry } from "@multica/core/types";

/**
 * Walks the parent_id graph rooted at `rootId` and returns every descendant in
 * traversal order. CommentCard uses this to render the expanded thread while
 * keeping nested reply counts stable — direct-children-only counts diverge
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
