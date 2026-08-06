"use client";

import { cn } from "@multica/ui/lib/utils";

export function PillButton({
  children,
  className,
  ...props
}: React.ButtonHTMLAttributes<HTMLButtonElement>) {
  return (
    <button
      type="button"
      className={cn(
        "inline-flex items-center gap-1.5 rounded-full border px-2.5 py-1 text-caption",
        // Hard width cap. Pills carry user-generated text (project title,
        // assignee name, label names), and the create toolbars are flex-wrap
        // rows — an uncapped pill stretches until it owns the whole line and
        // pushes every sibling to the next one. 14rem matches the chat
        // composer's project pill. `min-w-0` + `overflow-hidden` let the
        // inner `truncate` spans actually shrink (a flex item only drops
        // `min-width: auto` once its overflow is hidden); leading icons are
        // `shrink-0`, so the text yields first.
        "min-w-0 max-w-56 overflow-hidden",
        "hover:bg-accent/60 transition-colors cursor-pointer",
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
