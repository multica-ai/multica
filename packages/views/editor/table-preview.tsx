"use client";

/**
 * TablePreview — a CSV / TSV attachment as a table, for the viewer's `table`
 * kind.
 *
 * Rendered by the shared DataTable, so the header stays pinned while the body
 * scrolls, rows virtualize (a 2 MB export is tens of thousands of them), and
 * columns resize by dragging their edge. Clicking a header sorts by that
 * column: ascending, descending, then back to file order. The row-number
 * column is frozen on the left and keeps each row's position in the file, so
 * a sorted row can still be found in the source.
 *
 * The first record is the header, as in every spreadsheet import.
 */

import { useMemo, useState } from "react";
import {
  getCoreRowModel,
  getSortedRowModel,
  useReactTable,
  type ColumnDef,
  type Row,
  type SortingState,
} from "@tanstack/react-table";
import { ArrowDown, ArrowUp, ArrowUpDown } from "lucide-react";
import { DataTable } from "@multica/ui/components/ui/data-table";
import { cn } from "@multica/ui/lib/utils";
import { useT } from "../i18n";
import {
  parseDelimited,
  sniffDelimiter,
  toTable,
  type CellDelimiter,
} from "./utils/parse-delimited";

const ROW_NUMBER_COLUMN = "__row";

// Column widths come from the content, not a flat default: an `id` column
// and a `description` column should not start out the same size. Estimated
// from a sample of rows, then clamped so one long value cannot claim the
// whole table — the user can widen any column by dragging.
const SAMPLE_ROWS = 200;
const BODY_FONT_PX = 14;
const HEADER_FONT_PX = 12;
const CELL_PADDING = 32;
// The sort arrow and its gap beside a header's name.
const SORT_ICON_ROOM = 18;
const MIN_COLUMN_WIDTH = 72;
const MAX_COLUMN_WIDTH = 320;
const ROW_HEIGHT = 37;
const SCROLLBAR_SLACK = 16;

// Rendered width of `value` at `fontPx`, in em-fractions per character class
// of a proportional UI face: figures and capitals run wide (and figures are
// tabular), lowercase and punctuation narrower, CJK glyphs a full em.
function textWidth(value: string, fontPx: number): number {
  let ems = 0;
  for (const char of value) {
    if ((char.codePointAt(0) ?? 0) > 0x2e80) ems += 1;
    else if (/[0-9A-Z]/.test(char)) ems += 0.64;
    else ems += 0.54;
  }
  return ems * fontPx;
}

function toNumber(value: string): number | null {
  const trimmed = value.trim();
  if (trimmed === "") return null;
  const number = Number(trimmed);
  return Number.isFinite(number) ? number : null;
}

const collator = new Intl.Collator(undefined, {
  numeric: true,
  sensitivity: "base",
});

// Numbers compare as numbers (so 9 < 10), and sort ahead of text in a mixed
// column. Empty cells never reach here — `sortUndefined: "last"` keeps them at
// the bottom in both directions.
export function compareCells(a: string, b: string): number {
  const x = toNumber(a);
  const y = toNumber(b);
  if (x !== null && y !== null) return x - y;
  if (x !== null) return -1;
  if (y !== null) return 1;
  return collator.compare(a, b);
}

interface ColumnInfo {
  width: number;
  numeric: boolean;
}

function describeColumns(header: string[], rows: string[][]): ColumnInfo[] {
  const sample = rows.slice(0, SAMPLE_ROWS);
  return header.map((title, index) => {
    let widest = textWidth(title, HEADER_FONT_PX) + SORT_ICON_ROOM;
    let numbers = 0;
    let filled = 0;
    for (const row of sample) {
      const value = row[index] ?? "";
      widest = Math.max(widest, textWidth(value, BODY_FONT_PX));
      if (value.trim() === "") continue;
      filled += 1;
      if (toNumber(value) !== null) numbers += 1;
    }
    const width = Math.ceil(widest + CELL_PADDING);
    return {
      width: Math.min(MAX_COLUMN_WIDTH, Math.max(MIN_COLUMN_WIDTH, width)),
      numeric: filled > 0 && numbers === filled,
    };
  });
}

function sortByCell(rowA: Row<string[]>, rowB: Row<string[]>, columnId: string) {
  return compareCells(
    rowA.getValue<string>(columnId),
    rowB.getValue<string>(columnId),
  );
}

interface TablePreviewProps {
  text: string;
  /** `\t` for TSV. A CSV's delimiter is sniffed — `;` exports are common. */
  delimiter: "," | "\t";
}

