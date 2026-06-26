"use client";

// CEREBRO-PATCH(attachment-list-cerebro): cerebro modification of upstream file

import { Download, FileText, Eye, X } from "lucide-react";
import { cn } from "@multica/ui/lib/utils";
import type { Attachment } from "@multica/core/types";
import { isViewableAttachment, viewableKind } from "@multica/cerebro-attachments/core/viewable";
import { standaloneAttachments } from "@multica/cerebro-attachments/core/standalone";
import { useFlagValue } from "@multica/cerebro-feature-flags";
import { AttachmentChip } from "@multica/cerebro-ui";
import { useAttachmentActions } from "../use-attachment-actions";

// Renders attachments that are NOT already referenced inline in the markdown
// content. Used both for issue bodies and for individual comments.
//
// Click behavior:
// - Viewable filetypes (HTML, Markdown, text, JSON, images, PDF) → open the
//   in-app attachment viewer in a new tab so the issue context isn't lost.
// - Other filetypes → fall back to opening the download URL in a new tab,
//   matching the original behavior.
//
// Filtering rules:
// - Skip an attachment whose URL appears in `content`.
// - Skip duplicate uploads of the same file (same name/type/size) when a
//   sibling with the same identity is already inline.
//
// When `content` is undefined, all attachments are rendered.
//
// Rendering (FIR-2034): when `cerebro_attachment_chips` is on, each attachment
// renders as the unified chip — image → thumbnail, document → colour-coded
// type card — identical to the composer and every other surface. When off, the
// legacy grey single-icon row is used.
export function AttachmentList({
  attachments,
  content,
  className,
  onRemove,
}: {
  attachments?: Attachment[];
  content?: string;
  className?: string;
  // CEREBRO-PATCH(attachment-list-onremove): edit-time attachment removal (upstream multi-attachment feature).
  onRemove?: (attachmentId: string) => void;
}) {
  const chipsEnabled = useFlagValue("cerebro_attachment_chips");
  const { openViewer, downloadFile } = useAttachmentActions();
  if (!attachments?.length) return null;
  const standalone = standaloneAttachments(attachments, content);
  if (!standalone.length) return null;

  if (chipsEnabled) {
    return (
      <div className={cn("flex flex-wrap items-start gap-2", className)}>
        {standalone.map((a) => {
          const viewable = isViewableAttachment(a.content_type, a.filename);
          const isImage = viewableKind(a.content_type, a.filename) === "image";
          const activate = () => {
            if (viewable) openViewer(a.id, a.filename);
            else if (a.download_url) downloadFile(a.id);
          };
          return (
            <AttachmentChip
              key={a.id}
              filename={a.filename}
              thumbnailSrc={isImage ? a.url : undefined}
              onActivate={activate}
              activateLabel={viewable ? "Open in viewer" : "Download"}
              onRemove={onRemove ? () => onRemove(a.id) : undefined}
            />
          );
        })}
      </div>
    );
  }

  return (
    <div className={cn("flex flex-col gap-1", className)}>
      {standalone.map((a) => {
        const viewable = isViewableAttachment(a.content_type, a.filename);
        const handleOpen = () => {
          if (viewable) {
            openViewer(a.id, a.filename);
          } else if (a.download_url) {
            downloadFile(a.id);
          }
        };
        return (
          <div
            key={a.id}
            className="flex items-center gap-2 rounded-md border border-border bg-muted/50 px-2.5 py-1 transition-colors hover:bg-muted"
          >
            <FileText className="size-4 shrink-0 text-muted-foreground" />
            <button
              type="button"
              onClick={handleOpen}
              className="min-w-0 flex-1 truncate text-left text-sm hover:underline"
              title={viewable ? "Open in viewer" : "Download"}
            >
              {a.filename}
            </button>
            {viewable && (
              <button
                type="button"
                aria-label="Open in viewer"
                title="Open in viewer"
                className="shrink-0 rounded-md p-1 text-muted-foreground transition-colors hover:bg-secondary hover:text-foreground"
                onClick={() => openViewer(a.id, a.filename)}
              >
                <Eye className="size-3.5" />
              </button>
            )}
            {a.download_url && (
              <button
                type="button"
                aria-label="Download"
                title="Download"
                className="shrink-0 rounded-md p-1 text-muted-foreground transition-colors hover:bg-secondary hover:text-foreground"
                onClick={() => downloadFile(a.id)}
              >
                <Download className="size-3.5" />
              </button>
            )}
            {onRemove && (
              <button
                type="button"
                aria-label="Remove attachment"
                title="Remove attachment"
                className="shrink-0 rounded-md p-1 text-muted-foreground transition-colors hover:bg-secondary hover:text-foreground"
                onClick={() => onRemove(a.id)}
              >
                <X className="size-3.5" />
              </button>
            )}
          </div>
        );
      })}
    </div>
  );
}
