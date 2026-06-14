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
 * A small translucent pill that floats over the top edge of a mobile compose
 * field. Tapping it grows the field from the default height cap (half the
 * space above the keyboard) to 80%, and back. TECH-3536.
 *
 * The parent must be `position: relative`. Sits top-right so it never covers
 * the caret, which starts at the top-left.
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
        "absolute right-2 top-1.5 z-10 flex items-center gap-1 rounded-full",
        "bg-background/55 px-2 py-0.5 text-[11px] font-medium text-muted-foreground",
        "ring-1 ring-border/50 backdrop-blur-sm transition-colors",
        "hover:bg-background/80 active:bg-background/90",
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
