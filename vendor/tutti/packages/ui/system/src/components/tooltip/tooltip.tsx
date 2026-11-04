import * as React from "react";
import { Tooltip as TooltipPrimitive } from "radix-ui";

import { cn } from "#lib/utils";
import { getPortalContainer } from "../../lib/portal-container";

function TooltipProvider({
  delayDuration = 0,
  ...props
}: React.ComponentProps<typeof TooltipPrimitive.Provider>) {
  return (
    <TooltipPrimitive.Provider
      data-slot="tooltip-provider"
      delayDuration={delayDuration}
      {...props}
    />
  );
}

function Tooltip({
  ...props
}: React.ComponentProps<typeof TooltipPrimitive.Root>) {
  return <TooltipPrimitive.Root data-slot="tooltip" {...props} />;
}

function TooltipTrigger({
  ...props
}: React.ComponentProps<typeof TooltipPrimitive.Trigger>) {
  return <TooltipPrimitive.Trigger data-slot="tooltip-trigger" {...props} />;
}

function TooltipPortal({
  ...props
}: React.ComponentProps<typeof TooltipPrimitive.Portal>) {
  return <TooltipPrimitive.Portal container={getPortalContainer()} data-slot="tooltip-portal" {...props} />;
}

function TooltipContent({
  className,
  sideOffset = 8,
  children,
  style,
  ...props
}: React.ComponentProps<typeof TooltipPrimitive.Content>) {
  return (
    <TooltipPortal>
      <TooltipPrimitive.Content
        data-slot="tooltip-content"
        sideOffset={sideOffset}
        className={cn(
          "inline-flex w-fit max-w-xs origin-(--radix-tooltip-content-transform-origin) items-center gap-2 rounded-md border border-[var(--border-1)] bg-[var(--background-fronted)] px-2 py-1 text-[13px] leading-[1.3] text-popover-foreground shadow-soft outline-none data-[side=bottom]:slide-in-from-top-2 data-[side=left]:slide-in-from-right-2 data-[side=right]:slide-in-from-left-2 data-[side=top]:slide-in-from-bottom-2 data-[state=delayed-open]:animate-in data-[state=delayed-open]:fade-in-0 data-[state=delayed-open]:zoom-in-95 data-open:animate-in data-open:fade-in-0 data-open:zoom-in-95 data-closed:animate-out data-closed:fade-out-0 data-closed:zoom-out-95",
          className
        )}
        style={{ zIndex: "var(--z-tooltip, 100700)", ...style }}
        {...props}
      >
        {children}
      </TooltipPrimitive.Content>
    </TooltipPortal>
  );
}

export {
  Tooltip,
  TooltipContent,
  TooltipPortal,
  TooltipProvider,
  TooltipTrigger
};
