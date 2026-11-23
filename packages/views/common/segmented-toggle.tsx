"use client";

import { useId, type ReactNode } from "react";
import { motion } from "motion/react";
import { useReducedMotion } from "@multica/core/appearance";
import { cn } from "@multica/ui/lib/utils";

const PILL_SPRING = { type: "spring", stiffness: 500, damping: 38 } as const;

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
  /** Overrides the compact default sizing (text-caption px-2 py-1). */
  buttonClassName?: string;
}) {
  const indicatorId = useId();
  const reduceMotion = useReducedMotion();

  return (
    <div className="grid auto-cols-fr grid-flow-col gap-1 rounded-md bg-muted p-1">
      {options.map(([key, label]) => {
        const active = value === key;
        return (
          <button
            key={key}
            type="button"
            aria-pressed={active}
            onClick={() => {
              if (key !== value) onChange(key);
            }}
            className={cn(
              "relative rounded-sm font-medium transition-colors",
              buttonClassName ?? "px-2 py-1 text-caption",
              active
                ? "text-foreground"
                : "text-muted-foreground hover:text-foreground",
            )}
          >
            {active && (
              <motion.span
                aria-hidden="true"
                layoutId={indicatorId}
                transition={reduceMotion ? { duration: 0 } : PILL_SPRING}
                className="pointer-events-none absolute inset-0 rounded-sm bg-background shadow-sm"
              />
            )}
            <span className="relative z-[1] inline-flex items-center justify-center gap-1.5">
              {label}
            </span>
          </button>
        );
      })}
    </div>
  );
}
