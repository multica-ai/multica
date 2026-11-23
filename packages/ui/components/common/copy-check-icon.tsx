"use client";

import { useState } from "react";
import { Check, Copy } from "@multica/ui/icons";
import { cn } from "@multica/ui/lib/utils";

interface CopyCheckIconProps {
  copied: boolean;
  /** Applied to the rendered icon (controls size, e.g. `size-3.5`). */
  className?: string;
  /** Extra classes on the idle copy icon. */
  copyClassName?: string;
  /** Extra classes on the confirmed check icon (defaults to `text-success`). */
  checkClassName?: string;
}

const ICON_SWAP_MOTION =
  "animate-in zoom-in-75 fade-in-0 duration-140 ease-[cubic-bezier(0.23,1,0.32,1)] motion-reduce:animate-none";

/**
 * Animates between Copy and Check in place (140ms UI_EASE_OUT_CSS) so copy
 * buttons give tactile confirmation without mounting a second idle SVG.
 */
function CopyCheckIcon({
  copied,
  className = "size-3.5",
  copyClassName,
  checkClassName = "text-success",
}: CopyCheckIconProps) {
  const [hasToggled, setHasToggled] = useState(copied);
  if (copied && !hasToggled) {
    setHasToggled(true);
  }

  return copied ? (
    <Check
      key="check"
      aria-hidden="true"
      data-slot="copy-check-icon"
      data-copied="true"
      className={cn(className, ICON_SWAP_MOTION, checkClassName)}
    />
  ) : (
    <Copy
      key="copy"
      aria-hidden="true"
      data-slot="copy-check-icon"
      data-copied="false"
      className={cn(className, hasToggled && ICON_SWAP_MOTION, copyClassName)}
    />
  );
}

export { CopyCheckIcon, type CopyCheckIconProps };
