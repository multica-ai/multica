"use client";

import { Download, FileText, Eye } from "lucide-react";
import { cn } from "@multica/ui/lib/utils";
import type { Attachment } from "@multica/core/types";
import { isViewableAttachment } from "@multica/cerebro-attachments/core/viewable";
import { useWorkspacePaths } from "@multica/core/paths";
import { useNavigation } from "../../navigation";

// Renders attachments that are NOT already referenced inline in the markdown
// content. Used both for issue bodies and for individual comments.
//
// Click behavior:
// - Viewable filetypes (HTML, Markdown, text, JSON) → open the in-app
//   attachment viewer in a new tab so the issue context isn't lost.
// - Other filetypes → fall back to opening the download URL in a new tab,
//   matching the original behavior.
//
// Filtering rules:
// - Skip an attachment whose URL appears in `content`.
// - Skip duplicate uploads of the same file (same name/type/size) when a
//   sibling with the same identity is already inline.
//
// When `content` is undefined, all attachments are rendered.
export function AttachmentList({
  attachments,
  content,
  className,
}: {
  attachments?: Attachment[];
  content?: string;
  className?: string;
}) {
  const wsPaths = useWorkspacePaths();
  const router = useNavigation();
  if (!attachments?.length) return null;
  const standalone = content
    ? attachments.filter((a) => {
        if (content.includes(a.url)) return false;
        const hasSiblingInContent = attachments.some(
          (other) =>
            other.id !== a.id &&
            other.filename === a.filename &&
            other.content_type === a.content_type &&
            other.size_bytes === a.size_bytes &&
            content.includes(other.url),
        );
        if (hasSiblingInContent) return false;
        return true;
      })
    : attachments;
  if (!standalone.length) return null;

  const openViewer = (id: string, filename: string) => {
    const path = wsPaths.attachmentView(id);
    if (router.openInNewTab) {
      router.openInNewTab(path, filename);
    } else if (router.getShareableUrl) {
      window.open(router.getShareableUrl(path), "_blank", "noopener,noreferrer");
    } else {
      window.open(path, "_blank", "noopener,noreferrer");
    }
  };

  return (
    <div className={cn("flex flex-col gap-1", className)}>
      {standalone.map((a) => {
        const viewable = isViewableAttachment(a.content_type, a.filename);
        const handleOpen = () => {
          if (viewable) {
            openViewer(a.id, a.filename);
          } else if (a.download_url) {
            window.open(a.download_url, "_blank", "noopener,noreferrer");
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
                onClick={() =>
                  window.open(a.download_url, "_blank", "noopener,noreferrer")
                }
              >
                <Download className="size-3.5" />
              </button>
            )}
          </div>
        );
      })}
    </div>
  );
}
