import type { ReactNode } from "react";

/**
 * Two-column property row used in detail-page sidebars: a fixed-width muted
 * label on the left and a flexible value on the right.
 *
 * `interactive` (default `true`) controls whether the row gets a hover
 * highlight. Most rows wrap a Picker/Popover trigger and are clickable
 * anywhere across the row, so the highlight tells users "this is one
 * target". Read-only rows (Owner / Created / Updated) should pass
 * `interactive={false}` so they don't pretend to be clickable when they
 * aren't.
 *
 * Used by:
 *   - issue detail sidebar (Status / Priority / Assignee / …)
 *   - agent detail inspector (Runtime / Model / Visibility / …)
 *
 * Width of the label is intentionally narrow (`w-16` = 64px) so even
 * 320px-wide sidebars (agent inspector) leave reasonable room for the
 * value column.
 */
export function PropRow({
  label,
  children,
  interactive = true,
}: {
  label: string;
  children: ReactNode;
  interactive?: boolean;
}) {
  return (
    <div
      className={`-mx-2 flex min-h-8 items-center gap-2 rounded-md px-2 ${
        interactive ? "transition-colors hover:bg-accent/50" : ""
      }`}
    >
      <span className="w-16 shrink-0 text-xs text-muted-foreground">
        {label}
      </span>
      <div className="flex min-w-0 flex-1 items-center gap-1.5 truncate text-xs">
        {children}
      </div>
    </div>
  );
}
