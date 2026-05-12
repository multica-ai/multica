"use client";

import { Pin } from "lucide-react";
import { cn } from "@multica/ui/lib/utils";
import { Tooltip, TooltipTrigger, TooltipContent } from "@multica/ui/components/ui/tooltip";

export interface PinButtonProps {
  isPinned: boolean;
  onToggle: () => void;
  pinLabel: string;
  unpinLabel: string;
  className?: string;
}

/**
 * Compact pin toggle shown next to the expand button on an input field.
 * Green when active (input is pinned to the viewport bottom), muted grey
 * otherwise.
 */
export function PinButton({ isPinned, onToggle, pinLabel, unpinLabel, className }: PinButtonProps) {
  return (
    <Tooltip>
      <TooltipTrigger
        render={
          <button
            type="button"
            aria-pressed={isPinned}
            aria-label={isPinned ? unpinLabel : pinLabel}
            onClick={onToggle}
            className={cn(
              "inline-flex h-6 w-6 items-center justify-center rounded-sm transition-all cursor-pointer hover:bg-accent/60",
              isPinned
                ? "text-emerald-500 opacity-100"
                : "text-muted-foreground opacity-70 hover:opacity-100",
              className,
            )}
          >
            <Pin className={cn("h-3.5 w-3.5", isPinned && "rotate-45 fill-emerald-500")} />
          </button>
        }
      />
      <TooltipContent side="top">{isPinned ? unpinLabel : pinLabel}</TooltipContent>
    </Tooltip>
  );
}
