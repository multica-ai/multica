"use client"

import * as ResizablePrimitive from "react-resizable-panels"

import { cn } from "@multica/ui/lib/utils"
import { resizeHandleVariants } from "@multica/ui/components/ui/resize-handle"

function ResizablePanelGroup({
  className,
  ...props
}: ResizablePrimitive.GroupProps) {
  return (
    <ResizablePrimitive.Group
      data-slot="resizable-panel-group"
      className={cn(
        "flex h-full w-full aria-[orientation=vertical]:flex-col",
        className
      )}
      {...props}
    />
  )
}

function ResizablePanel({ ...props }: ResizablePrimitive.PanelProps) {
  return <ResizablePrimitive.Panel data-slot="resizable-panel" {...props} />
}

// The separator owns the divider line between two panels: it renders the
// resting rule itself rather than sitting on top of a border the panels draw.
// Two elements painting the same 1px meant the hover/active states were
// invisible — they were tinting a line that was already there. Panels either
// side must not add their own edge border.
//
// Cursor and the drag-time cursor lock stay with the library, which injects
// `*, *:hover { cursor: … !important }` document-wide — hence `cursor: "none"`.
// Everything visual comes from the shared variants, so a panel divider and the
// sidebar rail cannot drift apart.
function ResizableHandle({
  withHandle,
  className,
  ...props
}: ResizablePrimitive.SeparatorProps & {
  withHandle?: boolean
}) {
  return (
    <ResizablePrimitive.Separator
      data-slot="resizable-handle"
      className={cn(
        resizeHandleVariants({
          axis: "x",
          cursor: "none",
          indicator: "rule",
          hitArea: "overlay",
        }),
        "relative flex w-0 items-center justify-center focus-visible:outline-hidden",
        // The library decides orientation at runtime, so the rule and the grab
        // zone are flipped here rather than through the `axis` variant.
        "aria-[orientation=horizontal]:h-0 aria-[orientation=horizontal]:w-full",
        "aria-[orientation=horizontal]:after:inset-x-0 aria-[orientation=horizontal]:after:inset-y-auto aria-[orientation=horizontal]:after:top-1/2 aria-[orientation=horizontal]:after:h-px aria-[orientation=horizontal]:after:w-full aria-[orientation=horizontal]:after:translate-x-0 aria-[orientation=horizontal]:after:-translate-y-1/2",
        "aria-[orientation=horizontal]:before:inset-x-0 aria-[orientation=horizontal]:before:inset-y-auto aria-[orientation=horizontal]:before:top-1/2 aria-[orientation=horizontal]:before:h-2 aria-[orientation=horizontal]:before:w-full aria-[orientation=horizontal]:before:translate-x-0 aria-[orientation=horizontal]:before:-translate-y-1/2",
        className
      )}
      {...props}
    >
      {withHandle && (
        <div className="z-10 flex h-6 w-1 shrink-0 rounded-lg bg-border" />
      )}
    </ResizablePrimitive.Separator>
  )
}

export { ResizableHandle, ResizablePanel, ResizablePanelGroup }
