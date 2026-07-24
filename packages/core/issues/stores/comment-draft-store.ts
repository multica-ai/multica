import { create } from "zustand";
import { createJSONStorage, persist } from "zustand/middleware";
import { createWorkspaceAwareStorage, registerForWorkspaceRehydration } from "../../platform/workspace-storage";
import { defaultStorage } from "../../platform/storage";
import { registerDraftCleanup } from "../../drafts/cleanup-registry";
import {
  type DraftUpload,
  type PendingDraftUpload,
  attachmentToDraftUpload,
  normalizeStoredUploads,
  uploadedAttachments,
} from "../../drafts/draft-upload";
import type { Attachment } from "../../types";

/**
 * Per-comment draft persistence — survives:
 *  - virtualization unmount (the reason this exists: when a TipTap editor
 *    scrolls out of the Virtuoso viewport, its in-memory state is lost)
 *  - tab close / accidental Cmd-W
 *  - reload
 *
 * A draft holds both the in-progress text AND the uploads started in the same
 * composer session (MUL-5181). Uploads used to live in component `useState`,
 * so closing or scrolling the composer away dropped in-flight and already-
 * uploaded files; persisting them here keeps them recoverable exactly like the
 * text. Each upload is a {@link DraftUpload} that carries its status — an
 * in-flight placeholder (owned by the upload coordinator) becomes the full
 * attachment once it settles, and a placeholder still `uploading` at load time
 * is coerced to `interrupted` because the bytes were never persisted.
 *
 * Keys are issue-scoped because createWorkspaceAwareStorage only partitions
 * by workspace, not by issue. Without issueId in the key, two issues with
 * thread replies open in adjacent desktop tabs would collide.
 */

export type CommentDraftKey =
  | `new:${string}`              // top-level CommentInput, key = `new:${issueId}`
  | `reply:${string}:${string}`  // ReplyInput inside a thread, key = `reply:${issueId}:${rootCommentId}`
  | `edit:${string}:${string}`;  // inline edit on existing comment, key = `edit:${issueId}:${commentId}`

interface CommentDraft {
  content: string;
  /** Uploads (placeholders + completed) for this composer session. */
  attachments: DraftUpload[];
  updatedAt: number;
}

interface CommentDraftStore {
  drafts: Record<string, CommentDraft>;
  getDraft: (key: CommentDraftKey) => string | undefined;
  /** Completed attachment rows only — the submit-bindable / preview set. */
  getAttachments: (key: CommentDraftKey) => Attachment[];
  /** Every upload for this draft (placeholders included) — for status chips. */
  getUploads: (key: CommentDraftKey) => DraftUpload[];
  setDraft: (key: CommentDraftKey, content: string) => void;
  /** Replace the upload list with completed attachments (legacy/compat path). */
  setAttachments: (key: CommentDraftKey, attachments: Attachment[]) => void;
  /** Record a placeholder the moment a file is picked (coordinator-owned). */
  addUpload: (key: CommentDraftKey, upload: DraftUpload) => void;
  /** Swap a placeholder for its completed attachment. No-op if it's gone. */
  settleUpload: (key: CommentDraftKey, clientUploadId: string, attachment: Attachment) => void;
  /** Mark a placeholder failed. No-op if it's gone. */
  failUpload: (key: CommentDraftKey, clientUploadId: string, error?: string) => void;
  /** Drop a placeholder (e.g. a dismissed failure). */
  removeUpload: (key: CommentDraftKey, clientUploadId: string) => void;
  clearDraft: (key: CommentDraftKey) => void;
}

// Drafts older than 30 days are dropped on store init. Without TTL the store
// would accumulate every edit attempt across every issue indefinitely and
// slowly leak localStorage quota.
const TTL_MS = 30 * 24 * 60 * 60 * 1000;

// Stable empty references so reads on a missing/empty draft return the same
// array every call — reading through a Zustand selector must not allocate, or
// every store touch would re-render the composer.
const EMPTY_UPLOADS: DraftUpload[] = [];
const EMPTY_ATTACHMENTS: Attachment[] = [];

// Derived completed-attachment lists, memoized by the uploads-array reference.
// Every mutation replaces the uploads array, so a stable ref means the derived
// list is unchanged — this keeps the `getAttachments` selector referentially
// stable across unrelated store touches.
const derivedCache = new WeakMap<DraftUpload[], Attachment[]>();
function deriveUploaded(uploads: DraftUpload[]): Attachment[] {
  const cached = derivedCache.get(uploads);
  if (cached) return cached;
  const derived = uploadedAttachments(uploads);
  const result = derived.length === 0 ? EMPTY_ATTACHMENTS : derived;
  derivedCache.set(uploads, result);
  return result;
}

