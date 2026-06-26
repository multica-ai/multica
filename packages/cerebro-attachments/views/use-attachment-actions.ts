"use client";

import { useCallback } from "react";
import { useWorkspacePaths } from "@multica/core/paths";
import { useWorkspaceId } from "@multica/core/hooks";
import { useNavigation } from "@multica/views/navigation";
import { attachmentForceDownloadPath } from "@multica/cerebro-attachments/core/download-url";

/**
 * Shared open/download behavior for an attachment, so the standalone list and
 * the Attachments tab (FIR-2034) wire identical actions:
 * - Viewable filetypes open the in-app viewer in a new tab (issue context kept).
 * - Everything else opens the force-download URL in a new tab.
 */
export function useAttachmentActions() {
  const wsPaths = useWorkspacePaths();
  const wsId = useWorkspaceId();
  const router = useNavigation();

  const openViewer = useCallback(
    (id: string, filename: string) => {
      const path = wsPaths.attachmentView(id);
      if (router.openInNewTab) {
        router.openInNewTab(path, filename);
      } else if (router.getShareableUrl) {
        window.open(router.getShareableUrl(path), "_blank", "noopener,noreferrer");
      } else {
        window.open(path, "_blank", "noopener,noreferrer");
      }
    },
    [wsPaths, router],
  );

  const downloadFile = useCallback(
    (id: string) =>
      window.open(attachmentForceDownloadPath(id, wsId), "_blank", "noopener,noreferrer"),
    [wsId],
  );

  return { openViewer, downloadFile };
}
