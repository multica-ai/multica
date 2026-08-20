"use client";

/**
 * Inline media re-sign (MUL-3254).
 *
 * Extracted from editor/attachment.tsx so the preview modal can run the same
 * upgrade: gallery navigation (MUL-5752) hands the modal an attachment the
 * user never clicked, so the modal can no longer rely on the inline renderer
 * having already resolved a loadable URL for it.
 */

import { useEffect, useState } from "react";
import { useQuery } from "@tanstack/react-query";
import { api } from "@multica/core/api";
import { attachmentIdFromDownloadURL } from "@multica/core/types/attachment-url";

// Keep refetches well inside the server's signed-URL TTL (30 min default,
// server/internal/handler/file.go) so a re-render never serves an expired
// signature from the query cache.
const RESIGN_STALE_MS = 20 * 60 * 1000;

// How long fetched image bytes stay in the query cache after the last <img>
// using them unmounts. The bytes themselves never go stale (an attachment id
// maps to an immutable storage object), so the blob query uses
// `staleTime: Infinity`; this bound exists purely to keep a long scroll
// through an image-heavy thread from pinning every decoded screenshot in
// renderer memory forever.
const INLINE_BLOB_GC_MS = 5 * 60 * 1000;

// useResignedInlineMediaURL upgrades an auth-gated media URL to a freshly
// signed one for clients that cannot load `/api/attachments/<id>/download`
// natively.
//
// The picked inline URL can end up being the stable per-attachment API
// endpoint (e.g. a reopened issue draft, whose persisted record deliberately
// strips the short-lived signed `download_url`). That endpoint needs
// credentials: web loads it because the session cookie rides on the <img>
// request when it is genuinely same-origin. Desktop's file:// renderer, the
// mobile webview, and split-origin web deployments cannot rely on that: no
// cookie is attached and the Bearer token cannot be put on a native resource
// fetch, so the image 401s. Desktop/mobile expose a non-empty
// `api.getBaseUrl()`; web can also hit this path when the server emits an
// absolute markdown URL whose origin differs from the current page.
//
// For them, fetch fresh attachment metadata through the authenticated API —
// the same re-sign the click-time download path already does — and swap in
// the response's signed `download_url`.
//
// The server only has a signed URL to offer under CloudFront signing or
// presign mode. In **proxy** download mode `GetAttachmentByID` hands back the
// auth-gated API path again, and before MUL-5445 the renderer simply kept the
// original URL — the image stayed broken and the metadata request was pure
// overhead. Proxy is not an exotic setting: the default `auto` mode forces it
// whenever the storage URL points at an internal host, which is exactly the
// docker-compose MinIO (`http://minio:9000`) self-host shape. So when the
// refreshed metadata confirms there is no signed URL, fall back to pulling the
// bytes through the authenticated client and rendering them from an object
// URL. `blobFallback` gates that on the caller actually rendering a native
// `<img>`: a file card only needs a link, and downloading a 100 MB archive
// into renderer memory to draw a chip would be a bad trade.
export function useResignedInlineMediaURL(
  attachmentId: string | undefined,
  pickedUrl: string,
  blobFallback: boolean,
): string {
  const idFromPickedUrl = attachmentIdFromDownloadURL(pickedUrl);
  const resignAttachmentId = attachmentId ?? idFromPickedUrl;
  const isCrossOriginWebURL = (() => {
    if (!/^https?:\/\//i.test(pickedUrl) || typeof window === "undefined") {
      return false;
    }
    try {
      return new URL(pickedUrl).origin !== window.location.origin;
    } catch {
      return false;
    }
  })();
  const needsResign =
    !!resignAttachmentId &&
    !!pickedUrl &&
    idFromPickedUrl !== undefined &&
    ((api.getBaseUrl?.() ?? "") !== "" || isCrossOriginWebURL);

  const { data: fresh } = useQuery({
    queryKey: ["attachment-inline-resign", resignAttachmentId],
    queryFn: () => api.getAttachment(resignAttachmentId as string),
    enabled: needsResign,
    staleTime: RESIGN_STALE_MS,
    gcTime: RESIGN_STALE_MS,
  });

  const dl = fresh?.download_url ?? "";
  // Accept the fresh URL only when it is an actual upgrade — absolute and no
  // longer the auth-gated API shape (i.e. a signed storage URL the renderer
  // can load natively).
  const signedUrl =
    /^https?:\/\//i.test(dl) && attachmentIdFromDownloadURL(dl) === undefined
      ? dl
      : "";

  // Only after `fresh` has landed do we know this deployment has nothing
  // signed to give — firing the byte fetch earlier would double-download on
  // every CloudFront / presign client.
  const { data: blob } = useQuery({
    queryKey: ["attachment-inline-blob", resignAttachmentId],
    queryFn: () => api.getAttachmentBlob(resignAttachmentId as string),
    enabled: needsResign && blobFallback && !!fresh && signedUrl === "",
    staleTime: Infinity,
    gcTime: INLINE_BLOB_GC_MS,
  });
  const blobUrl = useObjectURL(blob);

  if (!needsResign) return pickedUrl;
  if (signedUrl) return signedUrl;
  return blobUrl || pickedUrl;
}

// useObjectURL turns a Blob into a `blob:` URL for the lifetime of the calling
// component, revoking it on unmount / replacement so the bytes are released
// once nothing renders them. Returns "" while there is no blob (and during
// SSR, where createObjectURL does not exist).
function useObjectURL(blob: Blob | undefined): string {
  const [url, setUrl] = useState("");
  useEffect(() => {
    if (!blob || typeof URL.createObjectURL !== "function") {
      setUrl("");
      return;
    }
    const next = URL.createObjectURL(blob);
    setUrl(next);
    return () => {
      URL.revokeObjectURL(next);
    };
  }, [blob]);
  return url;
}

// isObjectURL flags a src that only resolves inside this renderer session —
// safe to paint, wrong to expose through Copy Link or persist anywhere.
export function isObjectURL(rawUrl: string): boolean {
  return /^blob:/i.test(rawUrl);
}
