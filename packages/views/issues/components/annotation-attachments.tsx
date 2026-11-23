"use client";

import type { DraftUpload } from "@multica/core/drafts";
import type { Attachment } from "@multica/core/types";
import { FileText, ImageIcon, Loader2, X } from "@multica/ui/icons";
import { useResignedInlineMedia } from "../../editor/hooks/use-inline-media-url";
import { useT } from "../../i18n";

function ImageThumbnail({ attachment }: { attachment: Attachment }) {
  const source = attachment.markdown_url || attachment.url;
  const { url, pending } = useResignedInlineMedia(attachment.id, source, true);
  return url && !pending ? (
    <img
      src={url}
      alt={attachment.filename}
      draggable={false}
      className="absolute inset-0 size-full rounded-lg object-cover"
    />
  ) : <ImageIcon className="size-4" />;
}

export function AnnotationAttachments({ uploads, onRemove }: {
  uploads: DraftUpload[];
  onRemove: (uploadId: string) => void;
}) {
  const { t } = useT("editor");
  if (uploads.length === 0) return null;

  return (
    <ul className="flex max-h-36 flex-wrap items-start gap-x-2 gap-y-2 overflow-y-auto px-3 py-1.5">
      {uploads.map((upload) => {
        const { clientUploadId, filename, contentType, status } = upload;
        const image = contentType?.startsWith("image/") === true;
        const uploading = status === "uploading";
        const removeLabel = `${t(($) => $.attachment.remove)} (${filename})`;
        return (
          <li key={clientUploadId} title={filename} aria-busy={uploading || undefined}>
            {image ? (
              <div className="relative flex size-12 items-center justify-center rounded-lg border border-border bg-muted text-muted-foreground">
                {status === "uploaded" ? <ImageThumbnail attachment={upload.attachment} /> :
                  <Loader2 className="size-4 animate-spin" aria-label={t(($) => $.upload.uploading_label, { filename })} />}
                <button
                  type="button"
                  aria-label={removeLabel}
                  onClick={() => onRemove(clientUploadId)}
                  className="absolute -right-1 -top-1 flex size-5 items-center justify-center rounded-md border border-border bg-popover text-muted-foreground shadow-sm hover:text-foreground focus-visible:outline-2 focus-visible:outline-ring"
                >
                  <X className="size-3" />
                </button>
              </div>
            ) : (
              <div className="flex h-9 max-w-52 items-center gap-1.5 rounded-lg border border-border bg-muted/50 pl-2 pr-1 text-caption">
                {uploading ? <Loader2 className="size-3.5 shrink-0 animate-spin text-muted-foreground" aria-label={t(($) => $.upload.uploading_label, { filename })} /> :
                  <FileText className="size-3.5 shrink-0 text-muted-foreground" />}
                <span className="truncate">{filename}</span>
                <button
                  type="button"
                  aria-label={removeLabel}
                  onClick={() => onRemove(clientUploadId)}
                  className="flex size-6 shrink-0 items-center justify-center rounded-md text-muted-foreground hover:bg-accent hover:text-foreground focus-visible:outline-2 focus-visible:outline-ring"
                >
                  <X className="size-3.5" />
                </button>
              </div>
            )}
          </li>
        );
      })}
    </ul>
  );
}
