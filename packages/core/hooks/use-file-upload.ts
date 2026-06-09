"use client";

import { useState, useCallback } from "react";
import type { ApiClient } from "../api/client";
import type { Attachment } from "../types";
import { attachmentDownloadPath } from "../types/attachment-url";
import { MAX_FILE_SIZE } from "../constants/upload";

// Carries the full Attachment so editors that need preview metadata
// (`content_type`, `download_url`) get it directly; `link` is the
// stable persisted URL — `/api/attachments/<id>/download` — that the
// editor writes into the markdown body. The previous design used
// `att.url`, but on the LocalStorage backend that value became a
// 30-min HMAC-signed `/uploads/<key>?exp&sig` URL after MUL-3132.
// Persisting a short-lived signature into a permanent comment body
// broke every image the moment the signature expired (MUL-3130).
// `attachmentDownloadPath(att.id)` re-signs at request time, never
// stops working until the attachment row is deleted, and resolves
// the workspace from the row itself so it loads as a native <img>
// src without any X-Workspace-* headers.
export type UploadResult = Attachment & { link: string };

export interface UploadContext {
  issueId?: string;
  commentId?: string;
  chatSessionId?: string;
}

export function useFileUpload(
  api: ApiClient,
  onError?: (error: Error) => void,
) {
  const [uploading, setUploading] = useState(false);

  const upload = useCallback(
    async (file: File, ctx?: UploadContext): Promise<UploadResult | null> => {
      if (file.size > MAX_FILE_SIZE) {
        throw new Error("File exceeds 100 MB limit");
      }

      setUploading(true);
      try {
        const att: Attachment = await api.uploadFile(file, {
          issueId: ctx?.issueId,
          commentId: ctx?.commentId,
          chatSessionId: ctx?.chatSessionId,
        });
        // Avatar uploads (no workspace context) come back without an id;
        // fall back to the storage URL so legacy avatar pickers that
        // persist `att.url` directly keep working. Comment / issue
        // uploads always carry an id.
        const link = att.id ? attachmentDownloadPath(att.id) : att.url;
        return { ...att, link };
      } finally {
        setUploading(false);
      }
    },
    [api],
  );

  const uploadWithToast = useCallback(
    async (file: File, ctx?: UploadContext): Promise<UploadResult | null> => {
      try {
        return await upload(file, ctx);
      } catch (err) {
        onError?.(err instanceof Error ? err : new Error("Upload failed"));
        return null;
      }
    },
    [upload, onError],
  );

  return { upload, uploadWithToast, uploading };
}
