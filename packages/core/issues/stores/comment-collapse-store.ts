import { create } from "zustand";
import { createJSONStorage, persist } from "zustand/middleware";
import { createWorkspaceAwareStorage, registerForWorkspaceRehydration } from "../../platform/workspace-storage";
import { defaultStorage } from "../../platform/storage";

type CommentIdsByIssue = Record<string, string[]>;

/** Explicit reader choices override the current starting default. */
interface CommentCollapseStore {
  collapsedByIssue: CommentIdsByIssue;
  expandedByIssue: CommentIdsByIssue;
  isCollapsed: (issueId: string, commentId: string, defaultCollapsed?: boolean) => boolean;
  toggle: (issueId: string, commentId: string, defaultCollapsed?: boolean) => void;
  setCollapsed: (issueId: string, commentId: string, collapsed: boolean) => void;
  collapseAll: (issueId: string, commentIds: readonly string[]) => void;
  expandAll: (issueId: string, commentIds: readonly string[]) => void;
}

function withoutIssue(ids: CommentIdsByIssue, issueId: string): CommentIdsByIssue {
  if (!(issueId in ids)) return ids;
  const { [issueId]: _, ...rest } = ids;
  return rest;
}

function withComment(
  ids: CommentIdsByIssue,
  issueId: string,
  commentId: string,
  present: boolean,
): CommentIdsByIssue {
  const current = ids[issueId] ?? [];
  if (current.includes(commentId) === present) return ids;
  const next = present ? [...current, commentId] : current.filter((id) => id !== commentId);
  return next.length ? { ...ids, [issueId]: next } : withoutIssue(ids, issueId);
}

export const useCommentCollapseStore = create<CommentCollapseStore>()(
  persist(
    (set, get) => ({
      collapsedByIssue: {},
      expandedByIssue: {},
      isCollapsed: (issueId, commentId, defaultCollapsed = false) => {
        const state = get();
        if (state.collapsedByIssue[issueId]?.includes(commentId)) return true;
        if (state.expandedByIssue[issueId]?.includes(commentId)) return false;
        return defaultCollapsed;
      },
      toggle: (issueId, commentId, defaultCollapsed = false) => {
        const state = get();
        state.setCollapsed(issueId, commentId, !state.isCollapsed(issueId, commentId, defaultCollapsed));
      },
      setCollapsed: (issueId, commentId, collapsed) =>
        set((s) => {
          const collapsedByIssue = withComment(s.collapsedByIssue, issueId, commentId, collapsed);
          const expandedByIssue = withComment(s.expandedByIssue, issueId, commentId, !collapsed);
          if (collapsedByIssue === s.collapsedByIssue && expandedByIssue === s.expandedByIssue) return s;
          return { collapsedByIssue, expandedByIssue };
        }),
      collapseAll: (issueId, commentIds) =>
        set((s) => ({
          collapsedByIssue: commentIds.length
            ? { ...s.collapsedByIssue, [issueId]: [...new Set(commentIds)] }
            : withoutIssue(s.collapsedByIssue, issueId),
          expandedByIssue: withoutIssue(s.expandedByIssue, issueId),
        })),
      expandAll: (issueId, commentIds) =>
        set((s) => ({
          collapsedByIssue: withoutIssue(s.collapsedByIssue, issueId),
          expandedByIssue: commentIds.length
            ? { ...s.expandedByIssue, [issueId]: [...new Set(commentIds)] }
            : withoutIssue(s.expandedByIssue, issueId),
        })),
    }),
    {
      name: "multica_comment_collapse",
      storage: createJSONStorage(() => createWorkspaceAwareStorage(defaultStorage)),
    },
  ),
);

registerForWorkspaceRehydration(() => useCommentCollapseStore.persist.rehydrate());
