"use client"

import * as React from "react"
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
      // instead, which stays put; during a drag `data-resizing` on <html>
      // (see ResizableHandle) keeps it wherever the pointer travels.
      disableCursor
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

/**
 * Marks <html> with `data-resizing="col" | "row"` while this separator is
 * dragged. A drag captures the pointer, which then travels across the panels,
 * away from the handle that carries the cursor; base.css puts the same cursor
 * on everything for as long as the mark is there. A `:has()` on the group
 * would do the same without script, but it makes every DOM change inside the
 * panels restyle their whole subtree, which live editing does many times a
 * second.
 */
function useResizingMark(element: HTMLDivElement | null) {
  React.useEffect(() => {
    if (!element) return
    const root = element.ownerDocument.documentElement
    let marked = false
    const sync = () => {
      const active = element.getAttribute("data-separator") === "active"
      if (active === marked) return
      marked = active
      if (active) {
        root.dataset.resizing =
          element.getAttribute("aria-orientation") === "horizontal" ? "row" : "col"
      } else {
        delete root.dataset.resizing
      }
    }
    const observer = new MutationObserver(sync)
    observer.observe(element, { attributes: true, attributeFilter: ["data-separator"] })
    sync()
    return () => {
      observer.disconnect()
      if (marked) delete root.dataset.resizing
    }
  }, [element])
}

function ResizableHandle({
  withHandle,
  className,
  elementRef,
  ...props
}: ResizablePrimitive.SeparatorProps & {
  withHandle?: boolean
}) {
  const [element, setElement] = React.useState<HTMLDivElement | null>(null)
  useResizingMark(element)
  const ref = React.useCallback(
    (node: HTMLDivElement | null) => {
      setElement(node)
      if (typeof elementRef === "function") elementRef(node)
      else if (elementRef) elementRef.current = node
    },
    [elementRef]
  )
  return (
    <ResizablePrimitive.Separator
      data-slot="resizable-handle"
      elementRef={ref}
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
