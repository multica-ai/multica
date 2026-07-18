"use client";

import type { ReactNode } from "react";
import { cn } from "@multica/ui/lib/utils";

/** A pill-button group for picking between mutually exclusive modes: the active
 *  option lifts to the background, the rest stay muted. One control for every
 *  either/or, so switching mode always looks and works the same wherever it
 *  appears. */
export function SegmentedToggle<T extends string>({
  value,
  options,
  onChange,
  buttonClassName,
}: {
  value: T;
  options: ReadonlyArray<readonly [T, ReactNode]>;
  onChange: (value: T) => void;
  /** Overrides the compact default sizing (text-xs px-2 py-1). */
  buttonClassName?: string;
}) {
  return (
    <div className="grid auto-cols-fr grid-flow-col gap-1 rounded-md bg-muted p-1">
      {options.map(([key, label]) => (
        <button
          key={key}
          type="button"
          aria-pressed={value === key}
          onClick={() => {
            if (key !== value) onChange(key);
          }}
          className={cn(
            "rounded-sm font-medium transition-colors",
            buttonClassName ?? "px-2 py-1 text-xs",
            value === key
              ? "bg-background text-foreground shadow-sm"
              : "text-muted-foreground hover:text-foreground",
          )}
        >
          {label}
        </button>
      ))}
    </div>
  );
}
