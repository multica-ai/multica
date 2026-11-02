"use client";

import type { ComponentProps } from "react";
import { ArrowUpDown } from "@multica/ui/icons";
import { cn } from "@multica/ui/lib/utils";
import { Button } from "@multica/ui/components/ui/button";
import { Toggle } from "@multica/ui/components/ui/toggle";
import { Tooltip, TooltipContent, TooltipTrigger } from "@multica/ui/components/ui/tooltip";

/**
 * An on/off chip in a display panel. Hover never fills an unpressed chip: in
 * light mode `muted` and `accent` are the same grey, so a hover fill reads as
 * selected.
 */
export function DisplayChip({ className, ...props }: ComponentProps<typeof Toggle>) {
  return (
    <Toggle
      size="sm"
      variant="outline"
      className={cn(
        "h-6 max-w-40 gap-1.5 rounded-md px-2 text-caption text-muted-foreground hover:border-foreground/25 hover:bg-transparent hover:text-foreground aria-pressed:border-transparent aria-pressed:bg-input aria-pressed:text-foreground aria-pressed:hover:bg-input/70",
        className,
      )}
      {...props}
    />
  );
}

/** Flips a sort; `label` names the current direction, e.g. "Newest first". */
export function SortDirectionButton({ label, onClick }: { label: string; onClick: () => void }) {
  return (
    <Tooltip>
      <TooltipTrigger
        render={
          <Button
            variant="outline"
            size="icon-sm"
            className="text-muted-foreground"
            onClick={onClick}
            aria-label={label}
          >
            <ArrowUpDown className="size-3.5" />
          </Button>
        }
      />
      <TooltipContent side="bottom">{label}</TooltipContent>
    </Tooltip>
  );
}
