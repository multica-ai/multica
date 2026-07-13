import { create } from "zustand";
import { createJSONStorage, persist } from "zustand/middleware";
import { createWorkspaceAwareStorage, registerForWorkspaceRehydration } from "../../platform/workspace-storage";
import { defaultStorage } from "../../platform/storage";
import type { Attachment } from "../../types";

/**
 * Per-comment draft persistence — survives:
 *  - virtualization unmount (the reason this exists: when a TipTap editor
 *    scrolls out of the Virtuoso viewport, its in-memory state is lost)
 *  - desktop tab switch (the Session model renders only the active tab, so an
 *    inactive tab's composer unmounts)
 *  - tab close / accidental Cmd-W
 *  - reload
 *
 * Keys are issue-scoped because createWorkspaceAwareStorage only partitions
 * by workspace, not by issue. Without issueId in the key, two issues with
 * thread replies open in adjacent desktop tabs would collide.
 */

export type CommentDraftKey =
  | `new:${string}`              // top-level CommentInput, key = `new:${issueId}`
  | `reply:${string}:${string}`  // ReplyInput inside a thread, key = `reply:${issueId}:${rootCommentId}`
  | `edit:${string}:${string}`;  // inline edit on existing comment, key = `edit:${issueId}:${commentId}`

export interface CommentDraft {
  content: string;
  /**
   * Attachments uploaded in this composer session. Persisted so the composer's
   * attachment set survives an unmount — otherwise a submit after remount
   * rebuilds `attachment_ids` from an empty list and silently drops them. The
   * attachment resource is already uploaded server-side; this is the draft's
   * reference list, needed only to re-derive `attachment_ids` at submit.
   */
  attachments: Attachment[];
  /** Agent trigger chips the user turned off for this draft. */
  suppressedAgentIds: string[];
  updatedAt: number;
}

/** Any subset of a draft's user-authored fields; the rest is preserved. */
export type CommentDraftPatch = Partial<
  Pick<CommentDraft, "content" | "attachments" | "suppressedAgentIds">
>;

interface CommentDraftStore {
  drafts: Record<string, CommentDraft>;
  getDraft: (key: CommentDraftKey) => CommentDraft | undefined;
  setDraft: (key: CommentDraftKey, patch: CommentDraftPatch) => void;
  clearDraft: (key: CommentDraftKey) => void;
}

// Drafts older than 30 days are dropped on store init. Without TTL the store
// would accumulate every edit attempt across every issue indefinitely and
// slowly leak localStorage quota.
const TTL_MS = 30 * 24 * 60 * 60 * 1000;

/** Fill in fields missing from an older persisted draft shape (content-only). */
function normalizeDraft(draft: Partial<CommentDraft> | undefined): CommentDraft {
  return {
    content: draft?.content ?? "",
    attachments: draft?.attachments ?? [],
    suppressedAgentIds: draft?.suppressedAgentIds ?? [],
    updatedAt: draft?.updatedAt ?? Date.now(),
  };
}

export function pruneStaleDrafts(
  drafts: Record<string, Partial<CommentDraft>>,
): Record<string, CommentDraft> {
  const cutoff = Date.now() - TTL_MS;
  const out: Record<string, CommentDraft> = {};
  for (const [k, v] of Object.entries(drafts)) {
    const draft = normalizeDraft(v);
    // A draft is only submittable with body text. Content-less entries — incl.
    // any orphaned attachments, whose URLs live in that body — are dropped.
    if (draft.updatedAt >= cutoff && draft.content.trim().length > 0) {
      out[k] = draft;
    }
  }
  return out;
}

export const useCommentDraftStore = create<CommentDraftStore>()(
  persist(
    (set, get) => ({
      drafts: {},
      getDraft: (key) => get().drafts[key],
      setDraft: (key, patch) =>
        set((s) => {
          const current = s.drafts[key];
          const next: CommentDraft = {
            content: patch.content ?? current?.content ?? "",
            attachments: patch.attachments ?? current?.attachments ?? [],
            suppressedAgentIds:
              patch.suppressedAgentIds ?? current?.suppressedAgentIds ?? [],
            updatedAt: Date.now(),
          };
          return { drafts: { ...s.drafts, [key]: next } };
        }),
      clearDraft: (key) =>
        set((s) => {
          if (!(key in s.drafts)) return s;
          const next = { ...s.drafts };
          delete next[key];
          return { drafts: next };
        }),
    }),
    {
      name: "multica_comment_drafts",
      storage: createJSONStorage(() => createWorkspaceAwareStorage(defaultStorage)),
      onRehydrateStorage: () => (state) => {
        if (state) {
          state.drafts = pruneStaleDrafts(state.drafts);
        }
      },
    },
  ),
);

registerForWorkspaceRehydration(() => useCommentDraftStore.persist.rehydrate());
