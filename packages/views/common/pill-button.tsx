"use client";

import { X as XIcon } from "lucide-react";
import { cn } from "@multica/ui/lib/utils";

// Shared pill chrome. `PillButton` puts it on the button itself; the clearable
// variant puts it on the shell that holds the trigger and the clear button, so
// both read as the same object.
//
// Hard width cap: pills carry user-generated text (project title, assignee
// name, label names), and the create toolbars are flex-wrap rows — an uncapped
// pill stretches until it owns the whole line and pushes every sibling to the
// next one. 14rem matches the chat composer's project pill. `min-w-0` +
// `overflow-hidden` let the inner `truncate` spans actually shrink (a flex item
// only drops `min-width: auto` once its overflow is hidden); leading icons are
// `shrink-0`, so the text yields first.
const PILL_CHROME =
  "inline-flex items-center rounded-full border text-caption min-w-0 max-w-56 overflow-hidden transition-colors";

export function PillButton({
  children,
  className,
  ...props
}: React.ButtonHTMLAttributes<HTMLButtonElement>) {
  return (
    <button
      type="button"
      className={cn(
        PILL_CHROME,
        "gap-1.5 px-2.5 py-1",
        "hover:bg-accent/60 cursor-pointer",
        "data-popup-open:bg-accent data-popup-open:text-accent-foreground",
        "disabled:cursor-not-allowed disabled:hover:bg-transparent",
        className,
      )}
      {...props}
    >
      {children}
    </button>
  );
}

/**
 * Pill trigger with a trailing quick-clear ×, for pills whose value can be
 * dropped in one click (project context, parent link, …). Same chrome as
 * `PillButton`, so a row can mix the two.
 *
 * Two sibling `<button>`s inside a plain shell — never a button nested in a
 * button, and never the overlay × this replaces: that one was an absolutely
 * positioned sibling of the trigger, so every caller had to reserve right
 * padding for it and three of five didn't, painting the × over the value
 * (MUL-5666).
 *
 * The root stays a `<span>` whether or not `onClear` is set. Presence of the
 * clear action tracks "the field has a value", which flips in the same commit
 * that closes the popover — swapping the root element type there would remount
 * the popover's own trigger mid-close.
 *
 * Forwarded props (including the `ref`, `onClick` and `data-popup-open` that
 * Base UI's `render` merges in) land on the inner trigger button, so the
 * popover anchors to the trigger and the × stays outside its hit area.
 */
export function ClearablePillButton({
  children,
  className,
  onClear,
  clearLabel,
  disabled,
  ...props
}: React.ButtonHTMLAttributes<HTMLButtonElement> & {
  /** Omit to render a plain pill — the × appears only when there is a value
   *  to drop. */
  onClear?: (() => void) | undefined;
  /** Accessible name for the × ("Clear project"). Required whenever
   *  `onClear` is set: the button has no text. */
  clearLabel?: string;
}) {
  // A disabled trigger disables clearing too: both are writes to the same
  // field, and a read-only pill that can still be emptied is not read-only.
  const clearable = !!onClear && !disabled;
  return (
    <span
      className={cn(
        PILL_CHROME,
        !disabled && "hover:bg-accent/60",
        // The open-state highlight lives on the trigger (Base UI stamps the
        // attribute there), but it has to paint the whole pill.
        "has-[[data-popup-open]]:bg-accent has-[[data-popup-open]]:text-accent-foreground",
        className,
      )}
    >
      <button
        type="button"
        disabled={disabled}
        className={cn(
          "flex min-w-0 items-center gap-1.5 overflow-hidden py-1 pl-2.5 cursor-pointer",
          clearable ? "pr-1" : "pr-2.5",
          "disabled:cursor-not-allowed",
        )}
        {...props}
      >
        {children}
      </button>
      {clearable && (
        <button
          type="button"
          aria-label={clearLabel}
          title={clearLabel}
          onClick={(e) => {
            e.stopPropagation();
            onClear?.();
          }}
          className="flex shrink-0 items-center py-1 pl-0.5 pr-2 text-muted-foreground hover:text-foreground cursor-pointer"
        >
          <XIcon className="size-3" />
        </button>
      )}
    </span>
  );
}
