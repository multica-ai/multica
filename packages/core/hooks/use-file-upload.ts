"use client";

import { useState, useCallback } from "react";
import type { ApiClient } from "../api/client";
import type { Attachment } from "../types";
import { MAX_FILE_SIZE } from "../constants/upload";

export interface UploadResult {
  id: string;
  filename: string;
  link: string;
}

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
        return { id: att.id, filename: att.filename, link: att.url };
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
