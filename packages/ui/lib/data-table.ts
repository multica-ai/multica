import type { Column, RowData } from "@tanstack/react-table";
import type * as React from "react";

// Extend TanStack Table's ColumnMeta with a `grow` flag. TanStack merges
// a default `size: 150` into every columnDef, so "no explicit size" can't
// be detected by inspecting columnDef.size (it's always a number). Setting
// `meta: { grow: true }` is the official extension point: DataTable skips
// the inline width for these columns until the user explicitly resizes them,
// then the resized width wins.
declare module "@tanstack/react-table" {
  interface ColumnMeta<TData extends RowData, TValue> {
    grow?: boolean;
  }
}

// Custom-property name carrying a column's width. Column ids are app-defined
// and may hold characters that are not valid in a CSS ident (`property:<uuid>`
// is the live example), so everything outside the ident set is folded to `_`.
// The ids in play differ well before that point, so folding cannot collide.
export function columnSizeVar(columnId: string) {
  return `--col-${columnId.replace(/[^a-zA-Z0-9_-]/g, "_")}-size`;
}

// Combined sizing + pinning style for a `<th>` / `<td>` cell. Width is
// emitted unless the column is flagged `meta.grow` (those rely on
// fixed-layout's leftover-space distribution). Pinned columns get
// sticky positioning — see notes below.
//
// The width is a reference to a custom property published once on the <table>
// rather than a number read per cell. During a resize that lets the browser
// apply every new width from a single declaration change, with no React work
// in the hundreds of cells that would otherwise each re-read column.getSize().
//
// Background is intentionally NOT set inline — the upstream Dice UI
// version writes `background: var(--background)` here, which can't
// react to `:hover`. Consumers set bg via Tailwind classes paired with
// `group-hover:`.
export function getCellStyle<TData>(
  column: Column<TData>,
  options?: { hasExplicitSize?: boolean },
): React.CSSProperties {
  const grow = column.columnDef.meta?.grow;
  const width =
    grow && !options?.hasExplicitSize
      ? undefined
      : `var(${columnSizeVar(column.id)})`;

  const isPinned = column.getIsPinned();
  if (!isPinned) {
    return width !== undefined ? { width } : {};
  }

  // No edge marker here. Where the frozen columns end is drawn by the scroll
  // container's mask instead — a shadow inside the pinned cell can only darken
  // the frozen column's own edge, while the mask fades the content that is
  // actually sliding underneath, which is the thing being described.
  return {
    width,
    position: "sticky",
    left: isPinned === "left" ? `${column.getStart("left")}px` : undefined,
    right: isPinned === "right" ? `${column.getAfter("right")}px` : undefined,
    zIndex: 1,
  };
}

// Mask for the scroll container: content emerging from under the frozen
// columns fades in over `fade` px instead of appearing at a hard edge.
//
// The stops are anchored to the frozen block's width, never to the scroll
// offset — a mask on a scroll container is positioned against its padding box,
// which does not scroll, so the fade stays welded to the boundary. Anchoring
// is also what keeps the frozen columns themselves at full opacity: the
// generic "fade both ends of a scroller" treatment would dissolve exactly the
// columns that have to stay solid.
export function pinnedScrollMask(
  pinnedWidth: number,
  fade = 24,
): React.CSSProperties | undefined {
  if (pinnedWidth <= 0) return undefined;
  const gradient =
    `linear-gradient(to right, black 0, black ${pinnedWidth}px, ` +
    `transparent ${pinnedWidth}px, black ${pinnedWidth + fade}px, black 100%)`;
  return { maskImage: gradient, WebkitMaskImage: gradient };
}
