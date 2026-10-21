"use client";

/**
 * A Markdown table at dialog size. Rendered by the shared DataTable — the same
 * shell as the CSV attachment preview — so the header stays pinned, columns
 * resize by dragging and sort by clicking their header, and the row-number
 * column keeps each row's position in the source. Cells are the rendered
 * Markdown cells, so links, mentions and inline code behave as they do inline.
 */

import { useMemo, useState, type ReactNode } from "react";
import {
  getCoreRowModel,
  getFilteredRowModel,
  getSortedRowModel,
  useReactTable,
  type ColumnDef,
  type FilterFn,
  type Row,
  type SortingState,
} from "@tanstack/react-table";
import { Check, Copy, Search, X } from "lucide-react";
import { Button } from "@multica/ui/components/ui/button";
import { DataTable } from "@multica/ui/components/ui/data-table";
import {
  Dialog,
  DialogClose,
  DialogContent,
  DialogTitle,
} from "@multica/ui/components/ui/dialog";
import { Input } from "@multica/ui/components/ui/input";
import { copyText } from "@multica/ui/lib/clipboard";
import { cn } from "@multica/ui/lib/utils";
import { useT } from "../i18n";
import { SortHeader, compareCells, textWidth } from "../editor/table-preview";
import { tableToCsv, type RichTableModel } from "./rich-table-model";

const ROW_NUMBER_COLUMN = "__row";
const BODY_FONT_PX = 14;
const HEADER_FONT_PX = 13;
const CELL_PADDING = 32;
const SORT_ICON_ROOM = 18;
const MIN_COLUMN_WIDTH = 96;
const MAX_COLUMN_WIDTH = 480;

interface DialogRow {
  index: number;
  cells: ReactNode[];
  text: string[];
}

const matchesFilter: FilterFn<DialogRow> = (row, _columnId, filter: string) => {
  const needle = filter.trim().toLocaleLowerCase();
  return !needle || row.original.text.some((text) => text.toLocaleLowerCase().includes(needle));
};

function sortByCell(rowA: Row<DialogRow>, rowB: Row<DialogRow>, columnId: string) {
  return compareCells(rowA.getValue<string>(columnId), rowB.getValue<string>(columnId));
}

function isNumeric(values: string[]): boolean {
  const filled = values.filter((value) => value !== "");
  return filled.length > 0 && filled.every((value) => Number.isFinite(Number(value)));
}

function useCopyFeedback() {
  const [copied, setCopied] = useState<string | null>(null);
  const copy = async (id: string, text: string) => {
    if (await copyText(text)) {
      setCopied(id);
      setTimeout(() => setCopied((current) => (current === id ? null : current)), 2000);
    }
  };
  return { copied, copy };
}

interface RichTableDialogProps {
  model: RichTableModel;
  open: boolean;
  onOpenChange: (open: boolean) => void;
  getMarkdown: () => string;
}

