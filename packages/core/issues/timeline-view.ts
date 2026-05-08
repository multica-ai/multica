import type { TimelineEntry } from "../types";

/**
 * One renderable unit in the activity feed. Either a comment thread (parent +
 * its recursively flattened descendants) or a run of one-or-more activities
 * that have already been coalesced.
 */
export type TimelineGroup =
  | {
      kind: "comment";
      parent: TimelineEntry;
      /** All descendants of `parent` in DFS preorder, regardless of depth. */
      replies: TimelineEntry[];
    }
  | { kind: "activities"; entries: TimelineEntry[] };

/** Coalesce window for consecutive same-actor + same-action activities. */
export const TIMELINE_COALESCE_MS = 2 * 60 * 1000;

/**
 * Action ids whose coalescing ignores the time window. Repeated agent task
 * outcomes can span hours — collapsing them by count regardless of spacing
 * keeps the activity feed legible.
 */
export const TIMELINE_NO_TIME_LIMIT_ACTIONS: ReadonlySet<string> = new Set([
  "task_completed",
  "task_failed",
]);

interface BuildTimelineGroupsResult {
  groups: TimelineGroup[];
  /**
   * Direct (one-level) replies indexed by parent comment id. Useful for
   * consumers that want to walk the reply tree themselves (e.g. web's
   * CommentCard renders nested replies via its own recursive helper).
   * Mobile renderers typically just read `groups[i].replies` which is
   * already recursively flattened.
   */
  repliesByParent: Map<string, TimelineEntry[]>;
}

/**
 * Group a flat (chronological-ascending) timeline into renderable groups.
 *
 * Pipeline:
 *   1. Index every reply (`type === "comment" && parent_id`) under its
 *      direct parent in `repliesByParent`.
 *   2. Filter to top-level entries: every activity, plus comments without a
 *      parent_id.
 *   3. Coalesce consecutive activities by (actor_type, actor_id, action)
 *      within a 2 min window — except `task_completed` / `task_failed`
 *      which coalesce across any time gap. Increments `coalesced_count`.
 *   4. Walk the coalesced top-level. Comments become a `{ kind: "comment" }`
 *      group whose `replies` field is the recursive DFS-preorder flatten of
 *      that comment's entire subtree. Runs of activities collapse into a
 *      single `{ kind: "activities" }` group each.
 *
 * The function is pure and free of React / i18n / DOM. Both web and mobile
 * use it so the activity feed renders identically by construction.
 *
 * @param entries  Chronologically ascending (oldest first). Backend returns
 *                 newest-first, so callers typically `.reverse()` first.
 */
export function buildTimelineGroups(
  entries: TimelineEntry[],
): BuildTimelineGroupsResult {
  const repliesByParent = new Map<string, TimelineEntry[]>();
  for (const e of entries) {
    if (e.type === "comment" && e.parent_id) {
      const list = repliesByParent.get(e.parent_id) ?? [];
      list.push(e);
      repliesByParent.set(e.parent_id, list);
    }
  }

  const topLevel = entries.filter(
    (e) => e.type === "activity" || !e.parent_id,
  );

  const coalesced: TimelineEntry[] = [];
  for (const entry of topLevel) {
    if (entry.type === "activity") {
      const prev = coalesced[coalesced.length - 1];
      if (
        prev?.type === "activity" &&
        prev.action === entry.action &&
        prev.actor_type === entry.actor_type &&
        prev.actor_id === entry.actor_id &&
        (TIMELINE_NO_TIME_LIMIT_ACTIONS.has(entry.action!) ||
          Math.abs(
            new Date(entry.created_at).getTime() -
              new Date(prev.created_at).getTime(),
          ) <= TIMELINE_COALESCE_MS)
      ) {
        coalesced[coalesced.length - 1] = {
          ...entry,
          coalesced_count: (prev.coalesced_count ?? 1) + 1,
        };
        continue;
      }
    }
    coalesced.push(entry);
  }

  const groups: TimelineGroup[] = [];
  for (const entry of coalesced) {
    if (entry.type === "activity") {
      const last = groups[groups.length - 1];
      if (last?.kind === "activities") {
        last.entries.push(entry);
      } else {
        groups.push({ kind: "activities", entries: [entry] });
      }
    } else {
      const replies: TimelineEntry[] = [];
      collectDescendants(entry.id, repliesByParent, replies);
      groups.push({ kind: "comment", parent: entry, replies });
    }
  }

  return { groups, repliesByParent };
}

function collectDescendants(
  parentId: string,
  byParent: Map<string, TimelineEntry[]>,
  out: TimelineEntry[],
): void {
  const children = byParent.get(parentId);
  if (!children) return;
  for (const child of children) {
    out.push(child);
    collectDescendants(child.id, byParent, out);
  }
}
