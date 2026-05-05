import type { ReactNode } from "react";

/**
 * Two-column property row used in detail-page sidebars: a muted label on the
 * left and a flexible value on the right.
 *
 * Uses **subgrid**, so the parent must declare the column tracks:
 *
 *   <div className="grid grid-cols-[auto_1fr] gap-x-2 gap-y-0.5">
 *     <PropRow label="…">…</PropRow>
 *     <PropRow label="…">…</PropRow>
 *   </div>
 *
 * The `auto` track sizes to the widest label across all rows in the parent
 * grid, so labels always fit and values stay aligned across rows without
 * picking a magic pixel width. Earlier versions used a fixed `w-16` label;
 * that broke whenever a label (e.g. "Concurrency") rendered wider than 64px
 * — the label would overflow into the gap and collide with the value.
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
      className={`-mx-2 col-span-2 grid min-h-8 grid-cols-subgrid items-center rounded-md px-2 ${
        interactive ? "transition-colors hover:bg-accent/50" : ""
      }`}
    >
      <span className="text-xs text-muted-foreground">{label}</span>
      <div className="flex min-w-0 items-center gap-1.5 truncate text-xs">
        {children}
      </div>
    </div>
  );
}
