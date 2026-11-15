"use client";

/**
 * AttachmentCard — shared file-card row UI (icon + filename + Eye + Download),
 * and AttachmentFileCard — the compact card a list of standalone attachments
 * lays out in a grid (MUL-7649).
 *
 * Subcomponents of the unified `<Attachment>` dispatcher (see attachment.tsx).
 * Rendered for every attachment kind except images, which render inline.
 * Kind-aware routing lives in `<Attachment>` — keep that decision out of this
 * file so these stay single-purpose UI.
 */

import type { ReactNode } from "react";
import { cn } from "@multica/ui/lib/utils";
import { Download, Eye, FileText, Loader2, Trash2 } from "@multica/ui/icons";
import { useT } from "../i18n";
import { DeferredTooltip } from "../common/deferred-tooltip";
import { formatBytes } from "../common/format-bytes";
import { fileIcon } from "./utils/file-icon";
import { canOpenPreview, fileTypeLabel, getPreviewKind } from "./utils/preview";
import { useAttachmentDownloadState } from "./hooks/use-attachment-download-state";

interface AttachmentCardChromeProps {
  filename: string;
  uploading?: boolean;
  canPreview: boolean;
  canDownload: boolean;
  canDelete?: boolean;
  action?: ReactNode;
  onPreview: () => void;
  onDownload: () => void | Promise<unknown>;
  onDelete?: () => void;
}

