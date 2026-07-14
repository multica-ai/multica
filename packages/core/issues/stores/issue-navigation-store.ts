"use client";

import { create } from "zustand";

/**
 * Ephemeral record of the list the user is currently navigating, used to power
 * previous/next navigation from inside the issue detail view.
 *
 * The detail page is a standalone route that only knows its own issue id — it
 * has no idea which board column or list row the user clicked to get there. So
 * the board and list views publish their displayed columns here as they render,
 * and the detail reads them back to offer "previous / next issue in this
 * column" without making the user back out to the list first.
 *
 * Keyed by workspace id so an open detail in one workspace isn't disturbed by a
 * list rendering in another — desktop keeps background tabs mounted, and each
 * tab is scoped to its own workspace. Within a single workspace it is
 * last-writer-wins: whichever board/list rendered most recently is the surface
 * the user is looking at, which is the one we want previous/next to follow.
 *
 * Each entry maps a column id (a status or assignee group) to the issue ids in
 * that column, ordered exactly as shown. It is deliberately NOT persisted: it
 * mirrors the last list you looked at this session, and a snapshot restored
 * across reloads would point at issues you can no longer see. Views with no
 * meaningful column ordering (swimlane, gantt) clear their workspace's entry so
 * the detail never offers navigation that wouldn't match what the user sees.
 */
interface IssueNavigationState {
  byWorkspace: Record<string, Record<string, string[]>>;
  setColumns: (wsId: string, columns: Record<string, string[]>) => void;
  clear: (wsId: string) => void;
}

export const useIssueNavigationStore = create<IssueNavigationState>()((set) => ({
  byWorkspace: {},
  setColumns: (wsId, columns) =>
    set((state) => ({
      byWorkspace: { ...state.byWorkspace, [wsId]: columns },
    })),
  clear: (wsId) =>
    set((state) => {
      // Keep the reference stable when there's nothing to clear so subscribers
      // (the open detail) don't re-render needlessly.
      if (!state.byWorkspace[wsId]) return state;
      const next = { ...state.byWorkspace };
      delete next[wsId];
      return { byWorkspace: next };
    }),
}));

export interface IssueSiblings {
  /** True when the issue was found in the last-viewed list, so previous/next
   *  navigation is meaningful. The detail hides the buttons when this is false
   *  (e.g. the user deep-linked straight to the issue). */
  hasContext: boolean;
  /** Issue above the current one in its column, or null at the top. */
  prevId: string | null;
  /** Issue below the current one in its column, or null at the bottom. */
  nextId: string | null;
}

const NO_SIBLINGS: IssueSiblings = { hasContext: false, prevId: null, nextId: null };

/**
 * Locate `issueId` within the published columns and return its immediate
 * neighbours. Pure — the React glue lives in `@multica/views`. The first column
 * that contains the issue wins; an issue never legitimately sits in two columns
 * of the same list, so order of iteration doesn't matter. A missing snapshot
 * (`undefined`, e.g. nothing published for this workspace yet) yields no
 * context.
 */
export function getIssueSiblings(
  columns: Record<string, string[]> | undefined,
  issueId: string,
): IssueSiblings {
  if (!columns) return NO_SIBLINGS;
  for (const ids of Object.values(columns)) {
    const index = ids.indexOf(issueId);
    if (index === -1) continue;
    return {
      hasContext: true,
      prevId: index > 0 ? ids[index - 1]! : null,
      nextId: index < ids.length - 1 ? ids[index + 1]! : null,
    };
  }
  return NO_SIBLINGS;
}
