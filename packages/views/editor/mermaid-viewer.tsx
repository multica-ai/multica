"use client";

/**
 * MermaidViewer — full-screen diagram viewer.
 *
 * Built on the shared `Dialog` (Base UI) rather than a bespoke portal, so it
 * inherits the behaviors the old Mermaid lightbox was missing: a real backdrop,
 * focus trap, background scroll lock, focus restore, and Escape-to-close.
 *
 * The diagram stays inside an `sandbox=""` iframe — the isolation boundary is
 * not relaxed to buy interactivity. Instead the iframe is `pointer-events:
 * none` and pan/zoom is applied from this document as a transform on the
 * wrapper (see `useDiagramCanvas`). A side effect worth keeping: because the
 * iframe never takes focus, key events cannot get stranded in a document the
 * dialog can't hear, which is why Escape used to stop working after a click.
 */

import { useCallback, useMemo, useRef, useState } from "react";
import {
  Check,
  Code as CodeIcon,
  Copy,
  Download,
  Frame,
  Image as ImageIcon,
  Maximize,
  Minus,
  Plus,
  RotateCcw,
  X,
} from "lucide-react";
import { cn } from "@multica/ui/lib/utils";
import { copyText } from "@multica/ui/lib/clipboard";
import { Dialog, DialogContent, DialogTitle } from "@multica/ui/components/ui/dialog";
import {
  DropdownMenu,
  DropdownMenuContent,
  DropdownMenuItem,
  DropdownMenuTrigger,
} from "@multica/ui/components/ui/dropdown-menu";
import { useT } from "../i18n";
import { CodeBlockStatic } from "./code-block-static";
import { useDiagramCanvas } from "./hooks/use-diagram-canvas";
import {
  buildExportSvg,
  diagramFilenameStem,
  downloadBlob,
  renderSvgToPngBlob,
} from "./utils/mermaid-export";
import type { Size } from "./utils/diagram-transform";

const COPY_FEEDBACK_MS = 2000;

interface MermaidViewerContentProps {
  /** Mermaid source, for the source view and `.mmd` export. */
  chart: string;
  /** Rendered SVG markup, for export. */
  svg: string | null;
  /** Sandboxed document that draws the diagram at natural size. */
  viewerDocument: string | null;
  /** Natural size of the diagram. */
  layout: Size;
  /** Colors resolved from the host theme, so exports match what is on screen. */
  exportBackground: string;
  exportFontFamily: string;
}

export interface MermaidViewerProps extends MermaidViewerContentProps {
  open: boolean;
  onOpenChange: (open: boolean) => void;
  /** Focus returns here on close — the button that opened the viewer. */
  finalFocusRef?: React.RefObject<HTMLElement | null>;
}

function ToolbarButton({
  onClick,
  disabled,
  label,
  children,
}: {
  onClick: () => void;
  disabled?: boolean;
  label: string;
  children: React.ReactNode;
}) {
  return (
    <button
      type="button"
      onClick={onClick}
      disabled={disabled}
      title={label}
      aria-label={label}
      className="flex size-7 items-center justify-center rounded-md text-muted-foreground transition-colors hover:bg-secondary hover:text-foreground disabled:pointer-events-none disabled:opacity-40"
    >
      {children}
    </button>
  );
}

export function MermaidViewer({
  open,
  onOpenChange,
  finalFocusRef,
  ...content
}: MermaidViewerProps) {
  const { t } = useT("editor");
  const canvasRef = useRef<HTMLDivElement>(null);

  return (
    <Dialog open={open} onOpenChange={onOpenChange}>
      <DialogContent
        showCloseButton={false}
        initialFocus={canvasRef}
        finalFocus={finalFocusRef}
        // Dynamic viewport units: on mobile Safari and in narrow Electron
        // panes `vh` includes retracted browser chrome, which would push the
        // toolbar off-screen — the exact "can't find the close button"
        // failure this viewer exists to remove.
        className="!max-w-[calc(100vw-2rem)] !h-[min(90dvh,calc(100dvh-2rem))] !w-[calc(100vw-2rem)] flex flex-col gap-0 overflow-hidden p-0 xl:!max-w-[80rem]"
        aria-label={t(($) => $.mermaid.viewer_title)}
      >
        {/* Body state (zoom, pan, source toggle) deliberately lives below the
            portal boundary so it is destroyed on close. Each open is a fresh
            read that re-fits, rather than restoring a stale zoom from the last
            time this diagram happened to be opened. */}
        <MermaidViewerContent
          {...content}
          canvasRef={canvasRef}
          onClose={() => onOpenChange(false)}
        />
      </DialogContent>
    </Dialog>
  );
}