function AttachmentCardChrome({
  filename,
  uploading,
  canPreview,
  canDownload,
  canDelete,
  action,
  onPreview,
  onDownload,
  onDelete,
}: AttachmentCardChromeProps) {
  const { t } = useT("editor");
  const { downloading, handleDownload } = useAttachmentDownloadState(onDownload);
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
        <p className="truncate text-body">
          {uploading
            ? t(($) => $.file_card.uploading, { filename })
            : filename}
        </p>
      </div>
      {!uploading && action}
      {!uploading && canPreview && (
        <DeferredTooltip
          content={t(($) => $.attachment.preview)}
          trigger={
            <button
              type="button"
              className="shrink-0 rounded-md p-1 text-muted-foreground transition-colors hover:bg-secondary hover:text-foreground"
              aria-label={t(($) => $.attachment.preview)}
              onMouseDown={(e) => {
                e.preventDefault();
                e.stopPropagation();
                onPreview();
              }}
            >
              <Eye className="size-3.5" />
            </button>
          }
        />
      )}
      {!uploading && canDownload && (
        <DeferredTooltip
          content={t(($) => $.image.download)}
          trigger={
            <button
              type="button"
              className="shrink-0 rounded-md p-1 text-muted-foreground transition-colors hover:bg-secondary hover:text-foreground"
              aria-label={t(($) => $.image.download)}
              disabled={downloading}
              aria-busy={downloading}
              onMouseDown={(e) => {
                e.preventDefault();
                e.stopPropagation();
                handleDownload();
              }}
            >
              {downloading ? (
                <Loader2 className="size-3.5 animate-spin" />
              ) : (
                <Download className="size-3.5" />
              )}
            </button>
          }
        />
      )}
      {!uploading && canDelete && onDelete && (
        <DeferredTooltip
          content={t(($) => $.attachment.remove)}
          trigger={
            <button
              type="button"
              className="shrink-0 rounded-md p-1 text-muted-foreground transition-colors hover:bg-destructive/10 hover:text-destructive"
              aria-label={t(($) => $.attachment.remove)}
              onMouseDown={(e) => {
                e.preventDefault();
                e.stopPropagation();
                onDelete();
              }}
            >
              <Trash2 className="size-3.5" />
            </button>
          }
        />
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
  onDownload: () => void | Promise<unknown>;
  /** Optional remove button, used by editable comment/file-card surfaces. */
  onDelete?: () => void;
  /** A labelled button shown before the icons, such as Install theme. */
  action?: ReactNode;
  /** Registers the card with the right-click menu. */
  scopeRef?: (element: HTMLElement | null) => void;
}

export function AttachmentCard({
  filename,
  contentType = "",
  attachmentId,
  href,
  uploading,
  onPreview,
  onDownload,
  onDelete,
  action,
  scopeRef,
}: AttachmentCardProps) {
  const kind = filename ? getPreviewKind(contentType, filename) : null;
  // Without an attachmentId only the URL-renderable kinds open — otherwise
  // the Eye button would call tryOpen, get rejected, and do nothing.
  const canPreview = !!href && canOpenPreview(kind, !!attachmentId);

  return (
    <div ref={scopeRef} className="my-1">
      <AttachmentCardChrome
        filename={filename}
        uploading={uploading}
        canPreview={canPreview}
        canDownload={!!href}
        canDelete={!!onDelete}
        action={action}
        onPreview={onPreview}
        onDownload={onDownload}
        onDelete={onDelete}
      />
    </div>
  );
}

export interface AttachmentFileCardProps {
  filename: string;
  contentType?: string;
  /** 0 or absent hides the size. */
  sizeBytes?: number;
  canPreview: boolean;
  canDownload: boolean;
  uploading?: boolean;
  /** Rendered after the filename, e.g. a version badge. */
  badge?: ReactNode;
  onPreview: () => void;
  onDownload: () => void | Promise<unknown>;
  onDelete?: () => void;
  /** A labelled button that stays visible, such as Install theme. */
  action?: ReactNode;
  /** Registers the card with the right-click menu. */
  scopeRef?: (element: HTMLElement | null) => void;
}

/**
 * A file as a card: type glyph, name, "TYPE · size". The whole card opens the
 * file — the viewer when it can show it, a download otherwise — and download /
 * remove wait on hover so a grid of cards stays quiet.
 */
export function AttachmentFileCard({
  filename,
  contentType = "",
  sizeBytes = 0,
  canPreview,
  canDownload,
  uploading,
  badge,
  onPreview,
  onDownload,
  onDelete,
  action,
  scopeRef,
}: AttachmentFileCardProps) {
  const { t } = useT("editor");
  const { downloading, handleDownload } = useAttachmentDownloadState(onDownload);
  const Icon = fileIcon(contentType, filename);
  const meta = [fileTypeLabel(filename), sizeBytes > 0 ? formatBytes(sizeBytes) : ""]
    .filter(Boolean)
    .join(" · ");
  const open = canPreview ? onPreview : canDownload ? handleDownload : undefined;

  return (
    <div ref={scopeRef} className="group/file-card relative flex min-w-0 items-center gap-3 rounded-lg border border-border bg-card px-3 py-2.5 transition-colors hover:bg-muted/50">
      {/* The card's click target, under the content so text stays
          selectable-looking but every click lands here; the action buttons
          sit above it. */}
      {open && !uploading && (
        <button
          type="button"
          className="absolute inset-0 rounded-lg focus-visible:outline-none focus-visible:ring-2 focus-visible:ring-ring"
          title={filename}
          aria-label={filename}
          onClick={open}
          disabled={!canPreview && downloading}
        />
      )}
      <span className="pointer-events-none flex size-10 shrink-0 items-center justify-center rounded-md bg-muted text-muted-foreground">
        {uploading ? (
          <Loader2 className="size-5 animate-spin" />
        ) : (
          <Icon className="size-5" />
        )}
      </span>
      <span className="pointer-events-none min-w-0 flex-1">
        <span className="flex min-w-0 items-center gap-1.5">
          <span className="truncate text-body font-medium">
            {uploading ? t(($) => $.file_card.uploading, { filename }) : filename}
          </span>
          {badge}
        </span>
        {meta && (
          <span className="block truncate text-caption text-muted-foreground tabular-nums">
            {meta}
          </span>
        )}
      </span>
      {!uploading && action}
      {!uploading && (canDownload || onDelete) && (
        <span className={cn(
          "relative flex shrink-0 items-center gap-0.5 transition-opacity group-hover/file-card:opacity-100 group-focus-within/file-card:opacity-100 [@media(hover:none)]:opacity-100",
          downloading ? "opacity-100" : "opacity-0",
        )}>
          {canDownload && (
            <DeferredTooltip
              content={t(($) => $.image.download)}
              trigger={
                <button
                  type="button"
                  className="rounded-md p-1.5 text-muted-foreground transition-colors hover:bg-secondary hover:text-foreground"
                  aria-label={t(($) => $.image.download)}
                  disabled={downloading}
                  aria-busy={downloading}
                  onClick={handleDownload}
                >
                  {downloading ? (
                    <Loader2 className="size-4 animate-spin" />
                  ) : (
                    <Download className="size-4" />
                  )}
                </button>
              }
            />
          )}
          {onDelete && (
            <DeferredTooltip
              content={t(($) => $.attachment.remove)}
              trigger={
                <button
                  type="button"
                  className="rounded-md p-1.5 text-muted-foreground transition-colors hover:bg-destructive/10 hover:text-destructive"
                  aria-label={t(($) => $.attachment.remove)}
                  onClick={onDelete}
                >
                  <Trash2 className="size-4" />
                </button>
              }
            />
          )}
        </span>
      )}
    </div>
  );
}
