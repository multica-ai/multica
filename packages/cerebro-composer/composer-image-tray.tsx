"use client";

// Numbered thumbnail row shown above the composer input (FIR-2693). Each image
// carries its 1-based number (so the user can tell the agent "look at image 2"),
// opens a preview on click, can be embedded inline, and can be removed. Renders
// on every composer surface (chat, comments, channels, DMs) because it lives in
// BaseComposer.

import { ImageOff } from "lucide-react";
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
}

export function ComposerImageTray({
  items,
  onPreview,
  onEmbed,
  onRemove,
  hideEmbed = false,
}: ComposerImageTrayProps) {
  if (items.length === 0) return null;

  return (
    <div
      role="list"
      aria-label="Attached images"
      className="mb-1.5 flex items-center gap-2 overflow-x-auto pb-1"
    >
      {items.map((item, idx) => {
        const number = idx + 1;
        const failed = item.status === "failed";
        return (
          <div
            key={item.localId}
            role="listitem"
            className="group/tray relative shrink-0"
          >
            {failed ? (
              <span className="flex h-14 w-14 items-center justify-center rounded-md border border-destructive/40 bg-destructive/10 text-destructive">
                <ImageOff className="size-5" />
              </span>
            ) : (
              <AttachmentChip
                filename={item.filename}
                thumbnailSrc={item.uploadedUrl || item.blobUrl}
                onActivate={() => onPreview(item)}
                activateLabel={`Preview image ${number}`}
                onRemove={() => onRemove(item.localId)}
                uploading={item.status === "uploading"}
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
                className="absolute -right-1.5 -top-1.5 z-10 rounded-full border border-border bg-background p-0.5 text-muted-foreground shadow-sm hover:text-foreground"
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
                  item.status === "uploading" && "pointer-events-none",
                )}
                aria-label={`Embed image ${number} inline`}
                title="Embed inline in the message"
              >
                Embed
              </button>
            )}
          </div>
        );
      })}
    </div>
  );
}
