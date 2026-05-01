"use client";

import { Download, FileText } from "lucide-react";
import { cn } from "@multica/ui/lib/utils";
import type { Attachment } from "@multica/core/types";

// Renders attachments that are NOT already referenced inline in the markdown
// content. Used both for issue bodies and for individual comments.
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

  return (
    <div className={cn("flex flex-col gap-1", className)}>
      {standalone.map((a) => (
        <div
          key={a.id}
          className="flex items-center gap-2 rounded-md border border-border bg-muted/50 px-2.5 py-1 transition-colors hover:bg-muted"
        >
          <FileText className="size-4 shrink-0 text-muted-foreground" />
          <div className="min-w-0 flex-1">
            <p className="truncate text-sm">{a.filename}</p>
          </div>
          {a.download_url && (
            <button
              type="button"
              className="shrink-0 rounded-md p-1 text-muted-foreground transition-colors hover:bg-secondary hover:text-foreground"
              onClick={() =>
                window.open(a.download_url, "_blank", "noopener,noreferrer")
              }
            >
              <Download className="size-3.5" />
            </button>
          )}
        </div>
      ))}
    </div>
  );
}
