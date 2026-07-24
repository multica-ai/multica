"use client";

import { AlertCircle, Loader2, X } from "lucide-react";
import { Button } from "@multica/ui/components/ui/button";
import { cn } from "@multica/ui/lib/utils";
import type { DraftUpload } from "@multica/core/drafts";
import { useT } from "../../i18n";

/**
 * Status chips for uploads that are NOT yet a completed attachment (MUL-5181).
 *
 * Completed uploads render through the editor's inline preview / the standalone
 * AttachmentList; this strip covers the states those cannot show:
 *  - `uploading` — a spinner, so a reopened composer proves the upload is still
 *    running (it is owned by the coordinator, not this component).
 *  - `interrupted` — a reload/restart killed an in-flight upload; the bytes are
 *    gone, so the user must re-attach.
 *  - `failed` — the request errored.
 *
 * Interrupted/failed chips are dismissable; there is no retry because the file
 * bytes are not persisted.
 */
export function ComposerUploadChips({
  uploads,
  onRemove,
  className,
}: {
  uploads: DraftUpload[];
  onRemove: (clientUploadId: string) => void;
  className?: string;
}) {
  const { t } = useT("editor");
  const pending = uploads.filter((u) => u.status !== "uploaded");
  if (pending.length === 0) return null;

  return (
    <div className={cn("flex flex-col gap-1", className)}>
      {pending.map((u) => {
        const isUploading = u.status === "uploading";
        const isFailed = u.status === "failed";
        return (
          <div
            key={u.clientUploadId}
            className={cn(
              "flex items-center gap-2 rounded-md border px-2 py-1 text-xs",
              isUploading
                ? "border-border bg-muted/50 text-muted-foreground"
                : "border-destructive/40 bg-destructive/5 text-destructive",
            )}
          >
            {isUploading ? (
              <Loader2 className="h-3.5 w-3.5 shrink-0 animate-spin" aria-hidden />
            ) : (
              <AlertCircle className="h-3.5 w-3.5 shrink-0" aria-hidden />
            )}
            <span className="min-w-0 flex-1 truncate">
              {isUploading
                ? t(($) => $.upload.uploading_label, { filename: u.filename })
                : isFailed
                  ? t(($) => $.upload.failed, {
                      filename: u.filename,
                      reason: u.error ?? "",
                    })
                  : `${t(($) => $.upload.failed_label, { filename: u.filename })} — ${t(($) => $.upload.interrupted)}`}
            </span>
            {!isUploading && (
              <Button
                type="button"
                size="icon-sm"
                variant="ghost"
                className="h-5 w-5 shrink-0"
                aria-label={t(($) => $.upload.remove)}
                onClick={() => onRemove(u.clientUploadId)}
              >
                <X className="h-3 w-3" />
              </Button>
            )}
          </div>
        );
      })}
    </div>
  );
}
