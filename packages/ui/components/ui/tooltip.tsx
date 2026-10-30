"use client"

import { Tooltip as TooltipPrimitive } from "@base-ui/react/tooltip"

import { useEffect } from "react"

import { cn } from "@multica/ui/lib/utils"

/** Hover time before a tooltip opens, shared by every tooltip in the app. */
const TOOLTIP_OPEN_DELAY = 500

/**
 * After a tooltip closes, hovering another trigger within this window opens it
 * at once, so moving along a toolbar only waits on the first tooltip.
 */
const TOOLTIP_INSTANT_WINDOW = 400

// Tooltip activity across every tooltip popup, including ones Base UI's delay
// group doesn't see (DeferredTooltip drives a controlled Tooltip with no
// Trigger). Updated by the popup itself as it mounts and unmounts.
let openPopups = 0
let lastPopupClosedAt = Number.NEGATIVE_INFINITY

/** True while a tooltip is showing or just closed, i.e. the next one should skip its delay. */
function isTooltipInstantPhase(): boolean {
  return (
    openPopups > 0 ||
    Date.now() - lastPopupClosedAt < TOOLTIP_INSTANT_WINDOW
  )
}

function TooltipActivity() {
  useEffect(() => {
    openPopups += 1
    return () => {
      openPopups -= 1
      lastPopupClosedAt = Date.now()
    }
  }, [])
  return null
}

function TooltipProvider({
  delay = TOOLTIP_OPEN_DELAY,
  timeout = TOOLTIP_INSTANT_WINDOW,
  ...props
}: TooltipPrimitive.Provider.Props) {
  return (
    <TooltipPrimitive.Provider
      data-slot="tooltip-provider"
      delay={delay}
      timeout={timeout}
      {...props}
    />
  )
}

function Tooltip({ ...props }: TooltipPrimitive.Root.Props) {
  return <TooltipPrimitive.Root data-slot="tooltip" {...props} />
}

function TooltipTrigger({ ...props }: TooltipPrimitive.Trigger.Props) {
  return <TooltipPrimitive.Trigger data-slot="tooltip-trigger" {...props} />
}

function TooltipContent({
  className,
  side = "top",
  sideOffset = 4,
  align = "center",
  alignOffset = 0,
  anchor,
  children,
  ...props
}: TooltipPrimitive.Popup.Props &
  Pick<
    TooltipPrimitive.Positioner.Props,
    "align" | "alignOffset" | "side" | "sideOffset" | "anchor"
  >) {
  // The popup is a `.dark` scope, so the text, status colors, and keycaps
  // inside it take dark-theme values against the dark tooltip surface in both
  // page themes. Its own fill and shadow are resolved on the positioner, which
  // is still in the page theme.
  return (
    <TooltipPrimitive.Portal>
      <TooltipPrimitive.Positioner
        align={align}
        alignOffset={alignOffset}
        side={side}
        sideOffset={sideOffset}
        anchor={anchor}
        className="isolate z-50 [--tooltip-fill:var(--tooltip)] [--tooltip-elevation:var(--tooltip-shadow)]"
      >
        <TooltipPrimitive.Popup
          data-slot="tooltip-content"
          className={cn(
            "dark z-50 inline-flex w-fit max-w-xs origin-(--transform-origin) items-center gap-1.5 rounded-lg bg-(--tooltip-fill) px-2.5 py-1 text-caption text-foreground shadow-(--tooltip-elevation) has-data-[slot=kbd]:pr-1.5 data-[side=bottom]:slide-in-from-top-2 data-[side=inline-end]:slide-in-from-left-2 data-[side=inline-start]:slide-in-from-right-2 data-[side=left]:slide-in-from-right-2 data-[side=right]:slide-in-from-left-2 data-[side=top]:slide-in-from-bottom-2 **:data-[slot=kbd]:relative **:data-[slot=kbd]:isolate **:data-[slot=kbd]:z-50 **:data-[slot=kbd]:rounded-sm data-[state=delayed-open]:animate-in data-[state=delayed-open]:fade-in-0 data-[state=delayed-open]:zoom-in-95 data-open:animate-in data-open:fade-in-0 data-open:zoom-in-95 data-closed:animate-out data-closed:fade-out-0 data-closed:zoom-out-95",
            className
          )}
          {...props}
        >
          <TooltipActivity />
          {children}
        </TooltipPrimitive.Popup>
      </TooltipPrimitive.Positioner>
    </TooltipPrimitive.Portal>
  )
}

export {
  Tooltip,
  TooltipTrigger,
  TooltipContent,
  TooltipProvider,
  TOOLTIP_OPEN_DELAY,
  TOOLTIP_INSTANT_WINDOW,
  isTooltipInstantPhase,
}
