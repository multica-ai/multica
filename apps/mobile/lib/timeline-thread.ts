/**
 * Group timeline entries by reply thread for the mobile flat list.
 *
 * Mirrors the partition step of web's
 * `packages/views/issues/components/issue-detail.tsx:301-339`, but the output
 * shape differs: instead of emitting each reply as its own row (web renders a
 * recursive tree with indentation), we **bundle a comment's entire descendant
 * chain into the parent row** so the renderer can draw one bubble that
 * contains the whole thread. This matches the visual rule the user asked for
 * — no "Replying to" headers, the bubble boundary itself indicates the
 * thread.
 *
 * Two important rules carried over from web:
 *
 *   - **Orphan rescue (web #1857):** a reply whose parent is NOT in the
 *     loaded timeline gets promoted to top-level instead of disappearing.
 *     Without this the entire reply subtree would silently vanish and break
 *     the "Counts must agree" parity rule from apps/mobile/CLAUDE.md.
 *   - **Reply-to-reply still belongs to the same bundle.** A nested chain
 *     (A → B → C) gets flattened into A's row with `replies: [B, C]`. Mobile
 *     is a flat list (CLAUDE.md), so we don't preserve depth — just keep
 *     them all inside the same bubble in chronological order.
 *
 * Total comment+activity count emitted is identical to the input (just
 * fewer rows because replies are folded into parents). That preserves the
 * "Counts must agree" parity rule against web.
 */
import type { TimelineEntry } from "@multica/core/types";

export interface TimelineRow {
  entry: TimelineEntry;
  /** Flattened descendant chain in BFS / chronological order. Empty for
   *  activity rows and for top-level comments without replies. */
  replies: TimelineEntry[];
}

export function buildTimelineRows(
  entries: TimelineEntry[],
): TimelineRow[] {
  const commentIds = new Set<string>();
  for (const e of entries) {
    if (e.type === "comment") commentIds.add(e.id);
  }

  const topLevel: TimelineEntry[] = [];
  const childrenByParent = new Map<string, TimelineEntry[]>();

  for (const e of entries) {
    if (
      e.type === "comment" &&
      e.parent_id &&
      commentIds.has(e.parent_id)
    ) {
      const list = childrenByParent.get(e.parent_id) ?? [];
      list.push(e);
      childrenByParent.set(e.parent_id, list);
    } else {
      // Activity OR top-level comment OR orphan reply (parent not in batch).
      topLevel.push(e);
    }
  }

  function collectDescendants(parentId: string): TimelineEntry[] {
    // BFS — children are inserted in chronological order during the scan
    // above, so the bundle preserves "first reply first" without extra
    // sorting. Reply-to-reply gets appended after all top-level replies of
    // the same parent.
    const out: TimelineEntry[] = [];
    const queue: string[] = [parentId];
    while (queue.length > 0) {
      const pid = queue.shift()!;
      const kids = childrenByParent.get(pid);
      if (!kids) continue;
      for (const child of kids) {
        out.push(child);
        queue.push(child.id);
      }
    }
    return out;
  }

  return topLevel.map((entry) => ({
    entry,
    replies:
      entry.type === "comment" ? collectDescendants(entry.id) : [],
  }));
}