// A draft carries recoverable intent when it has non-blank text OR at least one
// upload (in any state). Upload-only drafts (text deleted, a file still pending
// or failed) are meaningful — pruning or dropping them would silently discard
// the upload the whole persistence exists to protect.
function isMeaningful(content: string, uploads: DraftUpload[]): boolean {
  return content.trim().length > 0 || uploads.length > 0;
}

// Shared writer for setDraft/setAttachments/upload mutations: an entry with
// neither text nor uploads carries nothing to recover, so drop it instead of
// leaving a blank record behind (parity with the old onUpdate "clear on empty
// text").
function writeDraft(
  drafts: Record<string, CommentDraft>,
  key: string,
  content: string,
  uploads: DraftUpload[],
): Record<string, CommentDraft> {
  if (!isMeaningful(content, uploads)) {
    if (!(key in drafts)) return drafts;
    const next = { ...drafts };
    delete next[key];
    return next;
  }
  return { ...drafts, [key]: { content, attachments: uploads, updatedAt: Date.now() } };
}

function uploadsOf(drafts: Record<string, CommentDraft>, key: string): DraftUpload[] {
  return drafts[key]?.attachments ?? EMPTY_UPLOADS;
}

function pruneStaleDrafts(drafts: Record<string, CommentDraft>): Record<string, CommentDraft> {
  const cutoff = Date.now() - TTL_MS;
  const out: Record<string, CommentDraft> = {};
  for (const [k, v] of Object.entries(drafts)) {
    // Normalize every persisted draft: legacy `Attachment[]` becomes uploaded
    // placeholders, and any placeholder still `uploading` becomes `interrupted`
    // (the bytes were never persisted, so the upload cannot resume).
    const uploads = normalizeStoredUploads(v.attachments);
    if (v.updatedAt >= cutoff && isMeaningful(v.content, uploads)) {
      out[k] = { ...v, attachments: uploads };
    }
  }
  return out;
}

export const useCommentDraftStore = create<CommentDraftStore>()(
  persist(
    (set, get) => ({
      drafts: {},
      getDraft: (key) => get().drafts[key]?.content,
      getAttachments: (key) => deriveUploaded(uploadsOf(get().drafts, key)),
      getUploads: (key) => uploadsOf(get().drafts, key),
      setDraft: (key, content) =>
        set((s) => ({
          drafts: writeDraft(s.drafts, key, content, uploadsOf(s.drafts, key)),
        })),
      setAttachments: (key, attachments) =>
        set((s) => ({
          drafts: writeDraft(
            s.drafts,
            key,
            s.drafts[key]?.content ?? "",
            attachments.map(attachmentToDraftUpload),
          ),
        })),
      addUpload: (key, upload) =>
        set((s) => {
          const current = uploadsOf(s.drafts, key);
          if (current.some((u) => u.clientUploadId === upload.clientUploadId)) return s;
          return {
            drafts: writeDraft(s.drafts, key, s.drafts[key]?.content ?? "", [...current, upload]),
          };
        }),
      settleUpload: (key, clientUploadId, attachment) =>
        set((s) => {
          const current = uploadsOf(s.drafts, key);
          if (!current.some((u) => u.clientUploadId === clientUploadId)) return s;
          const next = current.map((u) =>
            u.clientUploadId === clientUploadId
              ? { ...attachmentToDraftUpload(attachment), clientUploadId }
              : u,
          );
          return {
            drafts: writeDraft(s.drafts, key, s.drafts[key]?.content ?? "", next),
          };
        }),
      failUpload: (key, clientUploadId, error) =>
        set((s) => {
          const current = uploadsOf(s.drafts, key);
          const target = current.find((u) => u.clientUploadId === clientUploadId);
          if (!target) return s;
          const failed: PendingDraftUpload = {
            clientUploadId,
            status: "failed",
            filename: target.filename,
            size: target.size,
            contentType: target.contentType,
            error,
          };
          const next = current.map((u) => (u.clientUploadId === clientUploadId ? failed : u));
          return {
            drafts: writeDraft(s.drafts, key, s.drafts[key]?.content ?? "", next),
          };
        }),
      removeUpload: (key, clientUploadId) =>
        set((s) => {
          const current = uploadsOf(s.drafts, key);
          if (!current.some((u) => u.clientUploadId === clientUploadId)) return s;
          const next = current.filter((u) => u.clientUploadId !== clientUploadId);
          return {
            drafts: writeDraft(s.drafts, key, s.drafts[key]?.content ?? "", next),
          };
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

registerDraftCleanup({
  storageKey: "multica_comment_drafts",
  workspaceScoped: true,
  resetInMemory: () => useCommentDraftStore.setState({ drafts: {} }),
});
