"use client"

import { cva, type VariantProps } from "class-variance-authority"

import { cn } from "@multica/ui/lib/utils"
import {
  useResizeGesture,
  type ResizeAxis,
  type ResizeDelta,
  type ResizeEndMode,
} from "@multica/ui/hooks/use-resize-gesture"

// The single place resize cursors are chosen.
//
// `ew-resize`/`ns-resize` (not `col-resize`/`row-resize`) because that is what
// react-resizable-panels renders on Chromium and Firefox — every Multica
// runtime — and the panel handles it draws are the most common resize surface
// in the product. Hand-written handles have to match those, not the
// Safari-only fallback branch.
//
// The indicator is a pseudo-element on the resize boundary itself, so callers
// keep full control of the hit area without a wrapper element.
const resizeHandleVariants = cva("touch-none select-none", {
  variants: {
    axis: {
      x: "cursor-ew-resize",
      y: "cursor-ns-resize",
      xy: "cursor-nwse-resize",
    },
    indicator: {
      line: "after:absolute after:bg-transparent after:transition-colors hover:after:bg-foreground/15 data-[resizing=true]:after:bg-foreground/25",
      none: "",
    },
  },
  compoundVariants: [
    {
      axis: "x",
      indicator: "line",
      class: "after:inset-y-0 after:start-1/2 after:w-px",
    },
    {
      axis: "y",
      indicator: "line",
      class: "after:inset-x-0 after:top-1/2 after:h-px",
    },
    // A corner has no single boundary to draw a line along; the cursor is the
    // whole affordance.
    { axis: "xy", indicator: "line", class: "after:hidden" },
  ],
  defaultVariants: {
    axis: "x",
    indicator: "line",
  },
})

interface ResizeHandleProps
  extends Omit<React.ComponentProps<"div">, "onPointerDown">,
    VariantProps<typeof resizeHandleVariants> {
  axis: ResizeAxis
  onResize: (delta: ResizeDelta, event: PointerEvent) => void
  onResizeStart?: (event: React.PointerEvent<HTMLElement>) => boolean | void
  onResizeEnd?: (mode: ResizeEndMode) => void
  threshold?: number
  disabled?: boolean
}

// Default shell for a resize handle. Deliberately unopinionated about
// semantics: callers supply their own role/tabIndex/aria, because a table
// column separator wants a focusable `role="separator"` while the chat
// window's edges are `aria-hidden` decorations behind a keyboard-reachable
// expand button.
function ResizeHandle({
  axis,
  indicator,
  onResize,
  onResizeStart,
  onResizeEnd,
  threshold,
  disabled,
  className,
  ...props
}: ResizeHandleProps) {
  const { onPointerDown } = useResizeGesture({
    axis,
    threshold,
    disabled,
    onStart: onResizeStart,
    onMove: onResize,
    onEnd: onResizeEnd,
  })

  return (
    <div
      data-slot="resize-handle"
      onPointerDown={onPointerDown}
      className={cn(resizeHandleVariants({ axis, indicator }), className)}
      {...props}
    />
  )
}

export { ResizeHandle, resizeHandleVariants }
