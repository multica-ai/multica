"use client";

/**
 * AttachmentCard — shared attachment row UI used by every entry point that
 * renders a non-image attachment in the editor surface.
 *
 * Three call sites:
 *   1. `extensions/file-card.tsx` — Tiptap NodeView for `!file[name](url)`
 *      inline in markdown.
 *   2. `readonly-content.tsx` — readonly file-card `<div data-type="fileCard">`
 *      branch, rendered through preprocessMarkdown.
 *   3. `comment-card.tsx` `AttachmentList` — standalone attachments that were
 *      not referenced by URL inside the markdown body.
 *
 * Centralizing this avoids the third-instance trap: every previous attempt to
 * add a feature here had to be added in three places, and dropping one
 * silently re-introduced the bug — MUL-2330's HTML chart was a standalone
 * attachment, so the inline HTML preview only works if THIS path is covered.
 *
 * HTML kind extension:
 *   - When the attachment is HTML and the caller can provide an
 *     `attachmentId` (i.e. the attachment record is known — required for the
 *     ID-keyed `/api/attachments/{id}/content` proxy), the card mounts an
 *     inline `CodeBlockIframe` underneath the row to render the HTML body
 *     directly. Loading errors and 413/415 cases collapse back to the bare
 *     row + Eye/Download buttons.
 *   - For non-HTML kinds (or HTML where we only have a URL), the card looks
 *     and behaves exactly like the previous handwritten rows.
 */

import { Download, Eye, FileText, Loader2 } from "lucide-react";
import { useT } from "../i18n";
import { getPreviewKind } from "./utils/preview";
import { CodeBlockIframe } from "./code-block-iframe";
import { useAttachmentHtmlText } from "./hooks/use-attachment-html-text";

// ---------------------------------------------------------------------------
// Inline HTML preview body
// ---------------------------------------------------------------------------

// Fixed height per the V2 plan; auto-resize via postMessage handshake is
// explicitly out of scope for V1.
const INLINE_HTML_HEIGHT = "h-[480px]";

function InlineHtmlIframe({
  attachmentId,
  filename,
}: {
  attachmentId: string;
  filename: string;
}) {
  const { t } = useT("editor");
  const query = useAttachmentHtmlText(attachmentId);

  if (query.isLoading) {
    return (
      <div className="mt-1 flex h-[480px] items-center justify-center gap-2 rounded-md border border-border bg-muted/30 text-xs text-muted-foreground">
        <Loader2 className="size-3.5 animate-spin" />
        {t(($) => $.attachment.preview_loading)}
      </div>
    );
  }
  // Any error path (413 / 415 / transport) — fall back silently. The
  // surrounding card still offers Eye → modal (which surfaces the typed
  // error) and Download as escape hatches.
  if (query.error || !query.data) return null;

  return (
    <div className="mt-1">
      <CodeBlockIframe
        html={query.data.text}
        title={filename}
        heightClassName={INLINE_HTML_HEIGHT}
      />
    </div>
  );
}

// ---------------------------------------------------------------------------
// Card chrome — icon + filename + optional Eye + Download
// ---------------------------------------------------------------------------

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

// ---------------------------------------------------------------------------
// AttachmentCard — public component
// ---------------------------------------------------------------------------

export interface AttachmentCardProps {
  /** Filename used for icon label and previewable-kind detection. */
  filename: string;
  /** Content type used in addition to filename for previewable-kind detection. */
  contentType?: string;
  /**
   * Attachment id — required for HTML inline rendering (the `/content`
   * proxy is ID-keyed). Undefined means we only have a URL (e.g. a
   * cross-comment `!file[]()` reference) — the card still renders, the
   * HTML iframe just doesn't expand.
   */
  attachmentId?: string;
  /** Download URL — used purely as a non-null sentinel for the download button. */
  href?: string;
  /** True while a synchronous upload is in flight (file-card NodeView only). */
  uploading?: boolean;
  /** Pressed when the Eye button is clicked. */
  onPreview: () => void;
  /** Pressed when the Download button is clicked. */
  onDownload: () => void;
  /**
   * Set to false to disable the HTML inline preview branch (and behave like
   * the legacy chrome-only card). Useful for editor NodeViews while a draft
   * upload is still in flight.
   */
  inlineHtmlEnabled?: boolean;
}

export function AttachmentCard({
  filename,
  contentType = "",
  attachmentId,
  href,
  uploading,
  onPreview,
  onDownload,
  inlineHtmlEnabled = true,
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

  // Mount the inline iframe only when we can hit the /content proxy
  // (attachmentId present) AND kind is HTML AND no upload is in flight.
  const showInlineHtml =
    inlineHtmlEnabled &&
    !uploading &&
    kind === "html" &&
    !!attachmentId;

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
      {showInlineHtml && (
        <InlineHtmlIframe attachmentId={attachmentId!} filename={filename} />
      )}
    </div>
  );
}
