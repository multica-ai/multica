"use client";

import { useState, useCallback } from "react";
import { toast } from "sonner";
import { api } from "@/shared/api";
import type { Attachment } from "@/shared/types";

const MAX_FILE_SIZE = 100 * 1024 * 1024; // 100 MB

export interface UploadResult {
  id: string;
  filename: string;
  link: string;
}

export interface UploadContext {
  issueId?: string;
  commentId?: string;
}

export function useFileUpload() {
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
        });
        return { id: att.id, filename: att.filename, link: att.url };
      } finally {
        setUploading(false);
      }
    },
    [],
  );

  const uploadWithToast = useCallback(
    async (file: File, ctx?: UploadContext): Promise<UploadResult | null> => {
      try {
        return await upload(file, ctx);
      } catch (err) {
        toast.error(err instanceof Error ? err.message : "Upload failed");
        return null;
      }
    },
    [upload],
  );

  return { upload, uploadWithToast, uploading };
}
