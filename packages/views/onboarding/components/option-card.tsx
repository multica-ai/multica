"use client";

import { cn } from "@multica/ui/lib/utils";

export function RadioMark({ selected }: { selected: boolean }) {
  return (
    <span
      aria-hidden
      className={cn(
        "relative inline-block h-4 w-4 shrink-0 rounded-full border-[1.5px] transition-colors",
        selected ? "border-foreground" : "border-border",
      )}
    >
      {selected && (
        <span className="absolute inset-[3px] rounded-full bg-foreground" />
      )}
    </span>
  );
}
