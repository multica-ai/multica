"use client";

import { useCallback } from "react";
import { toast } from "sonner";
import { api } from "@multica/core/api";
import { useT } from "../i18n";

interface DesktopBridge {
  downloadURL?: (u: string) => Promise<void> | void;
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
 * Returns a callback that downloads an attachment by ID through a freshly
 * signed CloudFront URL. The server re-signs `download_url` on every
 * `GET /api/attachments/{id}` call, so this flow sidesteps stale signatures
 * cached in TanStack Query / inlined in markdown.
 *
 * Two execution shapes, picked at call time:
 *
 * - **Web**: open a same-origin `about:blank` tab *synchronously* inside
 *   the click handler — popup blockers (Safari especially) only consider
 *   the gesture frame, not the later async settle. The placeholder tab
 *   keeps the user activation receipt; after the fetch resolves we navigate
 *   it. We can NOT pass `"noopener"` to `window.open` because the HTML
 *   spec (`dom-open` step 17) makes that return `null`, which would leave
 *   us nothing to navigate. We disown the opener manually after the fetch.
 *
 * - **Desktop**: uses `desktopAPI.downloadURL()` which invokes Electron's
 *   native `webContents.downloadURL()`, showing a save dialog and saving
 *   the file directly. This avoids the system browser entirely and fixes
 *   the Linux/Ubuntu issue where HTML files are rendered inline instead
 *   of being downloaded.
 */
export function useDownloadAttachment(): (attachmentId: string) => Promise<void> {
  const { t } = useT("editor");
  return useCallback(
    async (attachmentId: string) => {
      const failed = () => toast.error(t(($) => $.attachment.download_failed));

      if (hasDesktopDownloadBridge()) {
        try {
          const fresh = await api.getAttachment(attachmentId);
          if (!fresh.download_url) {
            failed();
            return;
          }
          const bridge = (
            window as unknown as { desktopAPI?: DesktopBridge }
          ).desktopAPI;
          await bridge!.downloadURL!(fresh.download_url);
        } catch {
          failed();
        }
        return;
      }

      // Web: use a transient <a> tag to trigger the download / open.
      // This avoids the about:blank navigation issue where some browsers
      // silently drop an async location.href assignment on a placeholder
      // window, leaving the user staring at a blank tab.
      try {
        const fresh = await api.getAttachment(attachmentId);
        if (!fresh.download_url) {
          failed();
          return;
        }
        const a = document.createElement("a");
        a.href = fresh.download_url;
        a.download = fresh.filename || "";
        a.target = "_blank";
        a.rel = "noopener noreferrer";
        a.style.display = "none";
        document.body.appendChild(a);
        a.click();
        document.body.removeChild(a);
      } catch {
        failed();
      }
    },
    [t],
  );
}
