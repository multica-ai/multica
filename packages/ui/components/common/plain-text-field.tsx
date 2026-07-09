"use client";

import * as React from "react";
import { cn } from "../../lib/utils";
import { Textarea } from "../ui/textarea";

interface PlainTextFieldProps {
  defaultValue: string;
  /** Called on blur when the trimmed value changed and is within the limit. */
  onCommit: (value: string) => void;
  placeholder?: string;
  /** Soft limit: typing past it is allowed but shows the hint and blocks commit. */
  maxLength?: number;
  /** Over-limit hint line; caller supplies the localized text. */
  limitHint?: (count: number, max: number) => string;
  className?: string;
  id?: string;
  "aria-label"?: string;
}

/**
 * Not an editor — a borderless plain-text area that reads as page text until
 * clicked (Linear's "Add a description…" pattern). Single line at rest,
 * auto-grows with content. Commits on blur, restores on Escape. Re-seed by
 * changing the React `key` when the upstream value changes.
 */
function PlainTextField({
  defaultValue,
  onCommit,
  placeholder,
  maxLength = 255,
  limitHint,
  className,
  ...props
}: PlainTextFieldProps) {
  const [value, setValue] = React.useState(defaultValue);
  const over = value.length > maxLength;

  return (
    <div className="flex flex-col gap-1">
      <Textarea
        {...props}
        value={value}
        rows={1}
        placeholder={placeholder}
        onChange={(event) => setValue(event.target.value)}
        onKeyDown={(event) => {
          if (event.key === "Escape") {
            setValue(defaultValue);
            event.currentTarget.blur();
          }
        }}
        onBlur={() => {
          if (over) return; // keep the text and the hint; nothing is saved
          const next = value.trim();
          if (next !== defaultValue) onCommit(next);
        }}
        className={cn(
          "min-h-8 resize-none rounded-none border-0 bg-transparent px-0 py-1 shadow-none focus-visible:ring-0 dark:bg-transparent",
          className,
        )}
      />
      {over && (
        <p className="text-xs text-destructive">
          {limitHint ? limitHint(value.length, maxLength) : `${value.length}/${maxLength}`}
        </p>
      )}
    </div>
  );
}

export { PlainTextField };
