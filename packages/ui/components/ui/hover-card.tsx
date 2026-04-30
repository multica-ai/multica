"use client"

import { PreviewCard as PreviewCardPrimitive } from "@base-ui/react/preview-card"

import { cn } from "@multica/ui/lib/utils"

function HoverCard({ ...props }: PreviewCardPrimitive.Root.Props) {
  return <PreviewCardPrimitive.Root data-slot="hover-card" {...props} />
}

function HoverCardTrigger({ ...props }: PreviewCardPrimitive.Trigger.Props) {
  return (
    <PreviewCardPrimitive.Trigger data-slot="hover-card-trigger" {...props} />
  )
}

function HoverCardContent({
  className,
  side = "bottom",
  sideOffset = 4,
  align = "center",
  alignOffset = 4,
  onClick,
  onContextMenu,
  onAuxClick,
  onDoubleClick,
  ...props
}: PreviewCardPrimitive.Popup.Props &
  Pick<
    PreviewCardPrimitive.Positioner.Props,
    "align" | "alignOffset" | "side" | "sideOffset"
  >) {
  // Stop interaction events from bubbling out of the popup. Base UI portals
  // the popup to <body> so the DOM is detached, but React's synthetic event
  // system still bubbles through the React component tree — without this,
  // events on the popup would also fire on any ancestor of the trigger
  // (e.g. a clickable issue list row, a wrapping <a>).
  //
  // We stop the safe set: click / contextmenu / auxclick / dblclick.
  // We deliberately do NOT stop pointerdown / mousedown — Base UI's
  // outside-click dismiss listens to pointerdown on document and uses an
  // "inside React tree" check to decide whether to close. Stopping
  // pointerdown inside the popup would make the dismiss handler wrongly
  // think the click happened outside, requiring two clicks to close
  // (mirrors radix-ui/primitives#2782).
  const stop = <E extends React.SyntheticEvent>(forwarded?: (e: E) => void) =>
    (e: E) => {
      e.stopPropagation()
      forwarded?.(e)
    }
  return (
    <PreviewCardPrimitive.Portal data-slot="hover-card-portal">
      <PreviewCardPrimitive.Positioner
        align={align}
        alignOffset={alignOffset}
        side={side}
        sideOffset={sideOffset}
        className="isolate z-50"
      >
        <PreviewCardPrimitive.Popup
          data-slot="hover-card-content"
          onClick={stop(onClick)}
          onContextMenu={stop(onContextMenu)}
          onAuxClick={stop(onAuxClick)}
          onDoubleClick={stop(onDoubleClick)}
          className={cn(
            "z-50 w-64 origin-(--transform-origin) rounded-lg bg-popover p-2.5 text-sm text-popover-foreground shadow-md ring-1 ring-foreground/10 outline-hidden duration-100 data-[side=bottom]:slide-in-from-top-2 data-[side=inline-end]:slide-in-from-left-2 data-[side=inline-start]:slide-in-from-right-2 data-[side=left]:slide-in-from-right-2 data-[side=right]:slide-in-from-left-2 data-[side=top]:slide-in-from-bottom-2 data-open:animate-in data-open:fade-in-0 data-open:zoom-in-95 data-closed:animate-out data-closed:fade-out-0 data-closed:zoom-out-95",
            className
          )}
          {...props}
        />
      </PreviewCardPrimitive.Positioner>
    </PreviewCardPrimitive.Portal>
  )
}

export { HoverCard, HoverCardTrigger, HoverCardContent }
