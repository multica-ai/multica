"use client";

/**
 * HtmlAttachmentPreview — inline HTML attachment renderer.
 *
 * Visual model mirrors the image renderer: the iframe body is the card, and a
 * floating right-top toolbar reveals on hover with Open / Download / Copy code
 * actions. No file-card chrome (icon + filename row).
 *
 * Mounted by AttachmentBlock when the attachment is HTML and the caller can
 * supply an `attachmentId` (the /content proxy is ID-keyed). For other kinds,
 * AttachmentBlock falls back to the shared AttachmentCard.
 *
 * Failure mode (413 / 415 / transport): we do not unmount the figure or fall
 * back to AttachmentCard chrome — standalone attachment lists filter URLs
 * already inlined in the markdown body, so a silent unmount would remove the
 * user's only Open/Download entry point. Instead the body collapses to an
 * 80px placeholder, the toolbar pins itself open, Open and Download remain
 * enabled, and Copy code is disabled (no text payload available).
 */

import { useState } from "react";
import { Check, Copy, Download, Maximize2 } from "lucide-react";
import { cn } from "@multica/ui/lib/utils";
import { useT } from "../i18n";
import { useAttachmentHtmlText } from "./hooks/use-attachment-html-text";

const PREVIEW_HEIGHT = "h-[480px]";
const ERROR_PLACEHOLDER_HEIGHT = "h-20";

interface HtmlAttachmentPreviewProps {
  attachmentId: string;
  filename: string;
  onPreview: () => void;
  onDownload: () => void;
}

export function HtmlAttachmentPreview({
  attachmentId,
  filename,
  onPreview,
  onDownload,
}: HtmlAttachmentPreviewProps) {
  const { t } = useT("editor");
  const query = useAttachmentHtmlText(attachmentId);
  const [copied, setCopied] = useState(false);

  const text = query.data?.text;
  const isLoading = query.isLoading;
  const isError = !isLoading && (!!query.error || !text);
  const canCopy = !!text;

  const handleCopy = async () => {
    if (!text) return;
    try {
      await navigator.clipboard.writeText(text);
      setCopied(true);
      setTimeout(() => setCopied(false), 2000);
    } catch {
      // Clipboard failures are user-recoverable (try again, or open in modal
      // and use the text view). No toast — keep the toolbar quiet.
    }
  };

  return (
    <div
      className="group/html-preview relative my-1"
      onMouseDown={(e) => e.stopPropagation()}
    >
      {isLoading ? (
        <div
          className={cn(
            "flex items-center justify-center rounded-md border border-border bg-muted/30 text-xs text-muted-foreground",
            PREVIEW_HEIGHT,
          )}
        >
          {t(($) => $.attachment.preview_loading)}
        </div>
      ) : isError ? (
        <div
          className={cn(
            "flex items-center rounded-md border border-border bg-muted/30 px-3 text-xs text-muted-foreground",
            ERROR_PLACEHOLDER_HEIGHT,
          )}
          data-testid="html-attachment-preview-error"
        >
          <span className="truncate">{t(($) => $.attachment.preview_failed)}</span>
        </div>
      ) : (
        <iframe
          srcDoc={text}
          sandbox="allow-scripts"
          title={filename}
          className={cn(
            "block w-full rounded-md border border-border bg-background",
            PREVIEW_HEIGHT,
          )}
        />
      )}
      <div
        className={cn(
          "absolute right-2 top-2 flex items-center gap-0.5 rounded-md border border-border bg-background/95 p-0.5 shadow-sm transition-opacity",
          // Error state pins the toolbar open — Open / Download are the only
          // user-reachable escape hatches when inline render fails.
          isError
            ? "opacity-100"
            : "opacity-0 group-hover/html-preview:opacity-100",
        )}
      >
        <button
          type="button"
          className="flex h-6 w-6 items-center justify-center rounded text-muted-foreground transition-colors hover:bg-muted hover:text-foreground"
          title={t(($) => $.attachment.preview)}
          aria-label={t(($) => $.attachment.preview)}
          onMouseDown={(e) => {
            e.preventDefault();
            e.stopPropagation();
            onPreview();
          }}
        >
          <Maximize2 className="h-3.5 w-3.5" />
        </button>
        <button
          type="button"
          className="flex h-6 w-6 items-center justify-center rounded text-muted-foreground transition-colors hover:bg-muted hover:text-foreground"
          title={t(($) => $.image.download)}
          aria-label={t(($) => $.image.download)}
          onMouseDown={(e) => {
            e.preventDefault();
            e.stopPropagation();
            onDownload();
          }}
        >
          <Download className="h-3.5 w-3.5" />
        </button>
        <button
          type="button"
          className={cn(
            "flex h-6 w-6 items-center justify-center rounded text-muted-foreground transition-colors hover:bg-muted hover:text-foreground",
            !canCopy && "cursor-not-allowed opacity-50 hover:bg-transparent hover:text-muted-foreground",
          )}
          disabled={!canCopy}
          title={t(($) => $.code_block.copy_code)}
          aria-label={t(($) => $.code_block.copy_code)}
          onMouseDown={(e) => {
            e.preventDefault();
            e.stopPropagation();
            if (canCopy) void handleCopy();
          }}
        >
          {copied ? (
            <Check className="h-3.5 w-3.5" />
          ) : (
            <Copy className="h-3.5 w-3.5" />
          )}
        </button>
      </div>
    </div>
  );
}
