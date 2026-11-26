"use client";

import type { ReactNode } from "react";
import { toast } from "sonner";
import { isGlobalPath, isKnownWorkspacePath } from "@multica/core/paths";
import {
  isLocalFileHref,
  type LocalFileReadRequest,
  type LocalFileReadResult,
} from "@multica/core/paths/local-file-link";
import { useCurrentIssueRenderContext } from "../issues/current-issue-render-context";
import { useT } from "../i18n";
import { useAttachmentPreview } from "./attachment-preview-modal";

interface LocalFileAPI {
  readLocalFile?: (request: LocalFileReadRequest) => Promise<LocalFileReadResult>;
}

interface LocalFileLinkHandle {
  open: (href: string) => boolean;
  modal: ReactNode;
}

/** Intercept local document links before any browser or workspace navigation. */
export function useOpenLocalFileLink(): LocalFileLinkHandle {
  const issue = useCurrentIssueRenderContext();
  const { t } = useT("editor");
  const preview = useAttachmentPreview();

  const open = (href: string): boolean => {
    if (!isLocalFileHref(href) || isGlobalPath(href) || isKnownWorkspacePath(href)) {
      return false;
    }
    const api = (window as unknown as { desktopAPI?: LocalFileAPI }).desktopAPI;
    if (!api?.readLocalFile) {
      toast.error(t(($) => $.local_file.desktop_required));
      return true;
    }
    if (!issue?.workspaceId || !issue.projectId) {
      toast.error(t(($) => $.local_file.project_required));
      return true;
    }

    void api.readLocalFile({
      workspaceId: issue.workspaceId,
      projectId: issue.projectId,
      href,
    }).then((result) => {
      if (result.ok) {
        preview.open({
          kind: "local",
          filename: result.filename,
          text: result.text,
          contentType: result.contentType,
          sizeBytes: result.sizeBytes,
        });
        return;
      }
      switch (result.reason) {
        case "not_found":
          toast.error(t(($) => $.local_file.not_found));
          break;
        case "outside_project":
          toast.error(t(($) => $.local_file.outside_project));
          break;
        case "unsupported_type":
          toast.error(t(($) => $.local_file.unsupported_type));
          break;
        case "too_large":
          toast.error(t(($) => $.local_file.too_large));
          break;
        case "unavailable":
          toast.error(t(($) => $.local_file.unavailable));
          break;
        default:
          toast.error(t(($) => $.local_file.failed));
      }
    }).catch(() => toast.error(t(($) => $.local_file.failed)));
    return true;
  };

  return { open, modal: preview.modal };
}
