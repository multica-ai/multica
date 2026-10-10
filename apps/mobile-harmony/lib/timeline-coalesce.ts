/**
 * Coalesces consecutive identical activity entries. The exact rule is mirrored
 * from packages/views/issues/components/issue-detail.tsx:841-866 — this is a
 * behavioral parity gate: mobile must show the same N timeline entries as
 * web/desktop after coalescing (apps/mobile/CLAUDE.md "Counts and visibility
 * must agree").
 *
 * Rule (ASC chronological input):
 *   - Walk the array in order. If the next entry is an activity with
 *     identical (action, actor_type, actor_id) as the previous coalesced
 *     entry, AND either
 *       (a) the action is `task_completed` / `task_failed` (no time limit), or
 *       (b) the gap is ≤ 2 minutes,
 *     merge it: bump `coalesced_count` and replace the previous entry's body
 *     with the newer one (preserves the newest timestamp/actor).
 *   - `squad_leader_evaluated` NEVER coalesces — each entry carries unique
 *     audit data (outcome + reason) and merging would drop the second
 *     evaluation's context. Matches web.
 *   - Comments never coalesce (each is its own entry).
 *
 * Returns a new array; the input is not mutated.
 */
import type { TimelineEntry } from "@multica/core/types";

const COALESCE_MS = 2 * 60 * 1000;
const NO_TIME_LIMIT_ACTIONS = new Set(["task_completed", "task_failed"]);
const NEVER_COALESCE_ACTIONS = new Set(["squad_leader_evaluated"]);

export function coalesceTimeline(
  entries: TimelineEntry[],
): TimelineEntry[] {
  const out: TimelineEntry[] = [];
  for (const entry of entries) {
    if (entry.type === "activity") {
      const prev = out[out.length - 1];
      if (
        !NEVER_COALESCE_ACTIONS.has(entry.action ?? "") &&
        prev?.type === "activity" &&
        prev.action === entry.action &&
        prev.actor_type === entry.actor_type &&
        prev.actor_id === entry.actor_id &&
        (NO_TIME_LIMIT_ACTIONS.has(entry.action ?? "") ||
          Math.abs(
            new Date(entry.created_at).getTime() -
              new Date(prev.created_at).getTime(),
          ) <= COALESCE_MS)
      ) {
        out[out.length - 1] = {
          ...entry,
          coalesced_count: (prev.coalesced_count ?? 1) + 1,
        };
        continue;
      }
    }
    out.push(entry);
  }
  return out;
}
