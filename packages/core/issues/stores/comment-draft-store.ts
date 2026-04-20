import { create } from "zustand";
import { createJSONStorage, persist } from "zustand/middleware";
import { createWorkspaceAwareStorage, registerForWorkspaceRehydration } from "../../platform/workspace-storage";
import { defaultStorage } from "../../platform/storage";

/**
 * Persists unsent comment/reply draft text so it survives unmount —
 * e.g. switching issues, collapsing/expanding a comment card, or toggling
 * between the main comment box and a reply box.
 *
 * Keys are caller-supplied strings:
 *   - `comment:<issueId>` for the main comment input on an issue
 *   - `reply:<issueId>:<commentId>` for the reply input on a specific comment
 */
interface CommentDraftStore {
  drafts: Record<string, string>;
  getDraft: (key: string) => string;
  setDraft: (key: string, content: string) => void;
  clearDraft: (key: string) => void;
}

export const useCommentDraftStore = create<CommentDraftStore>()(
  persist(
    (set, get) => ({
      drafts: {},
      getDraft: (key) => get().drafts[key] ?? "",
      setDraft: (key, content) =>
        set((s) => {
          if (!content) {
            if (!(key in s.drafts)) return s;
            const { [key]: _, ...rest } = s.drafts;
            return { drafts: rest };
          }
          if (s.drafts[key] === content) return s;
          return { drafts: { ...s.drafts, [key]: content } };
        }),
      clearDraft: (key) =>
        set((s) => {
          if (!(key in s.drafts)) return s;
          const { [key]: _, ...rest } = s.drafts;
          return { drafts: rest };
        }),
    }),
    {
      name: "multica_comment_draft",
      storage: createJSONStorage(() => createWorkspaceAwareStorage(defaultStorage)),
    },
  ),
);

registerForWorkspaceRehydration(() => useCommentDraftStore.persist.rehydrate());

export function commentDraftKey(issueId: string): string {
  return `comment:${issueId}`;
}

export function replyDraftKey(issueId: string, commentId: string): string {
  return `reply:${issueId}:${commentId}`;
}
