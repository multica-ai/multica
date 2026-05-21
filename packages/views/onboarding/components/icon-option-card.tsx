"use client";

import type { ReactNode } from "react";
import { cn } from "@multica/ui/lib/utils";

const OTHER_INPUT_MAX_LENGTH = 80;

/**
 * Card-grid option used by the per-question questionnaire steps
 * (Source / Role / Use case). One row = icon + label. Clicking the
 * card selects it; the parent step decides when to advance (an
 * explicit Continue button gates the transition so users can change
 * their mind before committing). The `Other` variant swaps its
 * label area for a free-text input when selected.
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
 * "Other" variant — when selected, the label slot is replaced by a
 * borderless text input that inherits the card's typography so the
 * row keeps the same visual weight as the other cards. Auto-focuses
 * on open; Enter triggers the parent's `onConfirm`.
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
      role="radio"
      aria-checked={selected}
      onClick={() => {
        if (!selected) onSelect();
      }}
      className={cn(
        "flex w-full items-center gap-3 rounded-xl border bg-card px-4 py-3 text-left transition-all",
        selected
          ? "border-foreground shadow-[inset_0_0_0_1px_var(--color-foreground)]"
          : "cursor-pointer hover:border-foreground/30 hover:bg-accent/30",
      )}
    >
      <span
        aria-hidden
        className="flex h-7 w-7 shrink-0 items-center justify-center text-[18px] leading-none text-foreground"
      >
        {icon}
      </span>
      {selected ? (
        <input
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
          aria-label={placeholder}
          className="min-w-0 flex-1 border-0 bg-transparent p-0 text-[14px] font-medium leading-tight text-foreground placeholder:text-muted-foreground/60 focus:outline-none"
        />
      ) : (
        <span className="text-[14px] font-medium leading-tight text-foreground">
          {label}
        </span>
      )}
    </div>
  );
}

export { OTHER_INPUT_MAX_LENGTH };
