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
//   `markdownLink` — the URL the editor writes into markdown bodies.
//                    Source: `att.markdown_url` from the server, which
//                    `buildMarkdownURL` picks per deployment policy
//                    (public CDN durable URL, or
//                    `<MULTICA_PUBLIC_URL>/api/attachments/<id>/download`).
//                    The contract is "absolute, no TTL, loads natively
//                    on every client" — that's what fixes the Desktop /
//                    mobile-webview regression where a site-relative
//                    `/api/attachments/<id>/download` couldn't resolve
//                    against `file://` (MUL-3192).
//
//                    Falls back through two layers when the server
//                    didn't populate `markdown_url`:
//                      1. `attachmentDownloadPath(att.id)` — the legacy
//                         site-relative shape. Works on web (Next
//                         rewrite proxies /api/* to the API host) and
//                         is what older comments persist; render
//                         surfaces handle the absolutize for non-web
//                         clients via attachment.tsx's legacy compat.
//                      2. `att.url` — the no-workspace avatar branch
//                         where there's no attachment-row id at all.
//
// MUL-3130 introduced the persisted-image regression by collapsing
// these two semantics into a single `link` field; MUL-3192 followed up
// by moving the durable-URL choice from the client to the server so
// the persisted shape is correct for the deployment without the client
// having to know whether it's running on web / desktop / mobile.
export type UploadResult = Attachment & {
  link: string;
  markdownLink: string;
};

export interface UploadContext {
  issueId?: string;
  commentId?: string;
  chatSessionId?: string;
}

// pickMarkdownLink chooses the URL the editor will write into markdown.
//
// Order:
//   1. `att.markdown_url` — server-provided durable URL. This is the
//      modern contract introduced in MUL-3192; the server (`buildMarkdownURL`)
//      decides whether to emit a public CDN URL or an absolute API
//      endpoint pinned to `MULTICA_PUBLIC_URL` based on the deployment.
//   2. `attachmentDownloadPath(att.id)` — site-relative legacy shape,
//      retained for compatibility with backends old enough to predate
//      MUL-3192. Web's Next rewrite makes this load; desktop / mobile
//      surfaces hit the attachment.tsx legacy-absolutize fallback.
//   3. `att.url` — no attachment-row id (the no-workspace avatar branch
//      of UploadFile). Markdown callers fall back to whatever storage
//      backend produced for the upload; persistence is on the caller.
function pickMarkdownLink(att: Attachment): string {
  if (att.markdown_url) return att.markdown_url;
  if (att.id) return attachmentDownloadPath(att.id);
  return att.url;
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
        return { ...att, link: att.url, markdownLink: pickMarkdownLink(att) };
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
