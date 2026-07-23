"use client";

import {
  flexRender,
  type Header as TanstackHeader,
  type Row,
  type Table as TanstackTable,
} from "@tanstack/react-table";
import { useVirtualizer } from "@tanstack/react-virtual";
import * as React from "react";

// We deliberately use the lower-level shadcn primitives (TableHeader /
// TableBody / TableRow / TableHead / TableCell) but NOT the wrapping
// <Table> component. shadcn's <Table> nests the <table> inside an
// `overflow-x-auto` <div>, which would compete with our outer scroll
// container and pin the horizontal scrollbar to the bottom of the
// table rather than the viewport.
import {
  TableBody,
  TableCell,
  TableHead,
  TableHeader,
  TableRow,
} from "@multica/ui/components/ui/table";
import { getCellStyle } from "@multica/ui/lib/data-table";
import { cn } from "@multica/ui/lib/utils";

interface DataTableProps<TData> extends React.ComponentProps<"div"> {
  table: TanstackTable<TData>;
  // Optional bar shown below the table when ≥1 row is selected. We
  // don't currently use selection — kept on the API surface for parity
  // with Dice UI's component so future row-select features just work.
  actionBar?: React.ReactNode;
  // Override for the empty-state cell text.
  emptyMessage?: React.ReactNode;
  // Called when the user clicks a row (anywhere outside an interactive
  // descendant — buttons / dropdowns inside cells should call
  // event.stopPropagation in their own handlers). Used to navigate to
  // a detail page on row click without nesting an <a> around <tr>,
  // which is invalid HTML.
  onRowClick?: (row: Row<TData>) => void;
  // Optional escape hatch for semantic rows such as collapsible group
  // headers. Return null/undefined to use the standard data row renderer.
  renderRow?: (row: Row<TData>) => React.ReactNode;
  // A caller-supplied <tfoot> (summary / quick-create rows, for example).
  footer?: React.ReactNode;
  // Render only the visible row window for large tables. Callers should use
  // this when their rows have a stable height; the footer remains part of the
  // same scroll surface and is reachable after the virtual row space.
  virtualizeRows?: boolean;
  virtualRowHeight?: number;
  virtualOverscan?: number;
}

