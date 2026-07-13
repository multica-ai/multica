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
 * - **Web**: first refreshes attachment metadata for the existing error
 *   feedback path, then clicks a temporary same-origin
 *   `/api/attachments/{id}/download?workspace_slug=...` anchor. The backend
 *   endpoint owns CloudFront / S3 presign / proxy selection and download
 *   Content-Disposition, so large files stay in the browser's native download
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
          // Server may return a server-relative `download_url`
          // (`/api/attachments/{id}/download`) when no CloudFront
          // signer is configured — the unified download endpoint chooses
          // CloudFront/presign/proxy at request time. Electron's main-side
          // `downloadURLSafely` requires `new URL()` to parse to http/https,
          // so resolve against the configured API base before we cross the
          // bridge. Absolute URLs (legacy CloudFront / S3 presigned) pass
          // through unchanged.
          const downloadUrl = resolvePublicFileUrl(fresh.download_url);
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
        // Keep the preflight metadata request so permission/API failures still
        // produce the existing toast instead of a silent failed navigation. Do
        // not use `download_url` here: in CloudFront mode it may already be a
        // signed CDN URL, while the unified endpoint is the stable browser
        // entry point that chooses cloudfront / presign / proxy server-side.
        await api.getAttachment(attachmentId);
        if (typeof document === "undefined") {
          failed();
          return;
        }
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