export function TablePreview({ text, delimiter }: TablePreviewProps) {
  const { t } = useT("editor");
  const [sorting, setSorting] = useState<SortingState>([]);

  const table = useMemo(() => {
    const cellDelimiter: CellDelimiter =
      delimiter === "\t" ? "\t" : sniffDelimiter(text);
    return toTable(parseDelimited(text, cellDelimiter));
  }, [text, delimiter]);

  const columnInfo = useMemo(
    () => describeColumns(table.header, table.rows),
    [table],
  );

  const columns = useMemo<ColumnDef<string[]>[]>(() => {
    const digits = String(table.rows.length).length;
    const rowNumber: ColumnDef<string[]> = {
      id: ROW_NUMBER_COLUMN,
      header: "#",
      cell: ({ row }) => (
        <span className="block text-right tabular-nums text-muted-foreground">
          {row.index + 1}
        </span>
      ),
      size: Math.max(48, Math.ceil(digits * 0.64 * BODY_FONT_PX) + CELL_PADDING),
      enableSorting: false,
      enableResizing: false,
    };
    return [
      rowNumber,
      ...table.header.map<ColumnDef<string[]>>((title, index) => ({
        id: `c${index}`,
        // Empty cells read as "no value" so they can sort last.
        accessorFn: (row) => (row[index] === "" ? undefined : row[index]),
        header: ({ column }) => (
          <SortHeader
            title={title}
            numeric={columnInfo[index]?.numeric ?? false}
            sorted={column.getIsSorted()}
            onToggle={column.getToggleSortingHandler()}
          />
        ),
        cell: ({ getValue }) => {
          const value = getValue<string | undefined>() ?? "";
          return (
            <span
              className={cn(
                "block truncate",
                columnInfo[index]?.numeric && "text-right tabular-nums",
              )}
              title={value}
            >
              {value}
            </span>
          );
        },
        size: columnInfo[index]?.width ?? MIN_COLUMN_WIDTH,
        minSize: 48,
        sortingFn: sortByCell,
        sortUndefined: "last",
      })),
    ];
  }, [table, columnInfo]);

  const reactTable = useReactTable({
    data: table.rows,
    columns,
    state: { sorting },
    onSortingChange: setSorting,
    getCoreRowModel: getCoreRowModel(),
    getSortedRowModel: getSortedRowModel(),
    initialState: { columnPinning: { left: [ROW_NUMBER_COLUMN] } },
    columnResizeMode: "onChange",
  });

  return (
    <div className="flex h-full flex-col pb-4">
      {/* As wide as the columns need, up to the stage: a three-column file
          reads as a sheet, a wide export uses every pixel and scrolls. The
          slack is a classic scrollbar's width, so one never forces a
          sideways scroll on its own. */}
      <div
        className="mx-auto flex min-h-0 w-full flex-1 flex-col overflow-hidden rounded-lg bg-background text-foreground"
        style={{ maxWidth: reactTable.getTotalSize() + SCROLLBAR_SLACK }}
        data-testid="table-preview"
      >
        <DataTable
          table={reactTable}
          virtualizeRows
          virtualRowHeight={ROW_HEIGHT}
          emptyMessage={t(($) => $.attachment.table_empty)}
        />
        <p className="shrink-0 border-t border-border px-4 py-1.5 text-caption tabular-nums text-muted-foreground">
          {t(($) => $.attachment.table_rows, { count: table.rows.length })}
          {" · "}
          {t(($) => $.attachment.table_columns, { count: table.columnCount })}
        </p>
      </div>
    </div>
  );
}

function SortHeader({
  title,
  numeric,
  sorted,
  onToggle,
}: {
  title: string;
  numeric: boolean;
  sorted: false | "asc" | "desc";
  onToggle: ((event: unknown) => void) | undefined;
}) {
  const Icon = sorted === "asc" ? ArrowUp : sorted === "desc" ? ArrowDown : ArrowUpDown;
  return (
    <button
      type="button"
      onClick={onToggle}
      className={cn(
        // The table header's spreadsheet casing is for labels the app wrote;
        // these are the file's own column names and show as written.
        "group/sort flex w-full min-w-0 items-center gap-1 rounded-sm normal-case tracking-normal text-foreground outline-none focus-visible:ring-2 focus-visible:ring-ring",
        numeric && "flex-row-reverse",
      )}
    >
      <span className="truncate font-medium" title={title}>
        {title}
      </span>
      <Icon
        aria-hidden
        className={cn(
          "size-3.5 shrink-0",
          sorted
            ? "text-foreground"
            : "text-muted-foreground opacity-0 transition-opacity group-hover/sort:opacity-100 group-focus-visible/sort:opacity-100",
        )}
      />
    </button>
  );
}