// Headless data-table shell — adapted from Dice UI's data-table
// registry (https://diceui.com/r/data-table). Renders a TanStack Table
// instance using shadcn/ui's table primitives.
//
// Layout behaviour:
//   - `w-full` + `table-fixed` keeps the table at viewport width and
//     makes each column's width come from its first row's <th>
//     inline width. column.size is authoritative for sized columns.
//   - Columns flagged `meta.grow: true` skip their inline width, so
//     fixed table-layout assigns them the leftover space until the user
//     resizes them. Once resized, the explicit width is applied.
//   - The table's `min-width` is the sum of every column's TanStack
//     size (`table.getTotalSize()`). That gives grow columns a real
//     floor — fixed mode ignores cell-level min-width, but it does
//     respect `min-width` on the table itself. When the container is
//     wider than min-width the table tracks it; when narrower, the
//     table pins to min-width and the outer overflow-auto scrolls.
export function DataTable<TData>({
  table,
  actionBar,
  emptyMessage = "No results.",
  onRowClick,
  renderRow,
  footer,
  virtualizeRows = false,
  virtualRowHeight = 41,
  virtualOverscan = 10,
  className,
  ...props
}: DataTableProps<TData>) {
  const [resizingColumnId, setResizingColumnId] = React.useState<string | null>(
    null,
  );

  const columnSizing = table.getState().columnSizing;
  const hasExplicitSize = React.useCallback(
    (columnId: string) =>
      Object.prototype.hasOwnProperty.call(columnSizing, columnId),
    [columnSizing],
  );

  const setColumnWidth = React.useCallback(
    (header: TanstackHeader<TData, unknown>, width: number) => {
      const minSize = header.column.columnDef.minSize ?? 48;
      const maxSize =
        header.column.columnDef.maxSize ?? Number.MAX_SAFE_INTEGER;
      const next = Math.min(maxSize, Math.max(minSize, Math.round(width)));

      table.setColumnSizing((old) => ({
        ...old,
        [header.column.id]: next,
      }));
    },
    [table],
  );

  const beginColumnResize = React.useCallback(
    (
      header: TanstackHeader<TData, unknown>,
      event: React.PointerEvent<HTMLDivElement>,
    ) => {
      if (!header.column.getCanResize()) return;

      event.preventDefault();
      event.stopPropagation();

      const startX = event.clientX;
      const headerCell = event.currentTarget.closest("th");
      const startWidth =
        headerCell?.getBoundingClientRect().width ?? header.column.getSize();

      setResizingColumnId(header.column.id);
      setColumnWidth(header, startWidth);

      const originalCursor = document.body.style.cursor;
      const originalUserSelect = document.body.style.userSelect;
      document.body.style.cursor = "col-resize";
      document.body.style.userSelect = "none";

      const handlePointerMove = (pointerEvent: PointerEvent) => {
        setColumnWidth(header, startWidth + pointerEvent.clientX - startX);
      };

      const stopResize = () => {
        window.removeEventListener("pointermove", handlePointerMove);
        window.removeEventListener("pointerup", stopResize);
        window.removeEventListener("pointercancel", stopResize);
        document.body.style.cursor = originalCursor;
        document.body.style.userSelect = originalUserSelect;
        setResizingColumnId(null);
      };

      window.addEventListener("pointermove", handlePointerMove);
      window.addEventListener("pointerup", stopResize);
      window.addEventListener("pointercancel", stopResize);
    },
    [setColumnWidth],
  );

  const handleResizeKeyDown = React.useCallback(
    (
      header: TanstackHeader<TData, unknown>,
      event: React.KeyboardEvent<HTMLDivElement>,
    ) => {
      if (event.key !== "ArrowLeft" && event.key !== "ArrowRight") return;

      event.preventDefault();
      event.stopPropagation();

      const headerCell = event.currentTarget.closest("th");
      const currentWidth = hasExplicitSize(header.column.id)
        ? header.column.getSize()
        : (headerCell?.getBoundingClientRect().width ??
          header.column.getSize());
      const direction = event.key === "ArrowRight" ? 1 : -1;
      const step = event.shiftKey ? 20 : 8;

      setColumnWidth(header, currentWidth + direction * step);
    },
    [hasExplicitSize, setColumnWidth],
  );

  const scrollRef = React.useRef<HTMLDivElement | null>(null);
  const rows = table.getRowModel().rows;
  const getVirtualRowKey = React.useCallback(
    (index: number) => rows[index]?.id ?? index,
    [rows],
  );
  const rowVirtualizer = useVirtualizer({
    count: virtualizeRows ? rows.length : 0,
    getScrollElement: () => scrollRef.current,
    estimateSize: () => virtualRowHeight,
    getItemKey: getVirtualRowKey,
    overscan: virtualOverscan,
  });
  const virtualItems = rowVirtualizer.getVirtualItems();
  const firstVirtualItem = virtualItems[0];
  const lastVirtualItem = virtualItems[virtualItems.length - 1];
  const virtualPaddingTop = firstVirtualItem?.start ?? 0;
  const virtualPaddingBottom = lastVirtualItem
    ? rowVirtualizer.getTotalSize() - lastVirtualItem.end
    : 0;

  const renderDataRow = (row: Row<TData>) => {
    const customRow = renderRow?.(row);
    if (customRow != null) {
      return <React.Fragment key={row.id}>{customRow}</React.Fragment>;
    }
    return (
      <TableRow
        key={row.id}
        data-state={row.getIsSelected() && "selected"}
        onClick={onRowClick ? () => onRowClick(row) : undefined}
        // `group` lets pinned cells track row hover via group-hover (their bg
        // is in className, not on the row, so they stay opaque enough to cover
        // content scrolling beneath them).
        className={cn("group", onRowClick && "cursor-pointer")}
      >
        {row.getVisibleCells().map((cell) => {
          const isPinned = cell.column.getIsPinned();
          const columnHasExplicitSize = hasExplicitSize(cell.column.id);
          return (
            <TableCell
              key={cell.id}
              // px-4 across the board so cell content aligns with the
              // surrounding toolbar's px-4. Narrow trailing columns
              // (chevron / actions) declare enough width for icon + padding.
              // Pinned cells need an opaque bg + group-hover so they cover
              // content scrolling beneath them and follow row hover state.
              className={cn(
                "overflow-hidden px-4 py-2",
                isPinned &&
                  "bg-background group-hover:bg-[color-mix(in_oklab,var(--muted)_50%,var(--background))]",
              )}
              style={getCellStyle(cell.column, {
                withBorder: true,
                hasExplicitSize: columnHasExplicitSize,
              })}
            >
              {flexRender(cell.column.columnDef.cell, cell.getContext())}
            </TableCell>
          );
        })}
      </TableRow>
    );
  };

  const renderVirtualSpacer = (position: "top" | "bottom", height: number) =>
    height > 0 ? (
      <TableRow
        key={`virtual-spacer-${position}`}
        aria-hidden
        className="pointer-events-none border-0 hover:bg-transparent"
      >
        <TableCell
          colSpan={table.getVisibleLeafColumns().length}
          className="p-0"
          style={{ height: `${height}px` }}
        />
      </TableRow>
    ) : null;

  return (
    <div
      className={cn("flex min-h-0 flex-1 flex-col", className)}
      {...props}
    >
      <div
        ref={scrollRef}
        className="flex min-h-0 flex-1 flex-col overflow-auto bg-background"
      >
        <table
          className="w-full table-fixed caption-bottom text-sm"
          style={{ minWidth: `${table.getTotalSize()}px` }}
        >
          <TableHeader className="sticky top-0 z-10 bg-muted/30 backdrop-blur">
            {table.getHeaderGroups().map((headerGroup) => (
              <TableRow key={headerGroup.id} className="hover:bg-transparent">
                {headerGroup.headers.map((header) => {
                  const isPinned = header.column.getIsPinned();
                  const columnHasExplicitSize = hasExplicitSize(
                    header.column.id,
                  );
                  const headerLabel =
                    typeof header.column.columnDef.header === "string"
                      ? header.column.columnDef.header
                      : header.column.id;
                  return (
                    <TableHead
                      key={header.id}
                      colSpan={header.colSpan}
                      // Header typography overrides for a "spreadsheet
                      // header" look: smaller, all-caps, wider letter
                      // spacing, muted colour. shadcn's <TableHead>
                      // defaults to text-sm + text-foreground +
                      // font-medium, which reads as too heavy here.
                      // h-8 (32px) tightens the strip vs the default
                      // h-10 (40px).
                      // overflow-hidden caps any cell content that
                      // exceeds column.size. Tooltip / dropdown /
                      // hover-card bodies are portaled, so they are
                      // unaffected.
                      // Pinned cells must be opaque: translucent backgrounds
                      // reveal horizontally scrolled columns underneath. Mix
                      // muted with background to preserve the same visual tone
                      // as muted/30 without introducing alpha.
                      className={cn(
                        "relative h-8 overflow-hidden px-4 py-2 text-xs uppercase tracking-wider text-muted-foreground",
                        isPinned &&
                          "bg-[color-mix(in_oklab,var(--muted)_30%,var(--background))]",
                      )}
                      style={getCellStyle(header.column, {
                        withBorder: true,
                        hasExplicitSize: columnHasExplicitSize,
                      })}
                    >
                      {header.isPlaceholder
                        ? null
                        : flexRender(
                            header.column.columnDef.header,
                            header.getContext(),
                          )}
                      {!header.isPlaceholder &&
                        header.column.getCanResize() && (
                          <div
                            role="separator"
                            aria-label={`Resize ${headerLabel} column`}
                            aria-orientation="vertical"
                            tabIndex={0}
                            className={cn(
                              "absolute top-0 right-0 h-full w-2 cursor-col-resize touch-none select-none outline-none",
                              "after:absolute after:top-1/2 after:right-0 after:h-4 after:w-px after:-translate-y-1/2 after:bg-transparent after:transition-colors after:duration-100",
                              "hover:after:bg-foreground/20 focus-visible:after:bg-foreground/20",
                              resizingColumnId === header.column.id &&
                                "after:bg-foreground/40 after:transition-none",
                            )}
                            onPointerDown={(event) =>
                              beginColumnResize(header, event)
                            }
                            onDoubleClick={(event) => {
                              event.preventDefault();
                              event.stopPropagation();
                              header.column.resetSize();
                            }}
                            onKeyDown={(event) =>
                              handleResizeKeyDown(header, event)
                            }
                          />
                        )}
                    </TableHead>
                  );
                })}
              </TableRow>
            ))}
          </TableHeader>
          <TableBody>
            {rows.length ? (
              virtualizeRows ? (
                <>
                  {renderVirtualSpacer("top", virtualPaddingTop)}
                  {virtualItems.map((virtualItem) => {
                    const row = rows[virtualItem.index];
                    return row ? renderDataRow(row) : null;
                  })}
                  {renderVirtualSpacer("bottom", virtualPaddingBottom)}
                </>
              ) : (
                rows.map(renderDataRow)
              )
            ) : (
              <TableRow>
                <TableCell
                  colSpan={table.getAllColumns().length}
                  className="h-24 text-center text-muted-foreground"
                >
                  {emptyMessage}
                </TableCell>
              </TableRow>
            )}
          </TableBody>
          {footer}
        </table>
      </div>
      {actionBar &&
        table.getFilteredSelectedRowModel().rows.length > 0 &&
        actionBar}
    </div>
  );
}
