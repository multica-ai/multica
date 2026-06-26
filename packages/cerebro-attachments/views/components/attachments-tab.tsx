"use client";

// CEREBRO-PATCH-ZONE: cerebro-only package, no upstream marker needed.
//
// FIR-2034 — the issue-level Attachments tab. The standalone attachments that
// used to pile up as a mixed-size chip grid at the top of an issue move here
// and render as a tidy list of fixed-height rows: small preview, filename +
// type, size, uploader · time, and open/download actions on hover. The page
// top stays clean; this is gated behind `cerebro_attachments_tab`.

import { Download, ExternalLink } from "lucide-react";
import { cn } from "@multica/ui/lib/utils";
import type { Attachment } from "@multica/core/types";
import { isViewableAttachment, viewableKind } from "@multica/cerebro-attachments/core/viewable";
import { standaloneAttachments } from "@multica/cerebro-attachments/core/standalone";
import { formatBytes } from "@multica/cerebro-attachments/core/format-bytes";
import { attachmentDocType, DOC_TYPE_STYLES } from "@multica/cerebro-ui";
import { useActorName } from "@multica/core/workspace/hooks";
import { useTimeAgo } from "@multica/views/i18n";
import { useAttachmentActions } from "../use-attachment-actions";

// Newest first — a stable, dependency-free sort so the list reads like a
// timeline without introducing interactive sorter state (see the UI rules:
// don't add state, or affordances, the design doesn't call for).
function byNewest(a: Attachment, b: Attachment): number {
  return (b.created_at ?? "").localeCompare(a.created_at ?? "");
}

function AttachmentRow({
  attachment,
  uploaderName,
  when,
  onOpen,
  onDownload,
}: {
  attachment: Attachment;
  uploaderName: string;
  when: string;
  onOpen?: () => void;
  onDownload?: () => void;
}) {
  const isImage = viewableKind(attachment.content_type, attachment.filename) === "image";
  const { label, Icon, iconClass, tileClass } = DOC_TYPE_STYLES[
    attachmentDocType(attachment.filename)
  ];
  const primary = onOpen ?? onDownload;

  return (
    <div className="group flex items-center gap-3 px-3 py-2 transition-colors hover:bg-muted/50">
      {/* Preview — image thumbnail or type-coloured icon tile, fixed 40px. */}
      <span className="size-10 shrink-0 overflow-hidden rounded-md border border-border bg-muted/50">
        {isImage ? (
          <img
            src={attachment.url}
            alt={attachment.filename}
            className="size-10 object-cover"
            draggable={false}
          />
        ) : (
          <span className={cn("flex size-full items-center justify-center", tileClass)}>
            <Icon className={cn("size-5", iconClass)} />
          </span>
        )}
      </span>

      {/* Name + type. Filename gets the full row width and truncates. */}
      <button
        type="button"
        onClick={primary}
        title={onOpen ? "Open in viewer" : "Download"}
        className="flex min-w-0 flex-1 flex-col items-start text-left"
      >
        <span className="max-w-full truncate text-sm leading-tight text-foreground group-hover:underline">
          {attachment.filename}
        </span>
        <span className="text-xs leading-tight text-muted-foreground">{label}</span>
      </button>

      {/* Size — tabular so the column lines up. */}
      <span className="shrink-0 text-right text-xs tabular-nums text-muted-foreground">
        {formatBytes(attachment.size_bytes)}
      </span>

      {/* Uploader · time. Hidden on narrow widths to protect the name column. */}
      <span className="hidden w-40 shrink-0 truncate text-xs text-muted-foreground sm:block">
        <span className="text-foreground/70">{uploaderName}</span>
        {when ? ` · ${when}` : ""}
      </span>

      {/* Actions — appear on row hover / keyboard focus. */}
      <span className="flex shrink-0 items-center gap-1 opacity-0 transition-opacity group-hover:opacity-100 group-focus-within:opacity-100">
        {onOpen && (
          <button
            type="button"
            aria-label="Open in viewer"
            title="Open in viewer"
            onClick={onOpen}
            className="rounded-md border border-border bg-background p-1 text-muted-foreground transition-colors hover:text-foreground"
          >
            <ExternalLink className="size-3.5" />
          </button>
        )}
        {onDownload && (
          <button
            type="button"
            aria-label="Download"
            title="Download"
            onClick={onDownload}
            className="rounded-md border border-border bg-background p-1 text-muted-foreground transition-colors hover:text-foreground"
          >
            <Download className="size-3.5" />
          </button>
        )}
      </span>
    </div>
  );
}

export function AttachmentsTab({
  attachments,
  content,
  className,
}: {
  attachments?: Attachment[];
  content?: string;
  className?: string;
}) {
  const { getActorName } = useActorName();
  const timeAgo = useTimeAgo();
  const { openViewer, downloadFile } = useAttachmentActions();

  const files = standaloneAttachments(attachments, content).slice().sort(byNewest);
  if (!files.length) {
    return (
      <p className={cn("py-6 text-center text-sm text-muted-foreground", className)}>
        No attachments yet.
      </p>
    );
  }

  const totalBytes = files.reduce((sum, a) => sum + (a.size_bytes || 0), 0);

  return (
    <div className={className}>
      <div className="px-3 pb-2 text-xs text-muted-foreground">
        {files.length} {files.length === 1 ? "file" : "files"} · {formatBytes(totalBytes)}
      </div>
      <div className="divide-y divide-border overflow-hidden rounded-lg border border-border">
        {files.map((a) => {
          const viewable = isViewableAttachment(a.content_type, a.filename);
          return (
            <AttachmentRow
              key={a.id}
              attachment={a}
              uploaderName={getActorName(a.uploader_type, a.uploader_id)}
              when={a.created_at ? timeAgo(a.created_at) : ""}
              onOpen={viewable ? () => openViewer(a.id, a.filename) : undefined}
              onDownload={a.download_url ? () => downloadFile(a.id) : undefined}
            />
          );
        })}
      </div>
    </div>
  );
}

/**
 * Trigger label for the Attachments tab — the word plus a count pill, so the
 * tab advertises how many files live behind it (the page top no longer does).
 * Renders nothing extra when there are zero standalone files.
 */
export function AttachmentsTabLabel({
  attachments,
  content,
}: {
  attachments?: Attachment[];
  content?: string;
}) {
  const count = standaloneAttachments(attachments, content).length;
  return (
    <span className="inline-flex items-center gap-1.5">
      Attachments
      {count > 0 && (
        <span className="rounded-full bg-muted px-1.5 text-[10px] font-semibold leading-4 text-muted-foreground">
          {count}
        </span>
      )}
    </span>
  );
}
