import { create } from "zustand";

/**
 * Whether the sub-issues section of an issue detail page is collapsed, keyed
 * by issue ID. Only collapsed ids are stored — expanded is the default.
 *
 * Deliberately NOT persisted (same contract as resolved-expand-store): the
 * state lives in a store rather than issue-detail component state so leaving
 * an issue and navigating back finds the section as it was left (MUL-4741
 * state restoration on back), while a reload returns to the default.
 */
interface SubIssuesCollapseStore {
  collapsedIssueIds: ReadonlySet<string>;
  setCollapsed: (issueId: string, collapsed: boolean) => void;
}

export const useSubIssuesCollapseStore = create<SubIssuesCollapseStore>()(
  (set) => ({
    collapsedIssueIds: new Set<string>(),
    setCollapsed: (issueId, collapsed) =>
      set((s) => {
        if (s.collapsedIssueIds.has(issueId) === collapsed) return s;
        const next = new Set(s.collapsedIssueIds);
        if (collapsed) next.add(issueId);
        else next.delete(issueId);
        return { collapsedIssueIds: next };
      }),
  }),
);
