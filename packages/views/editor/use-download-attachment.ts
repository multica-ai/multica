"use client";

import { useCallback } from "react";
import { toast } from "sonner";
import { api } from "@multica/core/api";
import { useWorkspaceSlug } from "@multica/core/paths";
import { resolvePublicFileUrl } from "@multica/core/workspace/avatar-url";
import { useT } from "../i18n";

interface DesktopBridge {
  downloadURL?: (u: string) => Promise<void> | void;
}

function attachmentDownloadEndpoint(
  attachmentId: string,
  workspaceSlug: string,
): string {
  const params = new URLSearchParams({ workspace_slug: workspaceSlug });
  const path = `/api/attachments/${encodeURIComponent(attachmentId)}/download`;
  const endpoint = `${path}?${params.toString()}`;
  return resolvePublicFileUrl(endpoint) ?? endpoint;
}

// Resolve the forced-attachment ("download button") URL the server minted, if
// any. Unlike `download_url` (load-intent — media serves inline for preview),
// `attachment_download_url` carries Content-Disposition: attachment in every
// storage mode, so it downloads rather than previews. resolvePublicFileUrl keeps
// a site-relative proxy capability same-origin and passes absolute presign /
// CloudFront URLs through unchanged. Empty on a server that predates the field.
function forcedAttachmentUrl(url: string | undefined): string | null {
  if (!url) return null;
  return resolvePublicFileUrl(url);
}

// The capability download URL the server minted, if any.
//
// In proxy mode the backend puts a signed, single-attachment capability into
// `download_url` (a site-relative `/api/attachments/{id}/signed-download?...`
// URL, added in #6092). It carries its own credential in the query string, so a
// top-level `<a download>` navigation authenticates on its own — no
// `Authorization` header, no session cookie — which is what clears the 401 that
// otherwise makes the browser save the error body as `download.txt`. That 401
// is the token-mode case: auth is a bearer token held in JS, so a bare
// navigation to the cookie-gated slug endpoint sends no credential at all.
//
// The capability does NOT force an attachment disposition — it streams through
// `proxyAttachmentDownload` → `storage.ContentDisposition`, which returns
// `inline` for image/video/audio/PDF. What makes the browser download rather
// than preview is the request staying same-origin combined with the `download`
// attribute. This is the #6713 fallback for a server that has the #6092
// capability but not yet `attachment_download_url`; the forced-attachment URL
// preferred above closes the cross-origin-media gap #6712 tracked.
//
// Absolute CloudFront / S3 `download_url`s are deliberately NOT used here: they
// are cross-origin and inline-tuned, so an `<a download>` to them previews the
// file instead of downloading it. Those modes keep entering through the unified
// slug endpoint, and a server that predates #6092 returns no capability at all.
function capabilityDownloadUrl(downloadUrl: string | undefined): string | null {
  if (!downloadUrl) return null;
  if (!downloadUrl.startsWith("/api/attachments/")) return null;
  if (!downloadUrl.includes("/signed-download")) return null;
  return resolvePublicFileUrl(downloadUrl);
}

function triggerBrowserDownload(url: string): void {
  const anchor = document.createElement("a");
  anchor.href = url;
  // Keep the click in the current browsing context. For same-origin API
  // downloads this hint lets Chromium/Safari use Content-Disposition's
  // filename without opening a blank tab. If the endpoint later 302s to
  // CloudFront/S3, the server signs that redirect with an attachment
  // disposition; the browser follows it natively without buffering the file
  // into JS memory.
  anchor.download = "";
  anchor.rel = "noopener";
  anchor.style.display = "none";
  document.body.appendChild(anchor);
  anchor.click();
  anchor.remove();
}

// Detected at call time, not module load — the bridge is injected by the
// Electron preload after `window` exists, and reading it lazily lets the
// same hook work in both renderers without a build-time fork.
function hasDesktopDownloadBridge(): boolean {
  if (typeof window === "undefined") return false;
  const bridge = (window as unknown as { desktopAPI?: DesktopBridge }).desktopAPI;
  return Boolean(bridge?.downloadURL);
}

