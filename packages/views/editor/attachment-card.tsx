"use client";

/**
 * AttachmentCard — shared file-card row UI (icon + filename + Eye + Download).
 *
 * Rendered for every attachment kind that does not have a richer inline
 * renderer. Three call sites reach it indirectly through AttachmentBlock,
 * which picks the right component per kind:
 *
 *   1. `extensions/file-card.tsx`        — Tiptap NodeView for `!file[name](url)`.
 *   2. `readonly-content.tsx`            — readonly `<div data-type="fileCard">`.
 *   3. `comment-card.tsx` AttachmentList — standalone attachments not inlined.
 *
 * Kind-aware routing (e.g. HTML → inline iframe preview) lives in
 * AttachmentBlock — keep that decision out of this file so this stays a
 * single-purpose row UI.
 */

import { Download, Eye, FileText, Loader2 } from "lucide-react";
import { useT } from "../i18n";
import { getPreviewKind } from "./utils/preview";

interface AttachmentCardChromeProps {
  filename: string;
  uploading?: boolean;
  canPreview: boolean;
  canDownload: boolean;
  onPreview: () => void;
  onDownload: () => void;
}

function AttachmentCardChrome({
  filename,
  uploading,
  canPreview,
  canDownload,
  onPreview,
  onDownload,
}: AttachmentCardChromeProps) {
  const { t } = useT("editor");
  return (
    <div
      className="flex items-center gap-2 rounded-md border border-border bg-muted/50 px-2.5 py-1 transition-colors hover:bg-muted"
      onMouseDown={(e) => e.stopPropagation()}
    >
      {uploading ? (
        <Loader2 className="size-4 shrink-0 animate-spin text-muted-foreground" />
      ) : (
        <FileText className="size-4 shrink-0 text-muted-foreground" />
      )}
      <div className="min-w-0 flex-1">
        <p className="truncate text-sm">
          {uploading
            ? t(($) => $.file_card.uploading, { filename })
            : filename}
        </p>
      </div>
      {!uploading && canPreview && (
        <button
          type="button"
          className="shrink-0 rounded-md p-1 text-muted-foreground transition-colors hover:bg-secondary hover:text-foreground"
          title={t(($) => $.attachment.preview)}
          aria-label={t(($) => $.attachment.preview)}
          onMouseDown={(e) => {
            e.preventDefault();
            e.stopPropagation();
            onPreview();
          }}
        >
          <Eye className="size-3.5" />
        </button>
      )}
      {!uploading && canDownload && (
        <button
          type="button"
          className="shrink-0 rounded-md p-1 text-muted-foreground transition-colors hover:bg-secondary hover:text-foreground"
          title={t(($) => $.image.download)}
          aria-label={t(($) => $.image.download)}
          onMouseDown={(e) => {
            e.preventDefault();
            e.stopPropagation();
            onDownload();
          }}
        >
          <Download className="size-3.5" />
        </button>
      )}
    </div>
  );
}

export interface AttachmentCardProps {
  /** Filename used for icon label and previewable-kind detection. */
  filename: string;
  /** Content type used in addition to filename for previewable-kind detection. */
  contentType?: string;
  /**
   * Attachment id — required when the preview proxy is ID-keyed (text kinds
   * like markdown / html / text). Media kinds (pdf/video/audio) preview from
   * the URL alone.
   */
  attachmentId?: string;
  /** Download URL — used as a non-null sentinel for the download button. */
  href?: string;
  /** True while a synchronous upload is in flight (file-card NodeView only). */
  uploading?: boolean;
  /** Pressed when the Eye button is clicked. */
  onPreview: () => void;
  /** Pressed when the Download button is clicked. */
  onDownload: () => void;
}

export function AttachmentCard({
  filename,
  contentType = "",
  attachmentId,
  href,
  uploading,
  onPreview,
  onDownload,
}: AttachmentCardProps) {
  const kind = filename ? getPreviewKind(contentType, filename) : null;
  // Media kinds (pdf/video/audio) are previewable from a URL alone — the
  // modal renders them as <video>/<audio>/<iframe src=url>. Text kinds
  // (markdown/html/text) need the ID-keyed `/api/attachments/{id}/content`
  // proxy, so they only preview when we have an attachmentId — otherwise
  // the Eye button would call tryOpen, get rejected, and do nothing.
  const isUrlPreviewableKind =
    kind === "pdf" || kind === "video" || kind === "audio";
  const canPreview =
    !!href && kind !== null && (!!attachmentId || isUrlPreviewableKind);

  return (
    <div className="my-1">
      <AttachmentCardChrome
        filename={filename}
        uploading={uploading}
        canPreview={canPreview}
        canDownload={!!href}
        onPreview={onPreview}
        onDownload={onDownload}
      />
    </div>
  );
}
