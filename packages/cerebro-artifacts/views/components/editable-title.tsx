"use client";

import * as React from "react";
import { cn } from "@multica/ui/lib/utils";

/**
 * Inline-editable title rendered as a heading-sized field. The title is edited
 * in place — click and type, there is no separate "rename" dialog. Commits on
 * blur or Enter; Escape reverts to the last saved value.
 *
 * Shared by the Documents view (DocumentViewPage) and the Notes view
 * (NoteEditor) so both surfaces edit their title the exact same way (FIR-1647).
 */
export function EditableTitle({
  value,
  onSave,
  readOnly = false,
  allowEmpty = true,
  placeholder,
  className,
  ...rest
}: {
  value: string;
  onSave: (next: string) => void;
  readOnly?: boolean;
  /** When false, clearing the title reverts to the previous value instead of saving an empty title. */
  allowEmpty?: boolean;
  placeholder?: string;
  className?: string;
} & Omit<
  React.InputHTMLAttributes<HTMLInputElement>,
  "value" | "onChange" | "onBlur" | "disabled" | "placeholder" | "className"
>) {
  const [draft, setDraft] = React.useState(value);

  // Re-sync the local draft when the upstream title changes (switching items,
  // or another user renames it while this is open).
  React.useEffect(() => {
    setDraft(value);
  }, [value]);

  const commit = React.useCallback(() => {
    const next = draft.trim();
    if (!allowEmpty && !next) {
      setDraft(value);
      return;
    }
    if (next === value.trim()) return;
    onSave(next);
  }, [draft, value, allowEmpty, onSave]);

  return (
    <input
      aria-label="Title"
      value={draft}
      onChange={(e) => setDraft(e.target.value)}
      onBlur={commit}
      onKeyDown={(e) => {
        if (e.key === "Enter") {
          e.preventDefault();
          e.currentTarget.blur();
        } else if (e.key === "Escape") {
          e.preventDefault();
          setDraft(value);
          e.currentTarget.blur();
        }
      }}
      disabled={readOnly}
      placeholder={placeholder}
      className={cn(
        "w-full bg-transparent text-2xl font-bold leading-tight outline-none placeholder:text-muted-foreground/50 disabled:opacity-70",
        className,
      )}
      {...rest}
    />
  );
}