/**
 * Returns a callback that downloads an attachment by ID. The Web path uses
 * the unified server endpoint directly instead of opening a blank tab or
 * materializing the file as a Blob in renderer memory.
 *
 * Two execution shapes, picked at call time:
 *
 * - **Web**: refreshes attachment metadata (for the existing error-feedback
 *   path and to read the download URLs), then clicks a temporary anchor. It
 *   prefers `attachment_download_url` — the credential-free URL that forces
 *   Content-Disposition: attachment in every storage mode — then the #6092
 *   proxy capability, then the cookie-gated
 *   `/api/attachments/{id}/download?workspace_slug=...` endpoint for a server
 *   that predates both. Either way the backend owns CloudFront / S3 presign /
 *   proxy selection, so large files stay in the browser's native download
 *   pipeline.
 *
 * - **Desktop**: uses `desktopAPI.downloadURL()` which invokes Electron's
 *   native `webContents.downloadURL()`, showing a save dialog and saving
 *   the file directly. This avoids the system browser entirely and fixes
 *   the Linux/Ubuntu issue where HTML files are rendered inline instead
 *   of being downloaded.
 */
export function useDownloadAttachment(): (attachmentId: string) => Promise<void> {
  const { t } = useT("editor");
  const workspaceSlug = useWorkspaceSlug();
  return useCallback(
    async (attachmentId: string) => {
      const failed = () => toast.error(t(($) => $.attachment.download_failed));

      if (hasDesktopDownloadBridge()) {
        try {
          const fresh = await api.getAttachment(attachmentId);
          // Prefer the forced-attachment URL (Content-Disposition: attachment in
          // every storage mode) so the native save writes the file rather than
          // opening media inline; fall back to the load-intent `download_url` for
          // a server that predates the field. Either may be server-relative (the
          // proxy capability) or absolute (CloudFront / S3 presigned); Electron's
          // main-side `downloadURLSafely` requires `new URL()` to parse to
          // http/https, so resolve against the configured API base before we
          // cross the bridge. Absolute URLs pass through unchanged.
          const downloadUrl = resolvePublicFileUrl(
            fresh.attachment_download_url || fresh.download_url,
          );
          if (!downloadUrl) {
            failed();
            return;
          }
          const bridge = (
            window as unknown as { desktopAPI?: DesktopBridge }
          ).desktopAPI;
          await bridge!.downloadURL!(downloadUrl);
        } catch {
          failed();
        }
        return;
      }

      try {
        // The preflight metadata request is both the permission check (so API
        // failures still produce the existing toast instead of a silent failed
        // navigation) and where the server hands back the download URLs it mints
        // for this response.
        const fresh = await api.getAttachment(attachmentId);
        if (typeof document === "undefined") {
          failed();
          return;
        }
        // Prefer the forced-attachment URL when the server minted one: it is
        // credential-free and carries Content-Disposition: attachment in every
        // storage mode, so a bare `<a download>` navigation both authenticates
        // and downloads — instead of previewing media inline or 401ing in
        // token-mode.
        const forced = forcedAttachmentUrl(fresh.attachment_download_url);
        if (forced) {
          triggerBrowserDownload(forced);
          return;
        }
        // Older server without the forced-attachment field: fall back to the
        // #6092 proxy capability (#6713). A top-level `<a download>` navigation
        // sends no `Authorization` header, so in token-mode the cookie-gated
        // slug endpoint 401s; the capability URL carries its own credential in
        // the query string, so it authenticates that bare navigation itself.
        const capability = capabilityDownloadUrl(fresh.download_url);
        if (capability) {
          triggerBrowserDownload(capability);
          return;
        }
        // No capability (CloudFront / S3 signing, or a server that predates
        // #6092): fall back to the cookie-authenticated unified endpoint, which
        // still works wherever the session cookie applies.
        if (!workspaceSlug) {
          failed();
          return;
        }
        triggerBrowserDownload(
          attachmentDownloadEndpoint(attachmentId, workspaceSlug),
        );
      } catch {
        failed();
      }
    },
    [t, workspaceSlug],
  );
}
