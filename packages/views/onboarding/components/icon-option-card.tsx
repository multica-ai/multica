"use client";

import type { ReactNode } from "react";
import { Input } from "@multica/ui/components/ui/input";
import { cn } from "@multica/ui/lib/utils";

const OTHER_INPUT_MAX_LENGTH = 80;

/**
 * Card-grid option used by the per-question questionnaire steps
 * (Source / Role / Use case). One row = icon + label. Clicking the
 * card both selects and advances — there is no Continue button on
 * these steps. The `Other` variant expands a text input below the
 * row instead of auto-advancing; the parent gates advance on
 * non-empty trimmed text.
 */
export function IconOptionCard({
  icon,
  label,
  selected,
  onSelect,
}: {
  icon: ReactNode;
  label: string;
  selected: boolean;
  onSelect: () => void;
}) {
  return (
    <button
      type="button"
      role="radio"
      aria-checked={selected}
      onClick={onSelect}
      className={cn(
        "group flex w-full items-center gap-3 rounded-xl border bg-card px-4 py-3 text-left transition-all",
        selected
          ? "border-foreground shadow-[inset_0_0_0_1px_var(--color-foreground)]"
          : "hover:border-foreground/30 hover:bg-accent/30",
      )}
    >
      <span
        aria-hidden
        className="flex h-7 w-7 shrink-0 items-center justify-center text-[18px] leading-none text-foreground"
      >
        {icon}
      </span>
      <span className="text-[14px] font-medium leading-tight text-foreground">
        {label}
      </span>
    </button>
  );
}

/**
 * "Other" variant — expands a free-text input below the row when
 * selected. Auto-focuses the input on open. The parent controls
 * advance-on-Enter / advance-on-blur via the `onConfirm` callback.
 */
export function IconOtherOptionCard({
  icon,
  label,
  selected,
  onSelect,
  otherValue,
  onOtherChange,
  onConfirm,
  placeholder,
}: {
  icon: ReactNode;
  label: string;
  selected: boolean;
  onSelect: () => void;
  otherValue: string;
  onOtherChange: (value: string) => void;
  onConfirm: () => void;
  placeholder: string;
}) {
  return (
    <div
      className={cn(
        "flex w-full flex-col rounded-xl border bg-card transition-all",
        selected
          ? "border-foreground shadow-[inset_0_0_0_1px_var(--color-foreground)]"
          : "hover:border-foreground/30",
      )}
    >
      <button
        type="button"
        role="radio"
        aria-checked={selected}
        onClick={onSelect}
        className="flex w-full items-center gap-3 px-4 py-3 text-left"
      >
        <span
          aria-hidden
          className="flex h-7 w-7 shrink-0 items-center justify-center text-[18px] leading-none text-foreground"
        >
          {icon}
        </span>
        <span className="text-[14px] font-medium leading-tight text-foreground">
          {label}
        </span>
      </button>
      {selected && (
        <div className="px-4 pb-3 pl-[52px]">
          <Input
            autoFocus
            type="text"
            value={otherValue}
            onChange={(e) => onOtherChange(e.target.value)}
            onKeyDown={(e) => {
              if (e.key === "Enter" && otherValue.trim()) {
                e.preventDefault();
                onConfirm();
              }
            }}
            placeholder={placeholder}
            maxLength={OTHER_INPUT_MAX_LENGTH}
            className="h-8 rounded-none border-x-0 border-t-0 border-b px-0 text-sm shadow-none focus-visible:border-foreground focus-visible:ring-0"
            aria-label={placeholder}
          />
        </div>
      )}
    </div>
  );
}

export { OTHER_INPUT_MAX_LENGTH };