function MermaidViewerContent({
  chart,
  svg,
  viewerDocument,
  layout,
  exportBackground,
  exportFontFamily,
  canvasRef,
  onClose,
}: MermaidViewerContentProps & {
  canvasRef: React.RefObject<HTMLDivElement | null>;
  onClose: () => void;
}) {
  const { t } = useT("editor");
  const [showSource, setShowSource] = useState(false);
  const [copied, setCopied] = useState(false);
  const canvas = useDiagramCanvas({ content: layout });

  const handleCopySource = useCallback(async () => {
    if (await copyText(chart)) {
      setCopied(true);
      setTimeout(() => setCopied(false), COPY_FEEDBACK_MS);
    }
  }, [chart]);

  const standaloneSvg = useMemo(() => {
    if (!svg) return null;
    return buildExportSvg(svg, {
      background: exportBackground,
      fontFamily: exportFontFamily,
      width: layout.width,
      height: layout.height,
    });
  }, [svg, layout, exportBackground, exportFontFamily]);

  const filenameStem = useMemo(() => diagramFilenameStem(chart), [chart]);

  const handleDownloadSource = useCallback(() => {
    downloadBlob(
      new Blob([chart], { type: "text/vnd.mermaid;charset=utf-8" }),
      `${filenameStem}.mmd`,
    );
  }, [chart, filenameStem]);

  const handleDownloadSvg = useCallback(() => {
    if (!standaloneSvg) return;
    downloadBlob(
      new Blob([standaloneSvg], { type: "image/svg+xml;charset=utf-8" }),
      `${filenameStem}.svg`,
    );
  }, [standaloneSvg, filenameStem]);

  const handleDownloadPng = useCallback(async () => {
    if (!standaloneSvg) return;
    try {
      const blob = await renderSvgToPngBlob(standaloneSvg, layout);
      downloadBlob(blob, `${filenameStem}.png`);
    } catch {
      // Rasterization is best-effort: `.svg` and `.mmd` remain available, and
      // failing loudly here would put an error dialog over the diagram the
      // user is reading.
    }
  }, [standaloneSvg, layout, filenameStem]);

  const canExportImage = standaloneSvg !== null;

  return (
    <>
      {/* Sibling of the scrolling canvas, never inside it: the toolbar must
          stay put no matter how far the diagram is panned or zoomed. */}
      <header className="flex shrink-0 items-center gap-1 border-b border-border bg-muted/30 px-3 py-2">
          <DialogTitle className="mr-1 truncate text-sm font-medium">
            {t(($) => $.mermaid.viewer_title)}
          </DialogTitle>

          <div className="flex items-center gap-0.5">
            <ToolbarButton
              onClick={canvas.zoomOut}
              disabled={!canvas.canZoomOut || showSource}
              label={t(($) => $.mermaid.zoom_out)}
            >
              <Minus className="size-4" />
            </ToolbarButton>
            {/* Fixed width so the toolbar doesn't jitter as digits change. */}
            <span
              className="w-12 select-none text-center text-xs tabular-nums text-muted-foreground"
              aria-live="polite"
            >
              {canvas.zoomPercent}%
            </span>
            <ToolbarButton
              onClick={canvas.zoomIn}
              disabled={!canvas.canZoomIn || showSource}
              label={t(($) => $.mermaid.zoom_in)}
            >
              <Plus className="size-4" />
            </ToolbarButton>
            <ToolbarButton
              onClick={canvas.fit}
              disabled={showSource}
              label={t(($) => $.mermaid.zoom_fit)}
            >
              <Frame className="size-4" />
            </ToolbarButton>
            <ToolbarButton
              onClick={canvas.zoomToActualSize}
              disabled={showSource}
              label={t(($) => $.mermaid.zoom_actual)}
            >
              <Maximize className="size-4" />
            </ToolbarButton>
            <ToolbarButton
              onClick={canvas.reset}
              disabled={showSource || canvas.isFitted}
              label={t(($) => $.mermaid.reset_view)}
            >
              <RotateCcw className="size-4" />
            </ToolbarButton>
          </div>

          <div className="ml-auto flex items-center gap-0.5">
            <ToolbarButton
              onClick={() => setShowSource((value) => !value)}
              label={
                showSource
                  ? t(($) => $.mermaid.show_diagram)
                  : t(($) => $.mermaid.show_source)
              }
            >
              <CodeIcon className={cn("size-4", showSource && "text-foreground")} />
            </ToolbarButton>
            <ToolbarButton
              onClick={handleCopySource}
              label={t(($) => $.mermaid.copy_source)}
            >
              {copied ? <Check className="size-4" /> : <Copy className="size-4" />}
            </ToolbarButton>
            <DropdownMenu>
              <DropdownMenuTrigger
                render={
                  <button
                    type="button"
                    title={t(($) => $.mermaid.export)}
                    aria-label={t(($) => $.mermaid.export)}
                    className="flex size-7 items-center justify-center rounded-md text-muted-foreground transition-colors hover:bg-secondary hover:text-foreground"
                  >
                    <Download className="size-4" />
                  </button>
                }
              />
              <DropdownMenuContent align="end">
                <DropdownMenuItem onClick={handleDownloadPng} disabled={!canExportImage}>
                  <ImageIcon className="size-4" />
                  {t(($) => $.mermaid.export_png)}
                </DropdownMenuItem>
                <DropdownMenuItem onClick={handleDownloadSvg} disabled={!canExportImage}>
                  <ImageIcon className="size-4" />
                  {t(($) => $.mermaid.export_svg)}
                </DropdownMenuItem>
                <DropdownMenuItem onClick={handleDownloadSource}>
                  <CodeIcon className="size-4" />
                  {t(($) => $.mermaid.export_source)}
                </DropdownMenuItem>
              </DropdownMenuContent>
            </DropdownMenu>
            <ToolbarButton
              onClick={onClose}
              label={t(($) => $.mermaid.close_viewer)}
            >
              <X className="size-4" />
            </ToolbarButton>
          </div>
        </header>

        {showSource ? (
          <div className="min-h-0 flex-1 overflow-auto bg-background p-3">
            <CodeBlockStatic language="mermaid" body={chart} />
          </div>
        ) : (
          <div
            ref={(node) => {
              canvasRef.current = node;
              canvas.setViewportNode(node);
            }}
            className={cn("mermaid-viewer-canvas", canvas.isPanning && "is-panning")}
            // `application` tells screen readers to pass arrow keys through to
            // the canvas instead of using them for their own navigation.
            role="application"
            aria-label={t(($) => $.mermaid.canvas_label)}
            tabIndex={0}
            onPointerDown={canvas.handlePointerDown}
            onPointerMove={canvas.handlePointerMove}
            onPointerUp={canvas.handlePointerUp}
            onPointerCancel={canvas.handlePointerUp}
            onKeyDown={canvas.handleKeyDown}
          >
            {viewerDocument && (
              <div
                className={cn(
                  "mermaid-viewer-content",
                  canvas.isAnimated && "is-animated",
                )}
                style={{
                  width: `${layout.width}px`,
                  height: `${layout.height}px`,
                  transform: `translate(${canvas.transform.x}px, ${canvas.transform.y}px) scale(${canvas.transform.scale})`,
                }}
              >
                <iframe
                  className="mermaid-viewer-frame"
                  sandbox=""
                  srcDoc={viewerDocument}
                  title={t(($) => $.mermaid.viewer_title)}
                  style={{ width: `${layout.width}px`, height: `${layout.height}px` }}
                />
              </div>
            )}
          </div>
        )}
    </>
  );
}
