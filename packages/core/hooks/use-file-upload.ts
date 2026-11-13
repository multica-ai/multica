"use client";

import { useState, useCallback } from "react";
import type { ApiClient } from "../api/client";
import type { Attachment } from "../types";
import { attachmentDownloadPath } from "../types/attachment-url";
import { MAX_FILE_SIZE } from "../constants/upload";

// UploadResult keeps raw storage and durable Markdown URLs separate.
// link preserves att.url for avatars and other storage-reference consumers.
// markdownLink uses the server's absolute, non-expiring markdown_url so native
// clients can load persisted media across origins. Older responses fall back to
// an attachment-ID path, then the raw URL when no attachment row exists.
// The full Attachment carries preview metadata alongside these conveniences.
export type UploadResult = Attachment & {
  link: string;
  markdownLink: string;
};

export interface UploadContext {
  issueId?: string;
  commentId?: string;
  chatSessionId?: string;
}

// pickMarkdownLink prefers the server's durable URL. Older responses fall back
// to an attachment-ID path, then raw storage for uploads without a row ID.
function pickMarkdownLink(att: Attachment): string {
  if (att.markdown_url) return att.markdown_url;
  if (att.id) return attachmentDownloadPath(att.id);
  return att.url;
}

// Adapt a completed server `Attachment` into the editor's `UploadResult` (the
// two URL fields the editor writes into markdown / persists). Exported so the
// coordinated-upload path (MUL-5181), which runs the request through the
// module-level upload coordinator rather than this hook, can hand the editor
// the exact same shape for its inline preview swap.
export function toUploadResult(att: Attachment): UploadResult {
  return { ...att, link: att.url, markdownLink: pickMarkdownLink(att) };
}

export function useFileUpload(
  api: ApiClient,
  // Receives the failing `file` alongside the error so hosts can name it in
  // the failure toast. `uploadWithToast` swallows the rejection after this
  // fires, so a host that passes nothing reports nothing — the upload
  // placeholder still disappears, which on its own reads as "my file
  // silently vanished" (MUL-4808).
  onError?: (error: Error, file: File) => void,
) {
  // In-flight counter, NOT a single boolean. Callers fire multiple uploads
  // concurrently (drag-drop of N files, paste with multiple images) and the
  // boolean shape would flip false as soon as the FIRST upload's finally ran
  // — even though N-1 are still mid-request. Surfaces consuming `uploading`
  // (the quick-create submit gate, the editor's "Uploading…" button label)
  // would then unblock submit while uploads are still in flight, causing
  // `stripBlobUrls` to erase the still-pending images from the markdown and
  // their attachment ids never to be bound (MUL-3339).
  //
  // The exposed `uploading: boolean` keeps the existing call-site contract
  // (`{ uploading } = useFileUpload(api)` everywhere); only the internal
  // tracking shape changes.
  const [inFlight, setInFlight] = useState(0);
  const uploading = inFlight > 0;

  const upload = useCallback(
    async (file: File, ctx?: UploadContext): Promise<UploadResult | null> => {
      if (file.size > MAX_FILE_SIZE) {
        throw new Error("File exceeds 100 MB limit");
      }

      setInFlight((n) => n + 1);
      try {
        const att: Attachment = await api.uploadFile(file, {
          issueId: ctx?.issueId,
          commentId: ctx?.commentId,
          chatSessionId: ctx?.chatSessionId,
        });
        return toUploadResult(att);
      } finally {
        setInFlight((n) => n - 1);
      }
    },
    [api],
  );

  const uploadWithToast = useCallback(
    async (file: File, ctx?: UploadContext): Promise<UploadResult | null> => {
      try {
        return await upload(file, ctx);
      } catch (err) {
        onError?.(err instanceof Error ? err : new Error("Upload failed"), file);
        return null;
      }
    },
    [upload, onError],
  );

  return { upload, uploadWithToast, uploading };
}
