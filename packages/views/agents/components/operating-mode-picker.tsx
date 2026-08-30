"use client";

import {
  useId,
  useRef,
  type KeyboardEvent,
} from "react";
import type { AgentOperatingMode } from "@multica/core/types";
import { cn } from "@multica/ui/lib/utils";
import { useT } from "../../i18n";

const OPERATING_MODES: AgentOperatingMode[] = [
  "coding",
  "operational",
  "hybrid",
];

export function OperatingModePicker({
  value,
  onChange,
  disabled = false,
}: {
  value: AgentOperatingMode;
  onChange: (value: AgentOperatingMode) => void;
  disabled?: boolean;
}) {
  const { t } = useT("agents");
  const hintId = useId();
  const refs = useRef<Array<HTMLButtonElement | null>>([]);

  const handleKeyDown = (
    event: KeyboardEvent<HTMLButtonElement>,
    index: number,
  ) => {
    if (disabled) return;
    let nextIndex: number | null = null;
    if (event.key === "ArrowRight" || event.key === "ArrowDown") {
      nextIndex = (index + 1) % OPERATING_MODES.length;
    } else if (event.key === "ArrowLeft" || event.key === "ArrowUp") {
      nextIndex =
        (index - 1 + OPERATING_MODES.length) % OPERATING_MODES.length;
    } else if (event.key === "Home") {
      nextIndex = 0;
    } else if (event.key === "End") {
      nextIndex = OPERATING_MODES.length - 1;
    }
    if (nextIndex === null) return;
    event.preventDefault();
    const next = OPERATING_MODES[nextIndex]!;
    onChange(next);
    refs.current[nextIndex]?.focus();
  };

  return (
    <div className="space-y-3">
      <div>
        <p className="text-body font-medium">
          {t(($) => $.operating_mode.label)}
        </p>
        <p
          id={hintId}
          className="mt-0.5 text-caption leading-5 text-muted-foreground"
        >
          {t(($) => $.operating_mode.hint)}
        </p>
      </div>
      <div
        role="radiogroup"
        aria-label={t(($) => $.operating_mode.label)}
        aria-describedby={hintId}
        className="grid gap-2 md:grid-cols-3"
      >
        {OPERATING_MODES.map((mode, index) => {
          const selected = value === mode;
          return (
            <button
              key={mode}
              ref={(element) => {
                refs.current[index] = element;
              }}
              type="button"
              role="radio"
              aria-checked={selected}
              tabIndex={selected ? 0 : -1}
              disabled={disabled}
              onClick={() => onChange(mode)}
              onKeyDown={(event) => handleKeyDown(event, index)}
              className={cn(
                "rounded-lg border px-3 py-3 text-left transition-colors",
                "focus-visible:outline-none focus-visible:ring-2 focus-visible:ring-ring",
                selected
                  ? "border-primary bg-primary/5"
                  : "border-surface-border hover:bg-muted",
                disabled && "cursor-not-allowed opacity-60",
              )}
            >
              <span className="block text-body font-medium">
                {t(($) => $.operating_mode.modes[mode].title)}
              </span>
              <span className="mt-1 block text-caption leading-5 text-muted-foreground">
                {t(($) => $.operating_mode.modes[mode].description)}
              </span>
            </button>
          );
        })}
      </div>
    </div>
  );
}
