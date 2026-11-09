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
import {
  attachmentIdFromDownloadURL,
  attachmentIdFromLocalUploadURL,
} from "@multica/core/types/attachment-url";
import { resolvePublicFileUrl } from "@multica/core/workspace/avatar-url";

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
export const INLINE_BLOB_GC_MS = 5 * 60 * 1000;

// Module-level cache for blob: URLs keyed by attachment id. Without this,
// useObjectURL creates a new object URL on every mount (state "" -> effect
// creates URL), so a cached blob still flashes the fallback pickedUrl for one
// frame on re-entry — the flicker reported in #7741 for image-heavy issues.
const blobUrlCache = new Map<string, string>();
const blobUrlRefCount = new Map<string, number>();
const blobUrlGCTimers = new Map<string, ReturnType<typeof setTimeout>>();

export function __resetInlineMediaBlobCacheForTests(): void {
  for (const t of blobUrlGCTimers.values()) clearTimeout(t);
  blobUrlGCTimers.clear();
  blobUrlRefCount.clear();
  blobUrlCache.clear();
}

// authenticatedLoadTarget returns the attachment id to load through the
// authenticated API when `pickedUrl` is an auth-gated path this client cannot
// load natively, and undefined when the URL can be used as it is.
function authenticatedLoadTarget(
  attachmentId: string | undefined,
  pickedUrl: string,
): string | undefined {
  // A legacy local-disk /uploads link is just as auth-gated (MUL-8859), and
  // may name an attachment that is not in this surface's list.
  const idFromPickedUrl =
    attachmentIdFromDownloadURL(pickedUrl) ?? attachmentIdFromLocalUploadURL(pickedUrl);
  if (!pickedUrl || idFromPickedUrl === undefined) return undefined;
  const isCrossOriginWebURL = (() => {
    if (!/^https?:\/\//i.test(pickedUrl) || typeof window === "undefined") {
      return false;
    }
    try {
      return new URL(pickedUrl).origin !== window.location.origin;
    } catch (_err) {
      void _err;
      return false;
    }
  })();
  if ((api.getBaseUrl?.() ?? "") === "" && !isCrossOriginWebURL) return undefined;
  return attachmentId ?? idFromPickedUrl;
}

