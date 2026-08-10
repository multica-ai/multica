"use client";

// Numbered thumbnail row shown above the composer input (FIR-2693). Each image
// carries its 1-based number (so the user can tell the agent "look at image 2"),
// opens a preview on click, can be embedded inline, and can be removed. Renders
// on every composer surface (chat, comments, channels, DMs) because it lives in
// BaseComposer.

import { ImageOff, ImagePlus, X } from "lucide-react";
import { cn } from "@multica/ui/lib/utils";
import { AttachmentChip } from "@multica/cerebro-ui";
import type { ImageTrayItem } from "./use-image-tray";

interface ComposerImageTrayProps {
  items: ImageTrayItem[];
  onPreview: (item: ImageTrayItem) => void;
  onEmbed: (item: ImageTrayItem) => void;
  onRemove: (localId: string) => void;
  /** Hide the per-image "Embed inline" action. Field editors (description, note,
   *  document) use this: their tray images already live in the saved markdown, so
   *  there is no send-time embed decision to make — only preview + remove. */
  hideEmbed?: boolean;
  embedLabel?: string;
}

export function ComposerImageTray({
  items,
  onPreview,
  onEmbed,
  onRemove,
  hideEmbed = false,
  embedLabel = "Embed",
}: ComposerImageTrayProps) {
  if (items.length === 0) return null;

  return (
    <div
      role="list"
      aria-label="Attached images"
      data-image-tray
      className="mb-1.5 flex min-w-0 max-w-full items-center gap-2 overflow-x-auto pb-1"
    >
      {items.map((item, idx) => {
        const number = idx + 1;
        const failed = item.status === "failed";
        return (
          <div
            key={item.localId}
            role="listitem"
            className="group/tray relative shrink-0 max-md:w-40"
          >
            {failed ? (
              <span className="flex h-14 w-14 items-center justify-center rounded-md border border-destructive/40 bg-destructive/10 text-destructive max-md:mx-auto">
                <ImageOff className="size-5" />
              </span>
            ) : (
              <AttachmentChip
                filename={item.filename}
                thumbnailSrc={item.uploadedUrl || item.blobUrl}
                onActivate={() => onPreview(item)}
                activateLabel={`Preview image ${number}`}
                onRemove={() => onRemove(item.localId)}
                removeButtonClassName="max-md:hidden"
                uploading={item.status === "uploading"}
                className="max-md:mx-auto"
              />
            )}

            {/* 1-based number badge — the reference the user gives the agent. */}
            <span
              aria-hidden
              className="pointer-events-none absolute left-1 top-1 z-10 flex h-4 min-w-4 items-center justify-center rounded-full bg-foreground/80 px-1 text-[10px] font-semibold leading-none text-background"
            >
              {number}
            </span>

            {failed ? (
              <button
                type="button"
                onClick={() => onRemove(item.localId)}
                className="absolute -right-1.5 -top-1.5 z-10 rounded-full border border-border bg-background p-0.5 text-muted-foreground shadow-sm hover:text-foreground max-md:hidden"
                aria-label={`Remove failed image ${number}`}
                title="Upload failed — remove"
              >
                <ImageOff className="size-3" />
              </button>
            ) : hideEmbed ? null : (
              <button
                type="button"
                onClick={() => onEmbed(item)}
                onMouseDown={(e) => e.preventDefault()}
                className={cn(
                  "absolute inset-x-1 bottom-1 z-10 rounded bg-foreground/80 px-1 py-0.5 text-[10px] font-medium leading-none text-background opacity-0 transition-opacity",
                  "hover:bg-foreground focus-visible:opacity-100 group-hover/tray:opacity-100 group-focus-within/tray:opacity-100",
                  "max-md:hidden",
                  item.status === "uploading" && "pointer-events-none",
                )}
                aria-label={
                  embedLabel === "Place in text"
                    ? `Place image ${number} in text`
                    : `Embed image ${number} inline`
                }
                title={embedLabel}
              >
                {embedLabel}
              </button>
            )}

            <div className="mt-1 flex items-center justify-center gap-1 md:hidden">
              {!failed && !hideEmbed && (
                <button
                  type="button"
                  onClick={() => onEmbed(item)}
                  onMouseDown={(event) => event.preventDefault()}
                  disabled={item.status === "uploading"}
                  className="flex size-11 items-center justify-center rounded-md border border-border bg-background text-muted-foreground hover:text-foreground disabled:pointer-events-none disabled:opacity-50 md:hidden"
                  aria-label={
                    embedLabel === "Place in text"
                      ? `Place image ${number} in text`
                      : `Embed image ${number} inline`
                  }
                  title={embedLabel}
                >
                  <ImagePlus className="size-4" />
                </button>
              )}
              <button
                type="button"
                onClick={() => onRemove(item.localId)}
                className="flex size-11 items-center justify-center rounded-md border border-border bg-background text-muted-foreground hover:text-foreground md:hidden"
                aria-label={
                  failed
                    ? `Remove failed image ${number}`
                    : `Remove image ${number}`
                }
                title="Remove image"
              >
                <X className="size-4" />
              </button>
            </div>
          </div>
        );
      })}
    </div>
  );
}
