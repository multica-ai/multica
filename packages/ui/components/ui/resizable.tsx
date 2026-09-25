"use client"

import * as ResizablePrimitive from "react-resizable-panels"

import { cn } from "@multica/ui/lib/utils"

function ResizablePanelGroup({
  className,
  ...props
}: ResizablePrimitive.GroupProps) {
  return (
    <ResizablePrimitive.Group
      data-slot="resizable-panel-group"
      // The library otherwise injects `*, *:hover { cursor: … !important }`
      // while dragging, and narrows it to a one-way arrow the moment a panel
      // hits a bound — so the cursor flips between two icons mid-drag. It is
      // truthful, but it reads as a glitch. The handle carries `cursor-col-resize`
      // instead, which stays put.
      disableCursor
      className={cn(
        "flex h-full w-full aria-[orientation=vertical]:flex-col",
        // A drag captures the pointer, which then travels across the panels —
        // away from the handle that carries the cursor. `:has` puts the same
        // cursor on the whole group for as long as a separator is active, which
        // is what the library's global rule was doing before `disableCursor`
        // turned it off. Without this the cursor reverts to a text caret the
        // moment the pointer leaves the 8px handle.
        "[&:has([data-separator=active])_*]:cursor-col-resize [&:has([data-separator=active])]:cursor-col-resize",
        "aria-[orientation=vertical]:[&:has([data-separator=active])_*]:cursor-row-resize aria-[orientation=vertical]:[&:has([data-separator=active])]:cursor-row-resize",
        className
      )}
      {...props}
    />
  )
}

function ResizablePanel({ ...props }: ResizablePrimitive.PanelProps) {
  return <ResizablePrimitive.Panel data-slot="resizable-panel" {...props} />
}

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
        // The only cursor source: the group opts out of the library's own
        // (see ResizablePanelGroup). One icon for hover and for the whole drag.
        "cursor-col-resize aria-[orientation=horizontal]:cursor-row-resize",
        "relative flex w-0 items-center justify-center before:absolute before:inset-y-0 before:left-1/2 before:w-px before:-translate-x-1/2 before:bg-transparent before:transition-colors hover:before:bg-foreground/15 data-[separator=active]:before:bg-foreground/15 after:absolute after:inset-y-0 after:left-1/2 after:w-2 after:-translate-x-1/2 focus-visible:outline-hidden aria-[orientation=horizontal]:h-0 aria-[orientation=horizontal]:w-full aria-[orientation=horizontal]:before:inset-x-0 aria-[orientation=horizontal]:before:inset-y-auto aria-[orientation=horizontal]:before:h-px aria-[orientation=horizontal]:before:w-full aria-[orientation=horizontal]:before:translate-x-0 aria-[orientation=horizontal]:after:left-0 aria-[orientation=horizontal]:after:h-2 aria-[orientation=horizontal]:after:w-full aria-[orientation=horizontal]:after:translate-x-0 aria-[orientation=horizontal]:after:-translate-y-1/2",
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
