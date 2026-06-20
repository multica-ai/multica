"use client";

import { ChevronDown, ChevronUp } from "lucide-react";
import { cn } from "@multica/ui/lib/utils";

interface ComposerExpandToggleProps {
  isExpanded: boolean;
  onToggle: () => void;
  /** Localized labels for the two states (passed in so this stays i18n-free). */
  expandLabel: string;
  collapseLabel: string;
  className?: string;
}

/**
 * A small translucent pill that overlays the TOP-RIGHT corner INSIDE a compose
 * field. Tapping it grows the field to the larger size and back. TECH-3536.
 *
 * The parent must be `position: relative`. `top-1 right-1` anchors the pill in
 * the field's top-right so it sits on the same horizontal line as the
 * left-aligned "replying to <agent>" trigger overlay, freeing the vertical band
 * the two chips used to occupy above the field (FIR-1625 follow-up). The pill
 * keeps its translucent/backdrop-blur background so editor text scrolls behind
 * it rather than being pushed down (TECH-3577 kept it right-anchored, clear of
 * the trigger overlay on the left).
 */
export function ComposerExpandToggle({
  isExpanded,
  onToggle,
  expandLabel,
  collapseLabel,
  className,
}: ComposerExpandToggleProps) {
  const label = isExpanded ? collapseLabel : expandLabel;
  return (
    <button
      type="button"
      onClick={onToggle}
      aria-label={label}
      title={label}
      className={cn(
        "absolute top-1 right-1 z-20 flex items-center gap-1 rounded-full",
        "bg-background/70 px-2 py-0.5 text-[11px] font-medium text-muted-foreground",
        "ring-1 ring-border/60 backdrop-blur-sm transition-colors",
        "hover:bg-background/90 active:bg-background",
        className,
      )}
    >
      {isExpanded ? (
        <ChevronDown className="size-3" />
      ) : (
        <ChevronUp className="size-3" />
      )}
      <span>{label}</span>
    </button>
  );
}
