"use client";

/**
 * Coordinated file uploads for the comment composers (MUL-5181, L2).
 *
 * Ownership inversion: the upload is owned by the module-level upload
 * coordinator, not this component. On file pick we write a persisted
 * placeholder into the comment draft IMMEDIATELY, then hand the file to the
 * coordinator. Closing or scrolling the composer away no longer aborts the
 * upload — its result lands in the draft through `onSettled`, so reopening the
 * composer shows the attachment, and a placeholder still "uploading" at reload
 * time surfaces as "interrupted" (the store coerces it on rehydrate).
 *
 * `onSettled` is generation-guarded here: it re-reads the draft and only writes
 * if the placeholder is still tracked (the draft may have been submitted or
 * cleared while the request was in flight). The coordinator never calls
 * `onSettled` on abort, so logout — which aborts every upload then clears the
 * drafts — cannot resurrect a placeholder into a wiped draft.
 *
 * The returned `handleUpload` resolves the editor's `onUploadFile` promise so
 * the happy-path inline preview (blob node → real URL) still works; the SOURCE
 * OF TRUTH for submit is the draft, not the editor document.
 */

import { useCallback, useMemo, useState } from "react";
import { toast } from "sonner";
import { api } from "@multica/core/api";
import { startUpload, type DraftUpload } from "@multica/core/drafts";
import { attachmentToDraftUpload } from "@multica/core/drafts";
import { createSafeId } from "@multica/core/utils";
import type { Attachment } from "@multica/core/types";
import {
  toUploadResult,
  type UploadContext,
  type UploadResult,
} from "@multica/core/hooks/use-file-upload";
import { MAX_FILE_SIZE } from "@multica/core/constants/upload";
import { useCommentDraftStore, type CommentDraftKey } from "@multica/core/issues/stores";
import { useT } from "../../i18n";

const EMPTY_UPLOADS: DraftUpload[] = [];
const EMPTY_ATTACHMENTS: Attachment[] = [];

export interface CommentUploads {
  /** Every upload for this composer (placeholders included) — for status chips. */
  uploads: DraftUpload[];
  /** Completed attachment rows — the editor preview + submit-bindable set. */
  attachments: Attachment[];
  /** Wire to `<ContentEditor onUploadFile={...} />`. */
  handleUpload: (file: File) => Promise<UploadResult | null>;
  /** Drop a placeholder (dismiss a failure / interrupted). */
  removeUpload: (clientUploadId: string) => void;
}

/**
 * @param draftKey  When set, uploads persist in the comment draft store and
 *                  survive unmount. When absent (a reply box with no persistence
 *                  context) they fall back to component-local state.
 */
export function useCommentUploads(
  draftKey: CommentDraftKey | undefined,
  ctx: UploadContext,
): CommentUploads {
  const { t } = useT("editor");
  const [localUploads, setLocalUploads] = useState<DraftUpload[]>([]);

  const storeUploads = useCommentDraftStore((s) =>
    draftKey ? s.getUploads(draftKey) : EMPTY_UPLOADS,
  );
  const storeAttachments = useCommentDraftStore((s) =>
    draftKey ? s.getAttachments(draftKey) : EMPTY_ATTACHMENTS,
  );

  const uploads = draftKey ? storeUploads : localUploads;
  const localAttachments = useMemo(() => {
    if (draftKey) return EMPTY_ATTACHMENTS;
    const done = localUploads.filter((u) => u.status === "uploaded");
    return done.length === 0
      ? EMPTY_ATTACHMENTS
      : done.map((u) => (u as { attachment: Attachment }).attachment);
  }, [draftKey, localUploads]);
  const attachments = draftKey ? storeAttachments : localAttachments;

  const issueId = ctx.issueId;
  const commentId = ctx.commentId;
  const chatSessionId = ctx.chatSessionId;

  const handleUpload = useCallback(
    (file: File): Promise<UploadResult | null> => {
      const clientUploadId = createSafeId();
      const placeholder: DraftUpload = {
        clientUploadId,
        status: "uploading",
        filename: file.name,
        size: file.size,
        contentType: file.type || undefined,
      };

      if (file.size > MAX_FILE_SIZE) {
        // Never enters the coordinator — surface it as a failed placeholder so
        // the composer shows the reason instead of silently dropping the file.
        const reason = "File exceeds 100 MB limit";
        if (draftKey) {
          useCommentDraftStore.getState().addUpload(draftKey, placeholder);
          useCommentDraftStore.getState().failUpload(draftKey, clientUploadId, reason);
        } else {
          setLocalUploads((prev) => [
            ...prev,
            { clientUploadId, status: "failed", filename: file.name, size: file.size, error: reason },
          ]);
        }
        toast.error(t(($) => $.upload.failed, { filename: file.name, reason }));
        return Promise.resolve(null);
      }

      if (draftKey) {
        useCommentDraftStore.getState().addUpload(draftKey, placeholder);
      } else {
        setLocalUploads((prev) => [...prev, placeholder]);
      }

      return new Promise<UploadResult | null>((resolve) => {
        startUpload({
          clientUploadId,
          file,
          api,
          ctx: { issueId, commentId, chatSessionId },
          onSettled: (outcome) => {
            if (outcome.status === "uploaded") {
              if (draftKey) {
                const store = useCommentDraftStore.getState();
                // Generation guard: only write if the draft still tracks it.
                if (store.getUploads(draftKey).some((u) => u.clientUploadId === clientUploadId)) {
                  store.settleUpload(draftKey, clientUploadId, outcome.attachment);
                }
              } else {
                setLocalUploads((prev) =>
                  prev.map((u) =>
                    u.clientUploadId === clientUploadId
                      ? { ...attachmentToDraftUpload(outcome.attachment), clientUploadId }
                      : u,
                  ),
                );
              }
              resolve(toUploadResult(outcome.attachment));
            } else {
              const reason = outcome.error.message;
              if (draftKey) {
                const store = useCommentDraftStore.getState();
                if (store.getUploads(draftKey).some((u) => u.clientUploadId === clientUploadId)) {
                  store.failUpload(draftKey, clientUploadId, reason);
                }
              } else {
                setLocalUploads((prev) =>
                  prev.map((u) =>
                    u.clientUploadId === clientUploadId
                      ? { clientUploadId, status: "failed", filename: file.name, size: file.size, error: reason }
                      : u,
                  ),
                );
              }
              toast.error(t(($) => $.upload.failed, { filename: file.name, reason }));
              resolve(null);
            }
          },
        });
      });
    },
    [draftKey, issueId, commentId, chatSessionId, t],
  );

  const removeUpload = useCallback(
    (clientUploadId: string) => {
      if (draftKey) useCommentDraftStore.getState().removeUpload(draftKey, clientUploadId);
      else setLocalUploads((prev) => prev.filter((u) => u.clientUploadId !== clientUploadId));
    },
    [draftKey],
  );

  return { uploads, attachments, handleUpload, removeUpload };
}
