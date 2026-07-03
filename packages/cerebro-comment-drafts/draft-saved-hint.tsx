"use client";

import { Check } from "lucide-react";

export interface DraftSavedHintProps {
  /** Show the hint only when a draft is actually stored for this composer. */
  show: boolean;
  className?: string;
}

/**
 * TECH-3491 — tiny "Draft saved" cue shown beside a composer once its draft is
 * persisted, so the user can trust the text is safe to walk away from.
 */
export function DraftSavedHint({ show, className }: DraftSavedHintProps) {
  if (!show) return null;
  return (
    <span
      data-testid="draft-saved-hint"
      className={
        "inline-flex items-center gap-1 text-[11px] text-muted-foreground" +
        (className ? ` ${className}` : "")
      }
    >
      <Check className="h-3 w-3" aria-hidden />
      Draft saved
    </span>
  );
}
