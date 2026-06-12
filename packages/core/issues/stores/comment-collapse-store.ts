import { create } from "zustand";
import { createJSONStorage, persist } from "zustand/middleware";
import { createWorkspaceAwareStorage, registerForWorkspaceRehydration } from "../../platform/workspace-storage";
import { defaultStorage } from "../../platform/storage";

/**
 * Tracks which comments are collapsed, keyed by issue ID.
 * Only collapsed comment IDs are stored — expanded is the default state.
 */
interface CommentCollapseStore {
  collapsedByIssue: Record<string, string[]>;
  isCollapsed: (issueId: string, commentId: string) => boolean;
  toggle: (issueId: string, commentId: string) => void;
  setIssueCommentsCollapsed: (
    issueId: string,
    commentIds: string[],
    collapsed: boolean,
  ) => void;
}

export const useCommentCollapseStore = create<CommentCollapseStore>()(
  persist(
    (set, get) => ({
      collapsedByIssue: {},
      isCollapsed: (issueId, commentId) => {
        const ids = get().collapsedByIssue[issueId];
        return ids ? ids.includes(commentId) : false;
      },
      toggle: (issueId, commentId) =>
        set((s) => {
          const current = s.collapsedByIssue[issueId] ?? [];
          const isCurrentlyCollapsed = current.includes(commentId);
          if (isCurrentlyCollapsed) {
            const next = current.filter((id) => id !== commentId);
            if (next.length === 0) {
              const { [issueId]: _, ...rest } = s.collapsedByIssue;
              return { collapsedByIssue: rest };
            }
            return { collapsedByIssue: { ...s.collapsedByIssue, [issueId]: next } };
          }
          return { collapsedByIssue: { ...s.collapsedByIssue, [issueId]: [...current, commentId] } };
        }),
      setIssueCommentsCollapsed: (issueId, commentIds, collapsed) =>
        set((s) => {
          const ids = [...new Set(commentIds)];
          if (ids.length === 0) return s;

          const current = s.collapsedByIssue[issueId] ?? [];
          if (collapsed) {
            const next = [...new Set([...current, ...ids])];
            if (next.length === current.length) return s;
            return { collapsedByIssue: { ...s.collapsedByIssue, [issueId]: next } };
          }

          const remove = new Set(ids);
          const next = current.filter((id) => !remove.has(id));
          if (next.length === current.length) return s;
          if (next.length === 0) {
            const { [issueId]: _, ...rest } = s.collapsedByIssue;
            return { collapsedByIssue: rest };
          }
          return { collapsedByIssue: { ...s.collapsedByIssue, [issueId]: next } };
        }),
    }),
    {
      name: "multica_comment_collapse",
      storage: createJSONStorage(() => createWorkspaceAwareStorage(defaultStorage)),
    },
  ),
);

registerForWorkspaceRehydration(() => useCommentCollapseStore.persist.rehydrate());
