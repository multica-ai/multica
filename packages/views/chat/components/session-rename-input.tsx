"use client";

import { useEffect, useRef, useState } from "react";
import { isImeComposing } from "@multica/core/utils";
import { useT } from "../../i18n";

/**
 * Inline editor for a session title. Mounts focused with the existing
 * title pre-selected so the user can either replace it outright or arrow
 * into the existing text. Enter commits, Escape cancels, a real click
 * outside the input also commits.
 *
 * We do NOT commit on the input's `blur` event: the history popover can
 * move focus to sibling rows and nested actions while the user is still
 * interacting with the panel. Instead a document-level `pointerdown`
 * listener commits only when the user actually clicks outside the input.
 */
export function SessionRenameInput({
  initialValue,
  onSubmit,
  onCancel,
}: {
  initialValue: string;
  onSubmit: (value: string) => void;
  onCancel: () => void;
}) {
  const { t } = useT("chat");
  const [value, setValue] = useState(initialValue);
  const inputRef = useRef<HTMLInputElement>(null);
  // Hold the latest value + callback in refs so the mount-only effect's
  // listener always sees fresh state without re-subscribing on every
  // keystroke (which would briefly leave a window where pointerdown isn't
  // observed).
  const valueRef = useRef(value);
  valueRef.current = value;
  const onSubmitRef = useRef(onSubmit);
  onSubmitRef.current = onSubmit;

  useEffect(() => {
    inputRef.current?.focus();
    inputRef.current?.select();

    const handlePointerDown = (e: PointerEvent) => {
      const input = inputRef.current;
      if (!input) return;
      if (input.contains(e.target as Node)) return;
      onSubmitRef.current(valueRef.current);
    };
    // Capture phase — commit before outside-click handling can close the
    // popover and unmount this component.
    document.addEventListener("pointerdown", handlePointerDown, true);
    return () => {
      document.removeEventListener("pointerdown", handlePointerDown, true);
    };
  }, []);

  return (
    <input
      ref={inputRef}
      type="text"
      value={value}
      maxLength={200}
      aria-label={t(($) => $.session_history.row_rename_aria)}
      onChange={(e) => setValue(e.target.value)}
      onClick={(e) => e.stopPropagation()}
      onPointerDown={(e) => e.stopPropagation()}
      onKeyDown={(e) => {
        // Keep editing keys inside the input instead of letting the row
        // selection keyboard handler consume them.
        e.stopPropagation();
        if (isImeComposing(e)) return;
        if (e.key === "Enter") {
          e.preventDefault();
          onSubmit(value);
        } else if (e.key === "Escape") {
          e.preventDefault();
          onCancel();
        }
      }}
      className="w-full rounded-sm bg-background px-1 py-0.5 text-body outline-none ring-1 ring-border focus-visible:ring-brand"
    />
  );
}
