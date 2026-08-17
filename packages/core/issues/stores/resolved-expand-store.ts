import { create } from "zustand";

/**
 * Tracks which resolved threads are temporarily expanded, keyed by issue ID.
 * Only expanded thread-root IDs are stored — folded-to-a-bar is the default.
 *
 * Deliberately NOT persisted (matches Linear): a reload folds every resolved
 * thread back to its summary bar. It lives in a store rather than issue-detail
 * component state so the command palette's fold/unfold-all-comments commands
 * can drive it from outside the issue page.
 */
interface ResolvedExpandStore {
  expandedByIssue: Record<string, ReadonlySet<string>>;
  setExpanded: (issueId: string, commentId: string, expand: boolean) => void;
  /** Expand every thread root in `commentIds` at once (unfold-all). */
  expandAll: (issueId: string, commentIds: readonly string[]) => void;
  /** Fold every expanded thread back to its bar (fold-all). */
  collapseAll: (issueId: string) => void;
}

const EMPTY_EXPANDED: ReadonlySet<string> = new Set();

function withoutIssue(
  expandedByIssue: Record<string, ReadonlySet<string>>,
  issueId: string,
) {
  const { [issueId]: _, ...rest } = expandedByIssue;
  return rest;
}

export const useResolvedExpandStore = create<ResolvedExpandStore>()((set) => ({
  expandedByIssue: {},
  setExpanded: (issueId, commentId, expand) =>
    set((s) => {
      const current = s.expandedByIssue[issueId] ?? EMPTY_EXPANDED;
      if (current.has(commentId) === expand) return s;
      const next = new Set(current);
      if (expand) next.add(commentId);
      else next.delete(commentId);
      if (next.size === 0) {
        return { expandedByIssue: withoutIssue(s.expandedByIssue, issueId) };
      }
      return { expandedByIssue: { ...s.expandedByIssue, [issueId]: next } };
    }),
  expandAll: (issueId, commentIds) =>
    set((s) => {
      if (commentIds.length === 0) return s;
      const next = new Set(s.expandedByIssue[issueId] ?? EMPTY_EXPANDED);
      for (const id of commentIds) next.add(id);
      return { expandedByIssue: { ...s.expandedByIssue, [issueId]: next } };
    }),
  collapseAll: (issueId) =>
    set((s) => {
      if (!(issueId in s.expandedByIssue)) return s;
      return { expandedByIssue: withoutIssue(s.expandedByIssue, issueId) };
    }),
}));

/**
 * Stable-reference selector for one issue's expanded set (falls back to a
 * shared frozen empty set so unrelated store writes don't re-render readers).
 */
export function selectExpandedResolved(issueId: string) {
  return (s: ResolvedExpandStore): ReadonlySet<string> =>
    s.expandedByIssue[issueId] ?? EMPTY_EXPANDED;
}
