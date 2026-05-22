"use client";

/**
 * HtmlAttachmentPreview — inline HTML attachment renderer.
 *
 * Visual model mirrors the image renderer: the iframe body is the card, and a
 * floating right-top toolbar reveals on hover with Preview (full-screen modal)
 * / Open-in-new-tab / Download. No file-card chrome (icon + filename row).
 *
 * No "Copy code" button: this is a FILE, not an inline source snippet. The
 * inline ```html``` fenced block (HtmlBlockPreview) is the surface for reading
 * / copying HTML source; an attachment's contract is view + download.
 *
 * Open-in-new-tab routes to `/{slug}/attachments/{id}/preview` — desktop uses
 * `openInNewTab` to add an app tab; web falls back to `window.open` against
 * the shareable URL.
 *
 * Mounted by the unified `<Attachment>` dispatcher when the attachment is
 * HTML and an `attachmentId` is resolvable (the /content proxy is ID-keyed).
 * For other kinds, `<Attachment>` falls back to the shared AttachmentCard.
 *
 * Failure mode (413 / 415 / transport): we do not unmount the figure or fall
 * back to AttachmentCard chrome — standalone attachment lists filter URLs
 * already inlined in the markdown body, so a silent unmount would remove the
 * user's only Preview/Download entry point. Instead the body collapses to an
 * 80px placeholder and the toolbar pins itself open with all actions enabled.
 */

import { Download, ExternalLink, Maximize2 } from "lucide-react";
import { cn } from "@multica/ui/lib/utils";
import { paths, useWorkspaceSlug } from "@multica/core/paths";
import { useT } from "../i18n";
import { useNavigation } from "../navigation";
import { useAttachmentHtmlText } from "./hooks/use-attachment-html-text";
import { HtmlPreviewBody } from "./html-preview-body";

const PREVIEW_HEIGHT = "h-[480px]";
const ERROR_PLACEHOLDER_HEIGHT = "h-20";

interface HtmlAttachmentPreviewProps {
  attachmentId: string;
  filename: string;
  onPreview: () => void;
  onDownload: () => void;
}

export function HtmlAttachmentPreview({
  attachmentId,
  filename,
  onPreview,
  onDownload,
}: HtmlAttachmentPreviewProps) {
  const { t } = useT("editor");
  // Subscribe to the same React Query cache key the body consumes so the
  // toolbar can pin itself open during error. Re-subscribing is free — the
  // useQuery dedupe means no extra fetch.
  const query = useAttachmentHtmlText(attachmentId);
  const isError = !query.isLoading && (!!query.error || !query.data?.text);
  // useWorkspaceSlug — NOT useWorkspacePaths. The Paths-bound variant throws
  // when there's no slug; we want to render gracefully (just hide the
  // new-tab button) when the component is somehow mounted outside a
  // workspace route.
  const slug = useWorkspaceSlug();
  const navigation = useNavigation();

  // Only enable the new-tab button when the workspace slug is resolvable —
  // outside a workspace context the path is meaningless. Prefer desktop's
  // tab system; on web fall back to window.open against the public shareable
  // URL (auth is handled by the cookie session on the new page).
  const canOpenInNewTab = !!slug && !!attachmentId;
  const handleOpenInNewTab = () => {
    if (!slug) return;
    const nameQuery = filename ? `?name=${encodeURIComponent(filename)}` : "";
    const path = `${paths.workspace(slug).attachmentPreview(attachmentId)}${nameQuery}`;
    if (navigation.openInNewTab) {
      navigation.openInNewTab(path, filename, { activate: true });
      return;
    }
    const url = navigation.getShareableUrl(path);
    window.open(url, "_blank", "noopener,noreferrer");
  };

  return (
    <div
      className="group/html-preview relative my-1"
      onMouseDown={(e) => e.stopPropagation()}
    >
      <HtmlPreviewBody
        source={{ kind: "attachment", attachmentId }}
        title={filename}
        className={PREVIEW_HEIGHT}
        placeholderClassName={isError ? ERROR_PLACEHOLDER_HEIGHT : PREVIEW_HEIGHT}
        errorTestId="html-attachment-preview-error"
      />
      <div
        className={cn(
          "absolute right-2 top-2 flex items-center gap-0.5 rounded-md border border-border bg-background/95 p-0.5 shadow-sm transition-opacity",
          // Error state pins the toolbar open — Preview / Download are the
          // only user-reachable escape hatches when inline render fails.
          isError
            ? "opacity-100"
            : "opacity-0 group-hover/html-preview:opacity-100",
        )}
      >
        <button
          type="button"
          className="flex h-6 w-6 items-center justify-center rounded text-muted-foreground transition-colors hover:bg-muted hover:text-foreground"
          title={t(($) => $.attachment.preview)}
          aria-label={t(($) => $.attachment.preview)}
          onMouseDown={(e) => {
            e.preventDefault();
            e.stopPropagation();
            onPreview();
          }}
        >
          <Maximize2 className="h-3.5 w-3.5" />
        </button>
        {canOpenInNewTab && (
          <button
            type="button"
            className="flex h-6 w-6 items-center justify-center rounded text-muted-foreground transition-colors hover:bg-muted hover:text-foreground"
            title={t(($) => $.attachment.open_in_new_tab)}
            aria-label={t(($) => $.attachment.open_in_new_tab)}
            onMouseDown={(e) => {
              e.preventDefault();
              e.stopPropagation();
              handleOpenInNewTab();
            }}
          >
            <ExternalLink className="h-3.5 w-3.5" />
          </button>
        )}
        <button
          type="button"
          className="flex h-6 w-6 items-center justify-center rounded text-muted-foreground transition-colors hover:bg-muted hover:text-foreground"
          title={t(($) => $.image.download)}
          aria-label={t(($) => $.image.download)}
          onMouseDown={(e) => {
            e.preventDefault();
            e.stopPropagation();
            onDownload();
          }}
        >
          <Download className="h-3.5 w-3.5" />
        </button>
      </div>
    </div>
  );
}
