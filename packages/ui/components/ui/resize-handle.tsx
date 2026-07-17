"use client"

import { cva, type VariantProps } from "class-variance-authority"

import { cn } from "@multica/ui/lib/utils"
import {
  useResizeGesture,
  type ResizeAxis,
  type ResizeDelta,
  type ResizeEndMode,
} from "@multica/ui/hooks/use-resize-gesture"

// Every resize affordance in the product is described here — the cursor, the
// grab zone and the three indicator states — so that the hand-written handles
// and the react-resizable-panels wrapper cannot drift apart. If a value below
// is duplicated anywhere else, that is the bug.
//
// `ew-resize`/`ns-resize` (not `col-resize`/`row-resize`) because that is what
// react-resizable-panels renders on Chromium and Firefox — every Multica
// runtime. Hand-written handles have to match those, not the Safari-only
// fallback branch.
const resizeHandleVariants = cva("touch-none select-none", {
  variants: {
    axis: { x: "", y: "", xy: "" },
    // A handle whose library already drives the cursor globally opts out:
    // react-resizable-panels injects `*, *:hover { cursor: … !important }` for
    // the whole document, and fighting it would only cause flicker.
    cursor: { axis: "", none: "" },
    // `rule` is a permanent divider between two panels that darkens on hover;
    // `line` is invisible at rest and only appears on hover.
    indicator: { none: "", line: "", rule: "" },
    // `self`: the element is the grab zone. `overlay`: a zero-width host whose
    // grab zone is a centred pseudo-element, so it costs no layout space.
    hitArea: { none: "", self: "", overlay: "" },
  },
  compoundVariants: [
    { axis: "x", cursor: "axis", class: "cursor-ew-resize" },
    { axis: "y", cursor: "axis", class: "cursor-ns-resize" },
    { axis: "xy", cursor: "axis", class: "cursor-nwse-resize" },

    // Indicator states. `data-resizing` is set by useResizeGesture and
    // `data-separator` by react-resizable-panels; only ever one of them
    // matches, so a single declaration serves both kinds of handle.
    {
      indicator: ["line", "rule"],
      class:
        "after:absolute after:transition-colors hover:after:bg-foreground/15 data-[resizing=true]:after:bg-foreground/25 data-[separator=active]:after:bg-foreground/25",
    },
    { indicator: "line", class: "after:bg-transparent" },
    { indicator: "rule", class: "after:bg-border" },

    // The rule runs along the boundary the axis moves across.
    {
      axis: "x",
      indicator: ["line", "rule"],
      class: "after:inset-y-0 after:start-1/2 after:w-px after:-translate-x-1/2",
    },
    {
      axis: "y",
      indicator: ["line", "rule"],
      class: "after:inset-x-0 after:top-1/2 after:h-px after:-translate-y-1/2",
    },
    // A corner has no single boundary to draw a line along; the cursor is the
    // whole affordance.
    { axis: "xy", indicator: ["line", "rule"], class: "after:hidden" },

    // One grab zone size for the whole product: 8px, centred on the boundary.
    { axis: "x", hitArea: "self", class: "w-2" },
    { axis: "y", hitArea: "self", class: "h-2" },
    {
      axis: "x",
      hitArea: "overlay",
      class:
        "before:absolute before:inset-y-0 before:left-1/2 before:w-2 before:-translate-x-1/2",
    },
    {
      axis: "y",
      hitArea: "overlay",
      class:
        "before:absolute before:inset-x-0 before:top-1/2 before:h-2 before:-translate-y-1/2",
    },
  ],
  defaultVariants: {
    axis: "x",
    cursor: "axis",
    indicator: "line",
    hitArea: "none",
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
  hitArea = "self",
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
      className={cn(
        resizeHandleVariants({ axis, indicator, hitArea }),
        className
      )}
      {...props}
    />
  )
}

export { ResizeHandle, resizeHandleVariants }
