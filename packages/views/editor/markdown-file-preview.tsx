"use client";

import { useState } from "react";
import type { MouseEvent, ReactNode } from "react";
import { Eye } from "lucide-react";
import { toast } from "sonner";
import { api } from "@multica/core/api";
import { Dialog, DialogContent, DialogHeader, DialogTitle } from "@multica/ui/components/ui/dialog";
import { cn } from "@multica/ui/lib/utils";
import { useT } from "../i18n";

export function isMarkdownFilename(filename: string): boolean {
  const normalized = filename.toLowerCase();
  return normalized.endsWith(".md") || normalized.endsWith(".markdown");
}

export function MarkdownFilePreviewButton({
  href,
  filename,
  className,
  onPointerDown,
  renderContent,
}: {
  href: string;
  filename: string;
  className?: string;
  onPointerDown?: (event: MouseEvent<HTMLButtonElement>) => void;
  renderContent: (content: string) => ReactNode;
}) {
  const { t } = useT("editor");
  const [previewOpen, setPreviewOpen] = useState(false);
  const [previewContent, setPreviewContent] = useState<string | null>(null);
  const [previewLoading, setPreviewLoading] = useState(false);

  const openPreview = async () => {
    setPreviewOpen(true);
    if (previewContent !== null) return;
    setPreviewLoading(true);
    try {
      setPreviewContent(await api.previewAttachmentMarkdown(href));
    } catch (error) {
      console.error(error);
      toast.error(t(($) => $.file_card.preview_failed));
      setPreviewOpen(false);
    } finally {
      setPreviewLoading(false);
    }
  };

  return (
    <>
      <button
        type="button"
        className={cn("shrink-0 rounded-md p-1 text-muted-foreground transition-colors hover:bg-secondary hover:text-foreground", className)}
        aria-label={t(($) => $.file_card.preview, { filename })}
        title={t(($) => $.file_card.preview, { filename })}
        onMouseDown={(event) => {
          onPointerDown?.(event);
        }}
        onClick={() => {
          void openPreview();
        }}
      >
        <Eye className="size-3.5" />
      </button>
      <Dialog open={previewOpen} onOpenChange={setPreviewOpen}>
        <DialogContent className="grid h-[min(80vh,720px)] max-h-[min(80vh,720px)] grid-rows-[auto_minmax(0,1fr)] gap-3 overflow-hidden sm:max-w-3xl">
          <DialogHeader>
            <DialogTitle>{filename}</DialogTitle>
          </DialogHeader>
          <div
            data-testid="markdown-preview-scroll"
            className="min-h-0 overflow-y-auto rounded-md border border-border bg-background p-4"
          >
            {previewLoading ? (
              <p className="text-sm text-muted-foreground">{t(($) => $.file_card.preview_loading)}</p>
            ) : (
              renderContent(previewContent ?? "")
            )}
          </div>
        </DialogContent>
      </Dialog>
    </>
  );
}
