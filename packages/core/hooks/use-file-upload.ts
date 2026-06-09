"use client";

import { useState, useCallback } from "react";
import type { ApiClient } from "../api/client";
import type { Attachment } from "../types";
import { attachmentDownloadPath } from "../types/attachment-url";
import { MAX_FILE_SIZE } from "../constants/upload";

// Carries the full Attachment so editors that need preview metadata
// (`content_type`, `download_url`) get it directly. Two URL fields are
// surfaced because they have different lifetimes:
//
//   `link`         — the same value as `att.url`. Short-lived for the
//                    LocalStorage backend (HMAC-signed `/uploads/<key>`)
//                    and a long-lived CDN URL on S3 / CloudFront. This
//                    is what avatar / logo callers persist into
//                    `avatar_url` style fields, and what URL-only
//                    consumers (Markdown renderers without a record
//                    in hand) get to load directly. Keeping it
//                    semantically equal to `att.url` preserves the
//                    pre-MUL-3130 contract for non-markdown callers
//                    so avatar uploads do not get rerouted through
//                    the workspace-membership-gated download endpoint.
//
//   `markdownLink` — the stable per-attachment URL
//                    `/api/attachments/<id>/download`. Safe to embed
//                    in markdown bodies that outlive the current
//                    session because the server self-resolves the
//                    workspace from the attachment row and re-signs /
//                    proxies on every request. Empty when the upload
//                    has no attachment-row id (the no-workspace
//                    avatar branch of UploadFile, which falls back
//                    to `link`). Only the editor's markdown-persisting
//                    flow should use it; everything else uses `link`.
//
// MUL-3130 introduced the persisted-image regression by collapsing
// these two semantics into a single `link` field. Keeping them
// separate is what lets new comments embed a stable URL while
// avatar / logo callers continue to write `att.url` into long-lived
// fields without picking up workspace-membership gating.
export type UploadResult = Attachment & {
  link: string;
  markdownLink: string;
};

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
        // Avatar / no-workspace uploads come back without an
        // attachment-row id (UploadFile's no-workspace branch returns
        // {id, url, filename} that fails the AttachmentResponseSchema
        // and degrades to the empty record). In that case the stable
        // markdown URL doesn't exist — markdown callers fall back to
        // `link` which mirrors `att.url`.
        const markdownLink = att.id ? attachmentDownloadPath(att.id) : att.url;
        return { ...att, link: att.url, markdownLink };
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
