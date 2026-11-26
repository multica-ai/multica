"use client";

import { useState } from "react";
import { Check, Copy } from "@multica/ui/icons";
import { cn } from "@multica/ui/lib/utils";

/**
 * Shared Copy ↔ Check micro-feedback icon. Cross-scales between the two glyphs
 * over 140ms using the shared ease-out curve and tints the check with `text-success`.
 * Only the active icon is mounted so DOM assertions on `.icon-check` stay accurate,
 * and initial mount stays static until `copied` actually flips.
 */
export function CopyCheckIcon({
  copied,
  className,
  copyClassName,
  checkClassName,
}: {
  copied: boolean;
  className?: string;
  copyClassName?: string;
  checkClassName?: string;
}) {
  const [prevCopied, setPrevCopied] = useState(copied);
  const [hasFlipped, setHasFlipped] = useState(false);

  if (prevCopied !== copied) {
    setPrevCopied(copied);
    if (!hasFlipped) setHasFlipped(true);
  }

  const enterAnimation = hasFlipped
    ? "animate-in fade-in-0 zoom-in-50 duration-[140ms] ease-[cubic-bezier(0.23,1,0.32,1)] motion-reduce:animate-none!"
    : undefined;

  return (
    <span
      aria-hidden="true"
      data-slot="copy-check-icon"
      data-copied={copied ? "" : undefined}
      className={cn("relative inline-flex size-3.5 shrink-0 items-center justify-center", className)}
    >
      {copied ? (
        <Check
          key="check"
          aria-hidden="true"
          className={cn("size-full text-success", enterAnimation, checkClassName)}
        />
      ) : (
        <Copy
          key="copy"
          aria-hidden="true"
          className={cn("size-full", enterAnimation, copyClassName)}
        />
      )}
    </span>
  );
}
