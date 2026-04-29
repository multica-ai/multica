"use client";

import {
  flexRender,
  type Row,
  type Table as TanstackTable,
} from "@tanstack/react-table";
import type * as React from "react";

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
//     fixed table-layout assigns them the leftover space (no spacer
//     column needed).
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
  className,
  ...props
}: DataTableProps<TData>) {
  return (
    <div
      className={cn("flex min-h-0 flex-1 flex-col", className)}
      {...props}
    >
      <div className="flex min-h-0 flex-1 flex-col overflow-auto bg-background">
        <table
          className="w-full table-fixed caption-bottom text-sm"
          style={{ minWidth: `${table.getTotalSize()}px` }}
        >
          <TableHeader className="sticky top-0 z-10 bg-muted/30 backdrop-blur">
            {table.getHeaderGroups().map((headerGroup) => (
              <TableRow key={headerGroup.id} className="hover:bg-transparent">
                {headerGroup.headers.map((header) => {
                  const isPinned = header.column.getIsPinned();
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
                      // Pinned header cell uses muted/30 so it blends
                      // into the header strip rather than appearing as
                      // a white block under sticky scroll.
                      className={cn(
                        "h-8 overflow-hidden px-4 py-2 text-xs uppercase tracking-wider text-muted-foreground",
                        isPinned && "bg-muted/30 backdrop-blur",
                      )}
                      style={getCellStyle(header.column, { withBorder: true })}
                    >
                      {header.isPlaceholder
                        ? null
                        : flexRender(
                            header.column.columnDef.header,
                            header.getContext(),
                          )}
                    </TableHead>
                  );
                })}
              </TableRow>
            ))}
          </TableHeader>
          <TableBody>
            {table.getRowModel().rows?.length ? (
              table.getRowModel().rows.map((row) => (
                <TableRow
                  key={row.id}
                  data-state={row.getIsSelected() && "selected"}
                  onClick={
                    onRowClick ? () => onRowClick(row) : undefined
                  }
                  // `group` lets pinned cells track row hover via
                  // group-hover (their bg is in className, not on the
                  // row, so they stay opaque enough to cover content
                  // scrolling beneath them).
                  className={cn(
                    "group",
                    onRowClick && "cursor-pointer",
                  )}
                >
                  {row.getVisibleCells().map((cell) => {
                    const isPinned = cell.column.getIsPinned();
                    return (
                      <TableCell
                        key={cell.id}
                        // px-4 across the board so cell content
                        // aligns with the surrounding toolbar's
                        // px-4. Narrow trailing columns (chevron /
                        // actions) declare a column.size large enough
                        // to fit the icon plus 16+16 padding.
                        // Pinned cells need an opaque bg + group-
                        // hover so they cover content scrolling
                        // beneath them and follow row hover state.
                        className={cn(
                          "overflow-hidden px-4 py-2",
                          isPinned &&
                            "bg-background group-hover:bg-muted/50",
                        )}
                        style={getCellStyle(cell.column, { withBorder: true })}
                      >
                        {flexRender(
                          cell.column.columnDef.cell,
                          cell.getContext(),
                        )}
                      </TableCell>
                    );
                  })}
                </TableRow>
              ))
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
        </table>
      </div>
      {actionBar &&
        table.getFilteredSelectedRowModel().rows.length > 0 &&
        actionBar}
    </div>
  );
}
