import { create } from "zustand";

/**
 * Tracks which long comments the reader has expanded, keyed by issue ID.
 * Only expanded comment IDs are stored — showing a long comment's opening is
 * the default.
 *
 * A store rather than component state because the issue timeline is
 * virtualized: a comment scrolled out and back in remounts, and issue-detail
 * also reads the set so in-page find and row height estimates match what is
 * on screen. Not persisted: a reload collapses long comments again, as it
 * folds resolved threads.
 */
interface LongCommentExpandStore {
  expandedByIssue: Record<string, ReadonlySet<string>>;
  setExpanded: (issueId: string, commentId: string, expand: boolean) => void;
}

const EMPTY_EXPANDED: ReadonlySet<string> = new Set();

export const useLongCommentExpandStore = create<LongCommentExpandStore>()((set) => ({
  expandedByIssue: {},
  setExpanded: (issueId, commentId, expand) =>
    set((s) => {
      const current = s.expandedByIssue[issueId] ?? EMPTY_EXPANDED;
      if (current.has(commentId) === expand) return s;
      const next = new Set(current);
      if (expand) next.add(commentId);
      else next.delete(commentId);
      if (next.size === 0) {
        const { [issueId]: _, ...rest } = s.expandedByIssue;
        return { expandedByIssue: rest };
      }
      return { expandedByIssue: { ...s.expandedByIssue, [issueId]: next } };
    }),
}));

/** Stable-reference selector for one issue's expanded long comments. */
export function selectExpandedLongComments(issueId: string) {
  return (s: LongCommentExpandStore): ReadonlySet<string> =>
    s.expandedByIssue[issueId] ?? EMPTY_EXPANDED;
}
