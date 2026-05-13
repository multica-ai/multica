"use client";

import { useState } from "react";
import type { MouseEvent, ReactNode } from "react";
import { Eye, Maximize2, Minimize2, XIcon } from "lucide-react";
import { Rnd } from "react-rnd";
import { toast } from "sonner";
import { api } from "@multica/core/api";
import { Button } from "@multica/ui/components/ui/button";
import {
  Dialog,
  DialogClose,
  DialogContent,
  DialogHeader,
  DialogTitle,
} from "@multica/ui/components/ui/dialog";
import { cn } from "@multica/ui/lib/utils";
import { useT } from "../i18n";

export function isMarkdownFilename(filename: string): boolean {
  const normalized = filename.toLowerCase();
  return normalized.endsWith(".md") || normalized.endsWith(".markdown");
}

const DEFAULT_PREVIEW_WIDTH = 896;
const DEFAULT_PREVIEW_HEIGHT = 720;
const MIN_PREVIEW_WIDTH = 420;
const MIN_PREVIEW_HEIGHT = 320;
const PREVIEW_VIEWPORT_MARGIN = 16;

type PreviewBounds = {
  x: number;
  y: number;
  width: number;
  height: number;
};

function getAvailablePreviewSize(): { width: number; height: number } {
  if (typeof window === "undefined") {
    return {
      width: DEFAULT_PREVIEW_WIDTH,
      height: DEFAULT_PREVIEW_HEIGHT,
    };
  }

  return {
    width: Math.max(0, window.innerWidth - PREVIEW_VIEWPORT_MARGIN * 2),
    height: Math.max(0, window.innerHeight - PREVIEW_VIEWPORT_MARGIN * 2),
  };
}

function getDefaultPreviewBounds(): PreviewBounds {
  if (typeof window === "undefined") {
    return {
      x: 0,
      y: 0,
      width: DEFAULT_PREVIEW_WIDTH,
      height: DEFAULT_PREVIEW_HEIGHT,
    };
  }

  const { width: availableWidth, height: availableHeight } = getAvailablePreviewSize();
  const width = Math.min(DEFAULT_PREVIEW_WIDTH, availableWidth);
  const height = Math.min(DEFAULT_PREVIEW_HEIGHT, availableHeight);

  return {
    x: Math.max(PREVIEW_VIEWPORT_MARGIN, (window.innerWidth - width) / 2),
    y: Math.max(PREVIEW_VIEWPORT_MARGIN, (window.innerHeight - height) / 2),
    width,
    height,
  };
}

function getWindowedPreviewMinimumSize(): { minWidth: number; minHeight: number } {
  if (typeof window === "undefined") {
    return {
      minWidth: MIN_PREVIEW_WIDTH,
      minHeight: MIN_PREVIEW_HEIGHT,
    };
  }

  const { width: availableWidth, height: availableHeight } = getAvailablePreviewSize();

  return {
    minWidth: Math.min(MIN_PREVIEW_WIDTH, availableWidth),
    minHeight: Math.min(MIN_PREVIEW_HEIGHT, availableHeight),
  };
}

function getFullscreenPreviewBounds(fallback: PreviewBounds): PreviewBounds {
  if (typeof window === "undefined") return fallback;

  return {
    x: 0,
    y: 0,
    width: window.innerWidth,
    height: window.innerHeight,
  };
}

function DialogCloseButton({ label }: { label: string }) {
  return (
    <DialogClose
      render={
        <Button
          variant="ghost"
          size="icon-sm"
          className="[-webkit-app-region:no-drag]"
        />
      }
    >
      <XIcon />
      <span className="sr-only">{label}</span>
    </DialogClose>
  );
}

function MarkdownPreviewDialogContent({
  filename,
  previewLoading,
  children,
}: {
  filename: string;
  previewLoading: boolean;
  children: ReactNode;
}) {
  const { t } = useT("editor");
  const [fullscreen, setFullscreen] = useState(false);
  const [windowBounds, setWindowBounds] = useState(getDefaultPreviewBounds);
  const bounds = fullscreen ? getFullscreenPreviewBounds(windowBounds) : windowBounds;
  const minimumSize = getWindowedPreviewMinimumSize();
  const fullscreenLabel = fullscreen
    ? t(($) => $.file_card.exit_full_screen)
    : t(($) => $.file_card.enter_full_screen);

  return (
    <DialogContent
      showCloseButton={false}
      overlayClassName="bg-black/80 supports-backdrop-filter:backdrop-blur-xs"
      className="!fixed !top-0 !left-0 h-screen w-screen max-w-none !translate-x-0 !translate-y-0 border-0 bg-transparent p-0 shadow-none ring-0"
    >
      <Rnd
        key={fullscreen ? "fullscreen" : "windowed"}
        bounds="window"
        size={{ width: bounds.width, height: bounds.height }}
        position={{ x: bounds.x, y: bounds.y }}
        minWidth={minimumSize.minWidth}
        minHeight={minimumSize.minHeight}
        disableDragging={fullscreen}
        enableResizing={!fullscreen}
        dragHandleClassName="markdown-preview-drag-handle"
        onDragStop={(_event, data) => {
          setWindowBounds((current) => ({
            ...current,
            x: data.x,
            y: data.y,
          }));
        }}
        onResizeStop={(_event, _direction, ref, _delta, position) => {
          setWindowBounds({
            x: position.x,
            y: position.y,
            width: ref.offsetWidth,
            height: ref.offsetHeight,
          });
        }}
        data-testid="markdown-preview-shell"
        data-fullscreen={fullscreen ? "true" : "false"}
        className="overflow-hidden rounded-xl bg-popover text-sm text-popover-foreground shadow-lg ring-1 ring-foreground/10"
      >
        <div className="grid h-full grid-rows-[auto_minmax(0,1fr)] gap-3 p-4">
          <DialogHeader
            data-testid="markdown-preview-drag-handle"
            className="markdown-preview-drag-handle min-w-0 cursor-move pr-20"
          >
            <DialogTitle className="truncate">{filename}</DialogTitle>
          </DialogHeader>
          <div className="absolute top-2 right-2 flex items-center gap-1">
            <Button
              type="button"
              variant="ghost"
              size="icon-sm"
              className="[-webkit-app-region:no-drag]"
              aria-label={fullscreenLabel}
              title={fullscreenLabel}
              onClick={() => setFullscreen((value) => !value)}
            >
              {fullscreen ? <Minimize2 /> : <Maximize2 />}
            </Button>
            <DialogCloseButton label={t(($) => $.file_card.close_preview)} />
          </div>
          <div
            data-testid="markdown-preview-scroll"
            className="min-h-0 overflow-y-auto rounded-md border border-border bg-background p-4"
          >
            {previewLoading ? (
              <p className="text-sm text-muted-foreground">{t(($) => $.file_card.preview_loading)}</p>
            ) : (
              children
            )}
          </div>
        </div>
      </Rnd>
    </DialogContent>
  );
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
      <Dialog
        open={previewOpen}
        onOpenChange={(open) => {
          setPreviewOpen(open);
        }}
      >
        {previewOpen ? (
          <MarkdownPreviewDialogContent filename={filename} previewLoading={previewLoading}>
            {renderContent(previewContent ?? "")}
          </MarkdownPreviewDialogContent>
        ) : null}
      </Dialog>
    </>
  );
}
