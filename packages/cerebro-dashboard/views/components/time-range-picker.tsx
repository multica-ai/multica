"use client";

import { cn } from "@multica/ui/lib/utils";
import { useDashboardStore } from "../../core/store";
import type { TimeRange } from "../../core/types";

const RANGES: { value: TimeRange; label: string }[] = [
  { value: "24h", label: "24h" },
  { value: "7d", label: "7d" },
  { value: "30d", label: "30d" },
];

export function TimeRangePicker() {
  const range = useDashboardStore((s) => s.range);
  const setRange = useDashboardStore((s) => s.setRange);

  return (
    <div
      role="radiogroup"
      aria-label="Time range"
      className="inline-flex items-center rounded-md border bg-background p-0.5"
    >
      {RANGES.map((r) => {
        const active = range === r.value;
        return (
          <button
            key={r.value}
            role="radio"
            aria-checked={active}
            type="button"
            onClick={() => setRange(r.value)}
            className={cn(
              "rounded-sm px-2.5 py-1 text-xs font-medium transition-colors",
              active
                ? "bg-muted text-foreground"
                : "text-muted-foreground hover:text-foreground",
            )}
          >
            {r.label}
          </button>
        );
      })}
    </div>
  );
}
