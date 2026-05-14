"use client";

import { useCallback } from "react";
import { toast } from "sonner";
import { api } from "@multica/core/api";
import { openExternal } from "../platform";
import { useT } from "../i18n";

interface DesktopBridge {
  openExternal?: (u: string) => Promise<void> | void;
}

// Detected at call time, not module load — the bridge is injected by the
// Electron preload after `window` exists, and reading it lazily lets the
// same hook work in both renderers without a build-time fork.
function hasDesktopBridge(): boolean {
  if (typeof window === "undefined") return false;
  const bridge = (window as unknown as { desktopAPI?: DesktopBridge }).desktopAPI;
  return Boolean(bridge?.openExternal);
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
 * - **Desktop**: `window.open` is intercepted by Electron's
 *   `setWindowOpenHandler` and routed through `openExternalSafely`, which
 *   rejects `about:blank`. So on desktop we fetch first, then hand the URL
 *   to `openExternal()` which IPCs into `shell.openExternal` and opens the
 *   system browser.
 */
export function useDownloadAttachment(): (attachmentId: string) => Promise<void> {
  const { t } = useT("editor");
  return useCallback(
    async (attachmentId: string) => {
      const failed = () => toast.error(t(($) => $.attachment.download_failed));

      if (hasDesktopBridge()) {
        try {
          const fresh = await api.getAttachment(attachmentId);
          if (!fresh.download_url) {
            failed();
            return;
          }
          openExternal(fresh.download_url);
        } catch {
          failed();
        }
        return;
      }

      // Web: claim the popup permission synchronously, then hydrate the URL.
      // `window.open` here returns a WindowProxy because we deliberately
      // omit `noopener`; we revoke the back-channel ourselves once we have
      // the real URL.
      const placeholder = typeof window !== "undefined"
        ? window.open("about:blank", "_blank")
        : null;
      try {
        const fresh = await api.getAttachment(attachmentId);
        if (!fresh.download_url) {
          placeholder?.close();
          failed();
          return;
        }
        if (placeholder) {
          placeholder.opener = null;
          placeholder.location.href = fresh.download_url;
        } else if (typeof window !== "undefined") {
          // Popup blocked outright — last-resort navigate the current tab.
          window.location.href = fresh.download_url;
        }
      } catch {
        placeholder?.close();
        failed();
      }
    },
    [t],
  );
}
