"use client"

import * as React from "react"
import { Collapsible as CollapsiblePrimitive } from "@base-ui/react/collapsible"

import { cn } from "@multica/ui/lib/utils"

function Collapsible({ ...props }: CollapsiblePrimitive.Root.Props) {
  return <CollapsiblePrimitive.Root data-slot="collapsible" {...props} />
}

function CollapsibleTrigger({ ...props }: CollapsiblePrimitive.Trigger.Props) {
  return (
    <CollapsiblePrimitive.Trigger
      data-slot="collapsible-trigger"
      data-trigger="collapsible"
      {...props}
    />
  )
}

/**
 * Base UI keeps `--collapsible-panel-height` in px while the panel animates and
 * sets it to `auto` once the panel rests open. `data-settled` mirrors that, so
 * the panel clips only while its height moves and rows' focus rings are not
 * cut off at rest.
 */
function observeSettled(panel: HTMLDivElement) {
  const update = () =>
    panel.toggleAttribute(
      "data-settled",
      panel.style.getPropertyValue("--collapsible-panel-height") === "auto"
    )
  update()
  const observer = new MutationObserver(update)
  observer.observe(panel, { attributes: true, attributeFilter: ["style"] })
  return () => observer.disconnect()
}

function CollapsibleContent({
  className,
  ref,
  render,
  ...props
}: CollapsiblePrimitive.Panel.Props) {
  const panelRef = React.useCallback(
    (panel: HTMLDivElement | null) => {
      if (typeof ref === "function") ref(panel)
      else if (ref) ref.current = panel
      if (!panel) return
      const disconnect = observeSettled(panel)
      return () => {
        disconnect()
        if (typeof ref === "function") ref(null)
        else if (ref) ref.current = null
      }
    },
    [ref]
  )
  return (
    <CollapsiblePrimitive.Panel
      ref={panelRef}
      data-slot="collapsible-content"
      className={cn(
        // `flow-root` keeps child margins inside the panel once it stops clipping.
        "flow-root h-(--collapsible-panel-height) overflow-hidden transition-[height,opacity] duration-[180ms] ease-[cubic-bezier(0.23,1,0.32,1)] data-settled:overflow-visible data-starting-style:h-0 data-starting-style:opacity-0 data-ending-style:h-0 data-ending-style:opacity-0 motion-reduce:transition-none",
        className
      )}
      // Rows in a closing panel take no clicks or focus.
      render={
        render ??
        ((panelProps, state) => (
          <div
            {...panelProps}
            inert={panelProps.inert || state.transitionStatus === "ending"}
          />
        ))
      }
      {...props}
    />
  )
}

export { Collapsible, CollapsibleTrigger, CollapsibleContent }
