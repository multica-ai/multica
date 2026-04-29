"use client";

import {
  flexRender,
  type Row,
  type Table as TanstackTable,
} from "@tanstack/react-table";
import type * as React from "react";

// Note: we deliberately use the lower-level shadcn primitives
// (TableHeader / TableBody / TableRow / TableHead / TableCell) but NOT the
// wrapping <Table> component. shadcn's <Table> nests the <table> inside an
// `overflow-x-auto` <div>, which would compete with our outer scroll
// container and pin the horizontal scrollbar to the bottom of the table
// rather than the viewport. The <table> below is rendered inline with
// `w-max min-w-full table-fixed` so it sizes to the sum of column widths
// and the outer container handles both axes of scroll.
import {
  TableBody,
  TableCell,
  TableHead,
  TableHeader,
  TableRow,
} from "@multica/ui/components/ui/table";
import { getColumnPinningStyle } from "@multica/ui/lib/data-table";
import { cn } from "@multica/ui/lib/utils";

interface DataTableProps<TData> extends React.ComponentProps<"div"> {
  table: TanstackTable<TData>;
  // Optional bar shown above/below the table when ≥1 row is selected. We
  // don't currently use selection — kept on the API surface for parity
  // with Dice UI's component so future row-select features just work.
  actionBar?: React.ReactNode;
  // Slot rendered above the table viewport (toolbar, filters, etc.) so
  // it stays anchored while the table area scrolls. Optional.
  toolbar?: React.ReactNode;
  // Override for the empty-state cell text.
  emptyMessage?: React.ReactNode;
  // Called when the user clicks a row (anywhere outside an interactive
  // descendant — buttons / links / dropdowns inside cells should call
  // event.stopPropagation in their own handlers). Used to navigate to a
  // detail page on row click without nesting an <a> around <tr>, which
  // is invalid HTML.
  onRowClick?: (row: Row<TData>) => void;
}

// Headless data-table shell — adapted from Dice UI's data-table registry
// (https://diceui.com/r/data-table). Renders a TanStack Table instance
// using shadcn/ui's <Table> primitives. Column widths come from
// `column.getSize()` (set per column via the size/minSize/maxSize APIs in
// the column definition); when the sum of column widths exceeds the
// viewport, the inner wrapper's overflow-x: auto kicks in and the table
// scrolls horizontally.
//
// Differences from the upstream Dice UI component:
//   - No built-in pagination footer (we render full lists, not paginated).
//   - Toolbar slot is a named `toolbar` prop instead of children, so the
//     toolbar lives outside the scroll wrapper and stays put while the
//     table scrolls.
//   - Inner wrapper grows to fill remaining vertical space and scrolls
//     both axes.
export function DataTable<TData>({
  table,
  actionBar,
  toolbar,
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
      {toolbar}
      <div className="flex min-h-0 flex-1 flex-col overflow-auto bg-background">
        <table className="w-max min-w-full table-fixed caption-bottom text-sm">
          <TableHeader className="sticky top-0 z-10 bg-muted/30 backdrop-blur">
            {table.getHeaderGroups().map((headerGroup) => (
              <TableRow key={headerGroup.id}>
                {headerGroup.headers.map((header) => (
                  <TableHead
                    key={header.id}
                    colSpan={header.colSpan}
                    style={{
                      ...getColumnPinningStyle({ column: header.column }),
                    }}
                  >
                    {header.isPlaceholder
                      ? null
                      : flexRender(
                          header.column.columnDef.header,
                          header.getContext(),
                        )}
                  </TableHead>
                ))}
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
                  className={onRowClick ? "cursor-pointer" : undefined}
                >
                  {row.getVisibleCells().map((cell) => (
                    <TableCell
                      key={cell.id}
                      style={{
                        ...getColumnPinningStyle({ column: cell.column }),
                      }}
                    >
                      {flexRender(
                        cell.column.columnDef.cell,
                        cell.getContext(),
                      )}
                    </TableCell>
                  ))}
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
