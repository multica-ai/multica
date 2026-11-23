import { type ReplyAnnotation, EMPTY_REPLY_ANNOTATIONS, MAX_REPLY_ANNOTATIONS, MAX_ANNOTATION_QUOTE_LENGTH, hasAnnotationContent, normalizeReplyAnnotations } from "../../drafts/reply-annotation";
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
import { contentReferencesAttachment } from "../../types/attachment-url";
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
 * is dropped because the bytes were never persisted.
 *
 * Keys are issue-scoped because createWorkspaceAwareStorage only partitions
 * by workspace, not by issue. Without issueId in the key, two issues with
 * thread replies open in adjacent desktop tabs would collide.
 */

export type CommentDraftKey =
  | `new:${string}`              // top-level CommentInput, key = `new:${issueId}`
  | `reply:${string}:${string}`  // ReplyInput inside a thread, key = `reply:${issueId}:${rootCommentId}`
  | `edit:${string}:${string}`   // inline edit on existing comment, key = `edit:${issueId}:${commentId}`
  | `chat:${string}`;            // chat composer annotations only, key = `chat:${sessionId}`; text lives in the chat store

export interface PendingAnnotationDraft {
  annotation: ReplyAnnotation;
  existing: boolean;
}

interface CommentDraft {
  content: string;
  /** Uploads (placeholders + completed) for this composer session. */
  attachments: DraftUpload[];
  annotations?: ReplyAnnotation[];
  /** Unconfirmed selection-annotation note kept across virtualization unmount / tab switches. */
  pendingAnnotation?: PendingAnnotationDraft;
  replyTarget?: { commentId: string; actorName: string };
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
  /**
   * Append a markdown fragment to the draft body (upload write-back,
   * MUL-5181): an upload that finished after its composer unmounted has no
   * live editor to insert its link, so the settle handler lands it here —
   * the draft body stays the single source of truth for what gets bound.
   */
  appendToDraftContent: (key: CommentDraftKey, markdown: string) => void;
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
  getAnnotations: (key: CommentDraftKey) => ReplyAnnotation[];
  addAnnotation: (key: CommentDraftKey, annotation: ReplyAnnotation) => string | undefined;
  updateAnnotation: (key: CommentDraftKey, id: string, note: string, attachmentUploadIds?: string[]) => void;
  removeAnnotation: (key: CommentDraftKey, id: string) => void;
  getPendingAnnotation: (key: CommentDraftKey) => PendingAnnotationDraft | undefined;
  setPendingAnnotation: (key: CommentDraftKey, draft: PendingAnnotationDraft) => void;
  clearPendingAnnotation: (key: CommentDraftKey) => void;
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
function isMeaningful(
  content: string,
  uploads: DraftUpload[],
  annotations: ReplyAnnotation[] = EMPTY_REPLY_ANNOTATIONS,
  pendingAnnotation?: PendingAnnotationDraft,
): boolean {
  return content.trim().length > 0 || uploads.length > 0 || annotations.length > 0 || !!pendingAnnotation;
}

function retainAvailableAnnotationIds(annotation: ReplyAnnotation, uploads: DraftUpload[]): ReplyAnnotation {
  if (!annotation.attachmentUploadIds?.length) return annotation;
  const attachmentUploadIds = annotation.attachmentUploadIds.filter((id) => uploads.some((upload) => upload.clientUploadId === id));
  return attachmentUploadIds.length === annotation.attachmentUploadIds.length
    ? annotation : { ...annotation, attachmentUploadIds };
}

function normalizePendingAnnotation(value: unknown, uploads: DraftUpload[]): PendingAnnotationDraft | undefined {
  if (!value || typeof value !== "object") return undefined;
  const candidate = value as { annotation?: unknown; existing?: unknown };
  if (typeof candidate.existing !== "boolean") return undefined;
  const draft = candidate.annotation as Partial<ReplyAnnotation> | undefined;
  const allowEmptyNote = candidate.existing && Array.isArray(draft?.attachmentUploadIds);
  const normalized = normalizeReplyAnnotations([draft], { allowEmptyNote })[0];
  if (!normalized) return undefined;
  const annotation = retainAvailableAnnotationIds(normalized, uploads);
  return allowEmptyNote || hasAnnotationContent(annotation)
    ? { annotation, existing: candidate.existing }
    : undefined;
}

function samePendingAnnotation(a?: PendingAnnotationDraft, b?: PendingAnnotationDraft): boolean {
  if (a === b) return true;
  if (!a || !b) return false;
  return (
    a.existing === b.existing &&
    a.annotation.id === b.annotation.id &&
    a.annotation.sourceCommentId === b.annotation.sourceCommentId &&
    a.annotation.sourceActorName === b.annotation.sourceActorName &&
    a.annotation.sourceRevision === b.annotation.sourceRevision &&
    a.annotation.quote === b.annotation.quote &&
    a.annotation.note === b.annotation.note &&
    sameAttachmentIds(a.annotation.attachmentUploadIds, b.annotation.attachmentUploadIds) &&
    a.annotation.start === b.annotation.start &&
    a.annotation.prefix === b.annotation.prefix &&
    a.annotation.suffix === b.annotation.suffix
  );
}

function sameAttachmentIds(a?: string[], b?: string[]): boolean {
  return a === b || (!!a && !!b && a.length === b.length && a.every((id, index) => id === b[index]));
}

// Shared writer for setDraft/setAttachments/upload mutations: an entry with
// neither text nor uploads carries nothing to recover, so drop it instead of
// leaving a blank record behind (parity with the old onUpdate "clear on empty
// text"). Pass `pendingArg = null` to clear `pendingAnnotation`; omit it
// (`undefined`) to preserve the existing value.
function writeDraft(
  drafts: Record<string, CommentDraft>,
  key: string,
  content: string,
  uploads: DraftUpload[],
  annotations = drafts[key]?.annotations ?? EMPTY_REPLY_ANNOTATIONS,
  replyTarget = key.startsWith("reply:") && annotations.length ? drafts[key]?.replyTarget : undefined,
  pendingArg?: PendingAnnotationDraft | null,
): Record<string, CommentDraft> {
  const pendingAnnotation =
    pendingArg === undefined ? drafts[key]?.pendingAnnotation : (pendingArg ?? undefined);
  if (!isMeaningful(content, uploads, annotations, pendingAnnotation)) {
    if (!(key in drafts)) return drafts;
    const next = { ...drafts };
    delete next[key];
    return next;
  }
  // Idempotent on identical writes: the composers' visibilitychange/pagehide
  // flush re-writes the same content on every tab switch, and the stale-submit
  // guard compares entry identity — a fresh-but-equal entry would read as "the
  // user edited during the request" and wrongly keep a submitted draft alive.
  const existing = drafts[key];
  if (
    existing &&
    existing.content === content &&
    existing.replyTarget === replyTarget &&
    (existing.annotations ?? EMPTY_REPLY_ANNOTATIONS) === annotations &&
    existing.attachments === uploads &&
    samePendingAnnotation(existing.pendingAnnotation, pendingAnnotation)
  ) {
    return drafts;
  }
  return {
    ...drafts,
    [key]: { content, attachments: uploads, annotations, pendingAnnotation, replyTarget, updatedAt: Date.now() },
  };
}

function uploadsOf(drafts: Record<string, CommentDraft>, key: string): DraftUpload[] {
  return drafts[key]?.attachments ?? EMPTY_UPLOADS;
}

// Chat stores the main body in a separate draft. Its comment-draft uploads
// belong to annotations, so closing the pending note keeps only saved links.
function retainSavedAnnotationUploads(
  key: string,
  uploads: DraftUpload[],
  annotations: readonly ReplyAnnotation[],
): DraftUpload[] {
  if (!key.startsWith("chat:") || uploads.length === 0) return uploads;
  return uploads.filter((upload) =>
    upload.status === "uploaded" &&
    annotations.some(({ note, attachmentUploadIds }) =>
      attachmentUploadIds?.includes(upload.clientUploadId) || contentReferencesAttachment(note, upload.attachment)));
}

function pruneStaleDrafts(drafts: Record<string, CommentDraft>): Record<string, CommentDraft> {
  const cutoff = Date.now() - TTL_MS;
  const out: Record<string, CommentDraft> = {};
  for (const [k, v] of Object.entries(drafts)) {
    // Normalize every persisted draft: legacy `Attachment[]` becomes uploaded
    // placeholders, and any placeholder still `uploading` is dropped (the bytes
    // were never persisted, so the upload cannot resume).
    const normalizedUploads = normalizeStoredUploads(v.attachments);
    const annotations = normalizeReplyAnnotations(v.annotations)
      .map((annotation) => retainAvailableAnnotationIds(annotation, normalizedUploads))
      .filter(hasAnnotationContent);
    const pendingAnnotation = normalizePendingAnnotation(v.pendingAnnotation, normalizedUploads);
    const uploads = pendingAnnotation
      ? normalizedUploads
      : retainSavedAnnotationUploads(k, normalizedUploads, annotations);
    if (v.updatedAt >= cutoff && isMeaningful(v.content, uploads, annotations, pendingAnnotation)) {
      const first = annotations[0];
      const replyTarget = k.startsWith("reply:") && first ? (v.replyTarget && typeof v.replyTarget.commentId === "string" && typeof v.replyTarget.actorName === "string"
        ? v.replyTarget : { commentId: first.sourceCommentId, actorName: first.sourceActorName }) : undefined;
      out[k] = { ...v, attachments: uploads, annotations, pendingAnnotation, replyTarget };
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
      appendToDraftContent: (key, markdown) =>
        set((s) => {
          const existing = s.drafts[key]?.content ?? "";
          const next = existing.trim() ? `${existing.replace(/\s+$/, "")}\n\n${markdown}` : markdown;
          return {
            drafts: writeDraft(s.drafts, key, next, uploadsOf(s.drafts, key)),
          };
        }),
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
      getAnnotations: (key) => get().drafts[key]?.annotations ?? EMPTY_REPLY_ANNOTATIONS,
      addAnnotation: (key, candidate) => {
        const annotation = retainAvailableAnnotationIds(candidate, get().getUploads(key));
        if ((!key.startsWith("reply:") && !key.startsWith("new:") && !key.startsWith("chat:")) || !hasAnnotationContent(annotation) || !annotation.quote.trim() || annotation.quote.length > MAX_ANNOTATION_QUOTE_LENGTH) return undefined;
        const current = get().getAnnotations(key);
        const duplicate = current.find((a) => a.sourceCommentId === annotation.sourceCommentId &&
          a.start === annotation.start && a.quote === annotation.quote && a.prefix === annotation.prefix && a.suffix === annotation.suffix);
        if (duplicate) return duplicate.id;
        if (current.length >= MAX_REPLY_ANNOTATIONS) return undefined;
        set((s) => {
          const replyTarget = key.startsWith("reply:")
            ? s.drafts[key]?.replyTarget ?? { commentId: annotation.sourceCommentId, actorName: annotation.sourceActorName }
            : undefined;
          return { drafts: writeDraft(s.drafts, key, s.drafts[key]?.content ?? "", uploadsOf(s.drafts, key), [...current, annotation], replyTarget) };
        });
        return annotation.id;
      },
      updateAnnotation: (key, id, note, attachmentUploadIds) => set((s) => {
        const current = s.drafts[key]?.annotations ?? EMPTY_REPLY_ANNOTATIONS;
        const original = current.find((a) => a.id === id);
        if (!original) return s;
        const updated = retainAvailableAnnotationIds(
          { ...original, note, attachmentUploadIds: attachmentUploadIds ?? original.attachmentUploadIds },
          uploadsOf(s.drafts, key),
        );
        if (original.note === note && sameAttachmentIds(original.attachmentUploadIds, updated.attachmentUploadIds)) return s;
        const annotations = hasAnnotationContent(updated)
          ? current.map((a) => a.id === id ? updated : a)
          : current.filter((a) => a.id !== id);
        return { drafts: writeDraft(s.drafts, key, s.drafts[key]?.content ?? "", uploadsOf(s.drafts, key), annotations) };
      }),
      removeAnnotation: (key, id) => set((s) => {
        const current = s.drafts[key]?.annotations ?? EMPTY_REPLY_ANNOTATIONS;
        if (!current.some((a) => a.id === id)) return s;
        const pending = s.drafts[key]?.pendingAnnotation;
        const nextPending = pending?.annotation.id === id ? null : pending;
        const annotations = current.filter((a) => a.id !== id);
        const uploads = uploadsOf(s.drafts, key);
        return {
          drafts: writeDraft(
            s.drafts,
            key,
            s.drafts[key]?.content ?? "",
            nextPending ? uploads : retainSavedAnnotationUploads(key, uploads, annotations),
            annotations,
            undefined,
            nextPending,
          ),
        };
      }),
      getPendingAnnotation: (key) => get().drafts[key]?.pendingAnnotation,
      setPendingAnnotation: (key, draft) => {
        if (!key.startsWith("reply:") && !key.startsWith("new:") && !key.startsWith("chat:")) return;
        set((s) => {
          const uploads = uploadsOf(s.drafts, key);
          const normalized = normalizePendingAnnotation(draft, uploads);
          return { drafts: writeDraft(
            s.drafts,
            key,
            s.drafts[key]?.content ?? "",
            uploads,
            s.drafts[key]?.annotations ?? EMPTY_REPLY_ANNOTATIONS,
            key.startsWith("reply:") && (s.drafts[key]?.annotations?.length ?? 0) > 0
              ? s.drafts[key]?.replyTarget
              : undefined,
            normalized ?? null,
          ) };
        });
      },
      clearPendingAnnotation: (key) => set((s) => {
        if (!s.drafts[key]?.pendingAnnotation) return s;
        const annotations = s.drafts[key]?.annotations ?? EMPTY_REPLY_ANNOTATIONS;
        return {
          drafts: writeDraft(
            s.drafts,
            key,
            s.drafts[key]?.content ?? "",
            retainSavedAnnotationUploads(key, uploadsOf(s.drafts, key), annotations),
            annotations,
            key.startsWith("reply:") && (s.drafts[key]?.annotations?.length ?? 0) > 0
              ? s.drafts[key]?.replyTarget
              : undefined,
            null,
          ),
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
      // Zustand's default merge keeps the previous workspace's drafts when
      // the destination has no storage entry. An empty namespace must mean
      // an empty draft set, including private annotations and reply targets.
      merge: (persisted, current) => ({
        ...current,
        drafts: (persisted as { drafts?: Record<string, CommentDraft> } | undefined)?.drafts ?? {},
      }),
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