export function RichTableDialog({ model, open, onOpenChange, getMarkdown }: RichTableDialogProps) {
  const { t } = useT("editor");
  const [sorting, setSorting] = useState<SortingState>([]);
  const [filter, setFilter] = useState("");
  const { copied, copy } = useCopyFeedback();

  const data = useMemo<DialogRow[]>(
    () => model.rows.map((cells, index) => ({ index, cells, text: model.rowText[index] ?? [] })),
    [model],
  );

  const columns = useMemo<ColumnDef<DialogRow>[]>(() => {
    const digits = String(model.rows.length).length;
    const rowNumber: ColumnDef<DialogRow> = {
      id: ROW_NUMBER_COLUMN,
      header: "#",
      cell: ({ row }) => (
        <span className="block text-right tabular-nums text-muted-foreground">
          {row.original.index + 1}
        </span>
      ),
      size: Math.max(48, Math.ceil(digits * 0.64 * BODY_FONT_PX) + CELL_PADDING),
      enableSorting: false,
      enableResizing: false,
      enableGlobalFilter: false,
    };
    return [
      rowNumber,
      ...model.headerText.map<ColumnDef<DialogRow>>((title, index) => {
        const values = model.rowText.map((row) => row[index] ?? "");
        const numeric = isNumeric(values);
        const widest = Math.max(
          textWidth(title, HEADER_FONT_PX) + SORT_ICON_ROOM,
          ...values.slice(0, 200).map((value) => textWidth(value, BODY_FONT_PX)),
        );
        return {
          id: `c${index}`,
          accessorFn: (row) => (row.text[index] ? row.text[index] : undefined),
          header: ({ column }) => (
            <SortHeader
              title={title}
              numeric={numeric}
              sorted={column.getIsSorted()}
              onToggle={column.getToggleSortingHandler()}
            />
          ),
          cell: ({ row }) => (
            <div className={cn("rich-text-editor readonly whitespace-normal", numeric && "text-right tabular-nums")}>
              {row.original.cells[index]}
            </div>
          ),
          size: Math.min(MAX_COLUMN_WIDTH, Math.max(MIN_COLUMN_WIDTH, Math.ceil(widest + CELL_PADDING))),
          minSize: 64,
          sortingFn: sortByCell,
          sortUndefined: "last",
        };
      }),
    ];
  }, [model]);

  const table = useReactTable({
    data,
    columns,
    state: { sorting, globalFilter: filter },
    onSortingChange: setSorting,
    onGlobalFilterChange: setFilter,
    globalFilterFn: matchesFilter,
    getColumnCanGlobalFilter: (column) => column.id !== ROW_NUMBER_COLUMN,
    getCoreRowModel: getCoreRowModel(),
    getSortedRowModel: getSortedRowModel(),
    getFilteredRowModel: getFilteredRowModel(),
    initialState: { columnPinning: { left: [ROW_NUMBER_COLUMN] } },
    columnResizeMode: "onChange",
  });

  const shownRows = table.getRowModel().rows.length;

  return (
    <Dialog open={open} onOpenChange={onOpenChange}>
      <DialogContent
        showCloseButton={false}
        className="flex h-[min(85dvh,56rem)] w-[min(72rem,calc(100vw-4rem))] max-w-none flex-col gap-0 overflow-hidden p-0 sm:max-w-none"
      >
        <div className="flex shrink-0 items-center gap-2 border-b border-border py-2 pr-2 pl-4">
          <DialogTitle className="mr-auto text-body font-medium">
            {t(($) => $.table.expanded_title)}
          </DialogTitle>
          <div className="relative w-56 shrink-0">
            <Search
              aria-hidden
              className="pointer-events-none absolute top-1/2 left-2 size-3.5 -translate-y-1/2 text-muted-foreground"
            />
            <Input
              type="text"
              role="searchbox"
              inputMode="search"
              value={filter}
              onChange={(event) => setFilter(event.target.value)}
              aria-label={t(($) => $.table.filter_placeholder)}
              placeholder={t(($) => $.table.filter_placeholder)}
              className="h-7 pr-7 pl-7 text-caption"
            />
            {filter && (
              <Button
                type="button"
                variant="ghost"
                size="icon-xs"
                aria-label={t(($) => $.table.clear_filter)}
                onClick={() => setFilter("")}
                className="absolute top-0.5 right-0.5 text-muted-foreground"
              >
                <X className="size-3" />
              </Button>
            )}
          </div>
          <Button type="button" variant="outline" size="sm" onClick={() => copy("markdown", getMarkdown())}>
            {copied === "markdown" ? <Check /> : <Copy />}
            {t(($) => $.table.copy_markdown)}
          </Button>
          <Button
            type="button"
            variant="outline"
            size="sm"
            onClick={() => copy("csv", tableToCsv(model.headerText, model.rowText))}
          >
            {copied === "csv" ? <Check /> : <Copy />}
            {t(($) => $.table.copy_csv)}
          </Button>
          <DialogClose
            render={<Button variant="ghost" size="icon-sm" aria-label={t(($) => $.table.close)} />}
          >
            <X />
          </DialogClose>
        </div>
        <DataTable
          table={table}
          emptyMessage={t(($) => $.table.no_matches)}
          className="[&_td]:align-top"
        />
        <p className="shrink-0 border-t border-border px-4 py-1.5 text-caption tabular-nums text-muted-foreground">
          {filter && shownRows !== model.rows.length ? `${shownRows} / ` : null}
          {t(($) => $.attachment.table_rows, { count: model.rows.length })}
          {" · "}
          {t(($) => $.attachment.table_columns, { count: model.headerText.length })}
        </p>
      </DialogContent>
    </Dialog>
  );
}
