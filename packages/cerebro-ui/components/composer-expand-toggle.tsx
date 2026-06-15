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
 * A small translucent pill that floats just ABOVE the top edge of a compose
 * field. Tapping it grows the field to the larger size and back. TECH-3536.
 *
 * The parent must be `position: relative`. `bottom-full` anchors the pill's
 * bottom to the parent's top edge so it sits in the gap above the field and
 * never covers the text inside it (Jesper's feedback on TECH-3536). Centered
 * horizontally so it reads as attached to the field on every surface.
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
        "absolute bottom-full left-1/2 z-20 mb-1 -translate-x-1/2 flex items-center gap-1 rounded-full",
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
