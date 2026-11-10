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
  attachmentDownloadPath,
  attachmentIdFromDownloadURL,
  attachmentIdFromLocalUploadURL,
} from "@multica/core/types/attachment-url";

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
// CloudFront and presign URLs can load natively. Proxy capabilities last just
// 60 seconds, so images deliberately ignore those and fetch durable bytes
// through the authenticated client instead. Proxy is the default when `auto`
// mode encounters an internal storage host, such as docker-compose MinIO
// (`http://minio:9000`). Without a storage signature, pull authenticated bytes
// and render an object URL. `blobFallback` gates that on rendering a native
// `<img>`: a file card only needs a link, and downloading a 100 MB archive
// into renderer memory to draw a chip would be a bad trade.
//
// `pending` is true while that upgrade is still in flight — `url` is then the
// picked URL, which this client may be unable to load. A caller that treats a
// load failure as meaningful (the viewer skips broken images) waits it out
// instead of rendering it.
export function useResignedInlineMedia(
  attachmentId: string | undefined,
  rawPickedUrl: string,
  blobFallback: boolean,
): { url: string; pending: boolean } {
  const pickedUrl = resolveLegacyLocalMediaURL(rawPickedUrl);
  const idFromPickedUrl = attachmentIdFromDownloadURL(pickedUrl);
  const resignAttachmentId = attachmentId ?? idFromPickedUrl;
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
  const needsResign =
    !!resignAttachmentId &&
    !!pickedUrl &&
    idFromPickedUrl !== undefined &&
    ((api.getBaseUrl?.() ?? "") !== "" || isCrossOriginWebURL);

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

// useFreshStreamedMediaURL resolves the URL the viewer streams a video, audio
// file or PDF from. Unlike an inline image, these are never loaded before the
// viewer opens, so whatever URL the attachment record carries is used for the
// first time here — and records live in caches that never go stale, while the
// signed `download_url` in them expires after 30 minutes. Handing that to
// `<video>` on a page that has been open a while plays nothing (MUL-8892).
//
// So for these kinds the viewer always asks the authenticated metadata
// endpoint for a fresh signature, the same exchange the Download button makes.
// Proxy capabilities last only 60 seconds. Keep them in a separate query so
// image metadata caching cannot pin one into a native stream after it expires.
// Reopen always authenticates again; an open viewer renews before expiry.
export function useFreshStreamedMediaURL(
  attachmentId: string | undefined,
  rawPickedUrl: string,
  enabled: boolean,
): { url: string; pending: boolean; refreshIfExpired: () => Promise<boolean> } {
  const pickedUrl = resolveLegacyLocalMediaURL(rawPickedUrl);
  const id = attachmentId ?? attachmentIdFromDownloadURL(pickedUrl);
  const active = enabled && !!id && !!pickedUrl;
  const { data: fresh, isError, isStale, isFetching, refetch } = useQuery({
    queryKey: ["attachment-stream-resign", id],
    queryFn: () => api.getAttachment(id as string),
    enabled: active,
    staleTime: RESIGN_STALE_MS,
    gcTime: 0,
    retry: false,
    refetchOnMount: "always",
    refetchOnWindowFocus: "always",
    refetchInterval: (query) => {
      if (query.state.status === "error") return false;
      const expiresAt = capabilityExpiresAt(query.state.data?.download_url ?? "");
      return expiresAt === undefined
        ? false
        : Math.max(1000, expiresAt - Date.now() - 5000);
    },
  });

  const dl = fresh?.download_url ?? "";
  const expiresAt = capabilityExpiresAt(dl);
  const refreshIfExpired = async () => {
    if (!active || expiresAt === undefined || expiresAt > Date.now() + 5000) return false;
    const result = await refetch();
    return !result.isError && !!result.data?.download_url && result.data.download_url !== dl;
  };
  if (!active || isError) return { url: pickedUrl, pending: false, refreshIfExpired };
  if (expiresAt !== undefined) {
    if (expiresAt <= Date.now() && !isFetching) return { url: pickedUrl, pending: false, refreshIfExpired };
    return { url: absolutizeAPIPath(dl), pending: false, refreshIfExpired };
  }
  // A cached answer past its stale time may hold a signature that has
  // already expired: wait for the refetch it triggers rather than start a
  // stream on it and restart once the new URL lands.
  if (fresh && !(isStale && isFetching)) {
    const dl = fresh.download_url ?? "";
    const signed =
      /^https?:\/\//i.test(dl) && attachmentIdFromDownloadURL(dl) === undefined;
    return { url: signed ? dl : pickedUrl, pending: false, refreshIfExpired };
  }
  return { url: pickedUrl, pending: true, refreshIfExpired };
}

function absolutizeAPIPath(url: string): string {
  if (!url.startsWith("/") || url.startsWith("//")) return url;
  return `${(api.getBaseUrl?.() ?? "").replace(/\/+$/, "")}${url}`;
}

function resolveLegacyLocalMediaURL(url: string): string {
  const id = attachmentIdFromLocalUploadURL(url);
  return id ? absolutizeAPIPath(attachmentDownloadPath(id)) : url;
}

function capabilityExpiresAt(url: string): number | undefined {
  // Only the server's per-attachment capability has the short proxy TTL.
  try {
    const parsed = new URL(url, "https://attachment.invalid");
    if (!/^\/api\/attachments\/[^/]+\/signed-download$/.test(parsed.pathname)) return undefined;
    if (!parsed.searchParams.get("sig")) return undefined;
    const exp = Number(parsed.searchParams.get("exp"));
    return Number.isFinite(exp) && exp > 0 ? exp * 1000 : undefined;
  } catch {
    return undefined;
  }
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
