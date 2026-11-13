"use client";

import type { ComponentProps, ReactElement, ReactNode } from "react";
import {
  Tooltip,
  TooltipContent,
  TooltipTrigger,
} from "@multica/ui/components/ui/tooltip";

/**
 * Tooltip for a control that keeps its own element and styling (a raw
 * `<button>`, a link, a menu trigger, a text button that needs a longer
 * explanation). The child must carry its own accessible name; prefer
 * `IconButton` for a plain icon `Button`. A `null`/`undefined` label renders
 * the child alone, for hints that only apply in some states.
 */
function TooltipHint({
  label,
  shortcut,
  side = "top",
  children,
}: {
  label: ReactNode;
  /** Decorative keycaps shown after the label. */
  shortcut?: ReactNode;
  side?: ComponentProps<typeof TooltipContent>["side"];
  children: ReactElement;
}) {
  if (label == null || label === false || label === "") return children;
  return (
    <Tooltip>
      <TooltipTrigger render={children} />
      <TooltipContent side={side}>
        {label}
        {shortcut}
      </TooltipContent>
    </Tooltip>
  );
}

export { TooltipHint };
