"use client";

/**
 * ZoomCanvas + ZoomControls — the pan/zoom viewport shared by the Mermaid
 * viewer and the image attachment preview.
 *
 * Split into a hook (`useZoomCanvas`, all behaviour) and two presentational
 * pieces because the toolbar and the canvas live in different parents: each
 * surface owns its own header, so it calls the hook, drops `<ZoomControls>`
 * wherever its chrome belongs, and renders `<ZoomCanvas>` as the body. Content
 * comes in as children — this module knows the content's natural size and
 * nothing else about it.
 */

import { useCallback, type ReactNode, type RefObject } from "react";
import { Frame, Maximize, Minus, Plus } from "lucide-react";
import { cn } from "@multica/ui/lib/utils";
import {
  Tooltip,
  TooltipContent,
  TooltipProvider,
  TooltipTrigger,
} from "@multica/ui/components/ui/tooltip";
import { createShortcutChord, type ShortcutChord } from "@multica/core/shortcuts";
import { ShortcutKeycaps } from "../common/shortcut-keycaps";
import { useT } from "../i18n";
import type { ZoomCanvasApi } from "./hooks/use-zoom-canvas";
import type { Size } from "./utils/zoom-transform";
import "./styles/zoom-canvas.css";

const ZOOM_IN_SHORTCUT = createShortcutChord("Plus");
const ZOOM_OUT_SHORTCUT = createShortcutChord("Minus");
const FIT_SHORTCUT = createShortcutChord("0");

function ToolbarButton({
  onClick,
  disabled,
  label,
  shortcut,
  children,
}: {
  onClick: () => void;
  disabled?: boolean;
  label: string;
  shortcut?: ShortcutChord;
  children: ReactNode;
}) {
  return (
    <Tooltip>
      <TooltipTrigger
        render={
          <button
            type="button"
            onClick={onClick}
            disabled={disabled}
            aria-label={label}
            className="flex size-7 items-center justify-center rounded-md text-muted-foreground transition-colors hover:bg-secondary hover:text-foreground disabled:pointer-events-none disabled:opacity-40"
          />
        }
      >
        {children}
      </TooltipTrigger>
      <TooltipContent side="top" sideOffset={8}>
        {label}
        {shortcut && <ShortcutKeycaps shortcut={shortcut} className="ml-1.5" />}
      </TooltipContent>
    </Tooltip>
  );
}

export function ZoomControls({
  canvas,
  disabled,
  className,
}: {
  canvas: ZoomCanvasApi;
  /** Set while the surface shows something other than the canvas (e.g. source view). */
  disabled?: boolean;
  className?: string;
}) {
  const { t } = useT("editor");

  return (
    <TooltipProvider delay={300}>
      <div className={cn("flex items-center gap-0.5", className)}>
        <ToolbarButton
          onClick={canvas.zoomOut}
          disabled={!canvas.canZoomOut || disabled}
          label={t(($) => $.canvas.zoom_out)}
          shortcut={ZOOM_OUT_SHORTCUT}
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
          disabled={!canvas.canZoomIn || disabled}
          label={t(($) => $.canvas.zoom_in)}
          shortcut={ZOOM_IN_SHORTCUT}
        >
          <Plus className="size-4" />
        </ToolbarButton>
        <ToolbarButton
          onClick={canvas.fit}
          disabled={disabled}
          label={t(($) => $.canvas.zoom_fit)}
          shortcut={FIT_SHORTCUT}
        >
          <Frame className="size-4" />
        </ToolbarButton>
        <ToolbarButton
          onClick={canvas.zoomToActualSize}
          disabled={disabled}
          label={t(($) => $.canvas.zoom_actual)}
        >
          <Maximize className="size-4" />
        </ToolbarButton>
      </div>
    </TooltipProvider>
  );
}

export function ZoomCanvas({
  canvas,
  content,
  label,
  className,
  canvasRef,
  autoFocus,
  children,
}: {
  canvas: ZoomCanvasApi;
  /** Natural size of the children, or null while it is still unknown. */
  content: Size | null;
  /** Accessible name — surfaces describe their own content. */
  label: string;
  className?: string;
  canvasRef?: RefObject<HTMLDivElement | null>;
  /**
   * Focus the canvas on mount. Needed on surfaces without a focus-managing
   * Dialog: the keyboard controls are canvas-scoped, so without this the
   * +/-/0 and arrow keys do nothing until the user happens to click first.
   */
  autoFocus?: boolean;
  children: ReactNode;
}) {
  // Stable identity: an inline ref callback is a new function every render, so
  // React would detach (setViewportNode(null)) and reattach on each one.
  const setCanvasNode = useCallback(
    (node: HTMLDivElement | null) => {
      if (canvasRef) canvasRef.current = node;
      canvas.setViewportNode(node);
      if (node && autoFocus) node.focus({ preventScroll: true });
    },
    [canvasRef, canvas.setViewportNode, autoFocus],
  );

  return (
    <div
      ref={setCanvasNode}
      className={cn("zoom-canvas", canvas.isPanning && "is-panning", className)}
      // `application` tells screen readers to pass arrow keys through to the
      // canvas instead of using them for their own navigation.
      role="application"
      aria-label={label}
      tabIndex={0}
      onPointerDown={canvas.handlePointerDown}
      onPointerMove={canvas.handlePointerMove}
      onPointerUp={canvas.handlePointerUp}
      onPointerCancel={canvas.handlePointerUp}
      onDoubleClick={canvas.handleDoubleClick}
      onKeyDown={canvas.handleKeyDown}
    >
      <div
        className={cn(
          content ? "zoom-canvas-content" : "zoom-canvas-fit",
          content && canvas.isAnimated && "is-animated",
        )}
        style={
          content
            ? {
                width: `${content.width}px`,
                height: `${content.height}px`,
                transform: `translate(${canvas.transform.x}px, ${canvas.transform.y}px) scale(${canvas.transform.scale})`,
              }
            : undefined
        }
      >
        {children}
      </div>
    </div>
  );
}