// useNativeMediaURL gives an <iframe>, <video> or <audio> preview a URL it can
// load without credentials. Like the inline-image case below, an auth-gated
// URL 401s there; unlike it, the bytes are not worth pulling into renderer
// memory, and a player keeps issuing range requests for as long as it plays.
// So fetch the attachment's metadata each time the preview mounts and use its
// signed `download_url` — never one held in a cache, which may already have
// expired. Not a query on purpose: a cached entry would outlive the preview
// and hand a reopen the old signature.
export function useNativeMediaURL(
  attachmentId: string | undefined,
  pickedUrl: string,
  enabled: boolean,
): { url: string; pending: boolean } {
  const target = enabled ? authenticatedLoadTarget(attachmentId, pickedUrl) : undefined;
  const [signed, setSigned] = useState<{ target: string; url: string } | null>(null);

  useEffect(() => {
    if (target === undefined) return;
    let cancelled = false;
    const settle = (url: string) => {
      if (!cancelled) setSigned({ target, url });
    };
    api.getAttachment(target).then(
      (fresh) => {
        const dl = fresh.download_url ?? "";
        settle(dl && attachmentIdFromDownloadURL(dl) === undefined ? (resolvePublicFileUrl(dl) ?? dl) : "");
      },
      () => settle(""),
    );
    return () => {
      cancelled = true;
    };
  }, [target]);

  if (target === undefined) return { url: pickedUrl, pending: false };
  if (signed?.target !== target) return { url: pickedUrl, pending: true };
  return { url: signed.url || pickedUrl, pending: false };
}

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
//
// `pending` is true while that upgrade is still in flight — `url` is then the
// picked URL, which this client may be unable to load. A caller that treats a
// load failure as meaningful (the viewer skips broken images) waits it out
// instead of rendering it.
export function useResignedInlineMedia(
  attachmentId: string | undefined,
  pickedUrl: string,
  blobFallback: boolean,
): { url: string; pending: boolean } {
  const resignAttachmentId = authenticatedLoadTarget(attachmentId, pickedUrl);
  const needsResign = resignAttachmentId !== undefined;

  const { data: fresh, isError: freshFailed } = useQuery({
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
  const { data: blob, isError: blobFailed } = useQuery({
    queryKey: ["attachment-inline-blob", resignAttachmentId],
    queryFn: () => api.getAttachmentBlob(resignAttachmentId as string),
    enabled: needsResign && blobFallback && !!fresh && signedUrl === "",
    staleTime: Infinity,
    gcTime: INLINE_BLOB_GC_MS,
  });
  const isAuthenticated = !!fresh;
  const blobUrl = useObjectURL(resignAttachmentId, blob, isAuthenticated);

  if (!needsResign) return { url: pickedUrl, pending: false };
  if (signedUrl) return { url: signedUrl, pending: false };
  // Only return the cached blob URL when it belongs to the current id and
  // the current QueryClient has authenticated metadata for it. This keeps
  // the 5-min re-entry cache for the same session but prevents a logged-out
  // or account-switched QueryClient (fresh pending/rejected) from reusing a
  // previous account's private blob: URL without coupling to logout code.
  if (
    blobUrl &&
    resignAttachmentId &&
    isAuthenticated &&
    blobUrlCache.get(resignAttachmentId) === blobUrl
  ) {
    return { url: blobUrl, pending: false };
  }
  if (blobUrl && !resignAttachmentId && isAuthenticated) {
    return { url: blobUrl, pending: false };
  }
  // Still upgrading while the metadata is in flight, and — for a caller that
  // renders the bytes — while they are. A failure at either step ends the
  // wait on the picked URL.
  const pending = !freshFailed && (!fresh || (blobFallback && !blobFailed));
  return { url: pickedUrl, pending };
}

// useObjectURL turns a Blob into a `blob:` URL. The module cache keeps the
// URL stable across remounts so a re-entry with a cached blob does not flash
// the fallback pickedUrl for one frame (see #7741). Refcount + delayed GC
// ensures bytes are released 5 min after last unmount (matching
// INLINE_BLOB_GC_MS) without leaking until renderer exit.
function useObjectURL(
  id: string | undefined,
  blob: Blob | undefined,
  isAuthenticated: boolean,
): string {
  const [, bump] = useState(0);

  // Refcount: cancel pending GC on re-enter, schedule revoke+delete after
  // INLINE_BLOB_GC_MS when last consumer unmounts. Only participate when the
  // current QueryClient has authenticated metadata — otherwise a logged-out
  // client would cancel the previous account's GC and extend the lifetime of
  // private bytes.
  useEffect(() => {
    if (!id || !isAuthenticated) return;
    const pending = blobUrlGCTimers.get(id);
    if (pending) {
      clearTimeout(pending);
      blobUrlGCTimers.delete(id);
    }
    blobUrlRefCount.set(id, (blobUrlRefCount.get(id) ?? 0) + 1);
    return () => {
      const cur = blobUrlRefCount.get(id) ?? 1;
      const next = cur - 1;
      if (next <= 0) {
        blobUrlRefCount.delete(id);
        const t = setTimeout(() => {
          const cached = blobUrlCache.get(id);
          if (cached) {
            try {
              URL.revokeObjectURL(cached);
            } catch (_err) {
              void _err;
            }
            blobUrlCache.delete(id);
          }
          blobUrlGCTimers.delete(id);
        }, INLINE_BLOB_GC_MS);
        if (typeof (t as unknown as { unref?: () => void }).unref === "function") {
          (t as unknown as { unref: () => void }).unref();
        }
        blobUrlGCTimers.set(id, t);
      } else {
        blobUrlRefCount.set(id, next);
      }
    };
  }, [id, isAuthenticated]);

  useEffect(() => {
    if (!blob || !id || !isAuthenticated || typeof URL.createObjectURL !== "function") return;
    if (blobUrlCache.has(id)) {
      bump((v) => v + 1);
      return;
    }
    const next = URL.createObjectURL(blob);
    blobUrlCache.set(id, next);
    bump((v) => v + 1);
  }, [blob, id, isAuthenticated]);

  if (isAuthenticated && id && blobUrlCache.has(id)) return blobUrlCache.get(id)!;
  return "";
}

// isObjectURL flags a src that only resolves inside this renderer session —
// safe to paint, wrong to expose through Copy Link or persist anywhere.
export function isObjectURL(rawUrl: string): boolean {
  return /^blob:/i.test(rawUrl);
}
