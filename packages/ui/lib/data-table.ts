import type { Column, RowData } from "@tanstack/react-table";
import type * as React from "react";

// Extend TanStack Table's ColumnMeta with a `grow` flag. TanStack merges
// a default `size: 150` into every columnDef, so "no explicit size" can't
// be detected by inspecting columnDef.size (it's always a number). Setting
// `meta: { grow: true }` is the official extension point — DataTable then
// skips the inline width for these columns and lets fixed table-layout
// assign them the leftover space (Linear / GitHub-PR-list pattern: title
// column grows, others stay at their declared widths).
declare module "@tanstack/react-table" {
  interface ColumnMeta<TData extends RowData, TValue> {
    grow?: boolean;
  }
}

// Combined sizing + pinning style for a `<th>` / `<td>` cell. Width is
// emitted unless the column is flagged `meta.grow` (those rely on
// fixed-layout's leftover-space distribution). Pinned columns get
// sticky positioning — see notes below.
//
// Background is intentionally NOT set inline — the upstream Dice UI
// version writes `background: var(--background)` here, which can't
// react to `:hover`. Consumers set bg via Tailwind classes paired with
// `group-hover:`.
export function getCellStyle<TData>(
  column: Column<TData>,
  options?: { withBorder?: boolean },
): React.CSSProperties {
  const grow = column.columnDef.meta?.grow;
  const width = grow ? undefined : column.columnDef.size;

  const isPinned = column.getIsPinned();
  if (!isPinned) {
    return width !== undefined ? { width } : {};
  }

  const withBorder = options?.withBorder ?? false;
  const isLastLeftPinned =
    isPinned === "left" && column.getIsLastColumn("left");
  const isFirstRightPinned =
    isPinned === "right" && column.getIsFirstColumn("right");

  return {
    width,
    position: "sticky",
    left: isPinned === "left" ? `${column.getStart("left")}px` : undefined,
    right: isPinned === "right" ? `${column.getAfter("right")}px` : undefined,
    zIndex: 1,
    boxShadow: withBorder
      ? isLastLeftPinned
        ? "-4px 0 4px -4px var(--border) inset"
        : isFirstRightPinned
          ? "4px 0 4px -4px var(--border) inset"
          : undefined
      : undefined,
  };
}
