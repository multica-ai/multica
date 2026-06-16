/**
 * In-memory LRU of recently viewed issue UUIDs, scoped per workspace.
 * Powers the chat composer's `@` suggestion bar — the "Recent" section
 * shows whatever the user opened in the issue detail view most recently.
 *
 * Why in-memory only (no SecureStore persist):
 *   - Recency is a session signal; cold-start clearing is fine UX
 *   - Avoids the cost of awaiting SecureStore on every issue open
 *   - Persistence can be a follow-up if users miss it
 *
 * Why per-workspace:
 *   - Viewing MUL-1 in workspace A shouldn't surface it in workspace B's
 *     chat — different agents, different context
 *
 * Capacity 10 per workspace: enough for "the last few things I looked
 * at" without overwhelming the suggestion list. We render at most 5 in
 * the chat suggestion bar; the extras buffer against the user opening a
 * couple of issues they don't end up referencing.
 */
import { create } from "zustand";

const CAPACITY = 10;

interface State {
  byWorkspace: Record<string, string[]>;
  /** Promote `id` to the head of this workspace's list (most-recent
   *  first). De-dupes the prior occurrence. */
  push: (wsId: string, id: string) => void;
  /** Remove an id from a workspace's list — call when issue detail
   *  fetch returns 404, so a deleted issue stops appearing in `@`. */
  remove: (wsId: string, id: string) => void;
}

export const useViewedIssuesStore = create<State>((set) => ({
  byWorkspace: {},
  push: (wsId, id) =>
    set((s) => {
      const prev = s.byWorkspace[wsId] ?? [];
      const next = [id, ...prev.filter((x) => x !== id)].slice(0, CAPACITY);
      return { byWorkspace: { ...s.byWorkspace, [wsId]: next } };
    }),
  remove: (wsId, id) =>
    set((s) => {
      const prev = s.byWorkspace[wsId];
      if (!prev) return s;
      return {
        byWorkspace: {
          ...s.byWorkspace,
          [wsId]: prev.filter((x) => x !== id),
        },
      };
    }),
}));

/** Stable empty array — Zustand selectors that return a fresh `[]` each
 *  call trigger an infinite re-render loop in useSyncExternalStore
 *  (see CLAUDE.md "Common Zustand footguns"). All "no entry" paths
 *  share this single frozen reference. */
const EMPTY_IDS: readonly string[] = Object.freeze([]);

/** Selector — current workspace's viewed-issue ids in most-recent-first
 *  order. Stable reference: returns the underlying array when present,
 *  the shared EMPTY_IDS otherwise. */
export function selectViewedIssueIds(wsId: string | null) {
  return (s: State): readonly string[] =>
    wsId ? s.byWorkspace[wsId] ?? EMPTY_IDS : EMPTY_IDS;
}
