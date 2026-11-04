"use client";

/**
 * Structure editing for the editor's tables: a handle on the active column and
 * row, each opening its insert/delete menu, and "+" rails that append a column
 * or a row. Everything here is frame chrome (RICH_TABLE_CHROME_ATTRIBUTE), so
 * ProseMirror leaves its events to the browser.
 *
 * The first row is the Markdown header row and has no Markdown form without
 * it, so nothing here inserts above it or deletes it.
 */

import { useCallback, useEffect, useLayoutEffect, useRef, useState, type CSSProperties, type ReactNode } from "react";
import type { ChainedCommands, Editor } from "@tiptap/core";
import type { Node as ProseMirrorNode } from "@tiptap/pm/model";
import { TextSelection, type Transaction } from "@tiptap/pm/state";
import { useEditorState } from "@tiptap/react";
import { ArrowDown, ArrowLeft, ArrowRight, ArrowUp, GripHorizontal, GripVertical, Plus, Trash2 } from "lucide-react";
import {
  DropdownMenu,
  DropdownMenuContent,
  DropdownMenuItem,
  DropdownMenuSeparator,
  DropdownMenuTrigger,
} from "@multica/ui/components/ui/dropdown-menu";
import { cn } from "@multica/ui/lib/utils";
import { useT } from "../../i18n";
import { RICH_TABLE_CHROME_ATTRIBUTE } from "../../rich-content/rich-table";

export interface TableCell {
  row: number;
  column: number;
}

const CELL_TYPES = new Set(["tableCell", "tableHeader"]);

/** The cell holding the selection, when it is inside the table at `tablePos`. */
export function activeCellIn(editor: Editor, tablePos: number): TableCell | null {
  const { $from } = editor.state.selection;
  for (let depth = $from.depth; depth >= 3; depth--) {
    if (!CELL_TYPES.has($from.node(depth).type.name)) continue;
    if ($from.before(depth - 2) !== tablePos) return null;
    return { row: $from.index(depth - 2), column: $from.index(depth - 1) };
  }
  return null;
}

/** Puts the caret at the start of a cell; false when the cell does not exist. */
export function placeCaretInCell(tr: Transaction, tablePos: number, cell: TableCell): boolean {
  const table = tr.doc.nodeAt(tablePos);
  if (!table || cell.row >= table.childCount) return false;
  const row = table.child(cell.row);
  if (cell.column >= row.childCount) return false;
  let pos = tablePos + 1;
  for (let i = 0; i < cell.row; i++) pos += table.child(i).nodeSize;
  pos += 1;
  for (let i = 0; i < cell.column; i++) pos += row.child(i).nodeSize;
  tr.setSelection(TextSelection.near(tr.doc.resolve(pos + 1)));
  return true;
}

function sameCell(a: TableCell | null, b: TableCell | null): boolean {
  return a === b || (!!a && !!b && a.row === b.row && a.column === b.column);
}

interface Geometry {
  /** The table's visible box, relative to the overlay. */
  table: { top: number; left: number; width: number; height: number };
  /** Centre of the active column, or null while it is scrolled out of view. */
  columnX: number | null;
  /** Centre of the active row. */
  rowY: number | null;
}

function sameGeometry(a: Geometry | null, b: Geometry | null): boolean {
  if (a === b) return true;
  if (!a || !b) return false;
  return (
    a.columnX === b.columnX &&
    a.rowY === b.rowY &&
    a.table.top === b.table.top &&
    a.table.left === b.table.left &&
    a.table.width === b.table.width &&
    a.table.height === b.table.height
  );
}

function measure(overlay: HTMLElement, cell: TableCell | null): Geometry | null {
  const table = overlay.parentElement?.querySelector<HTMLTableElement>(".tableWrapper > table");
  const wrapper = table?.parentElement;
  if (!table || !wrapper) return null;
  const origin = overlay.getBoundingClientRect();
  const box = table.getBoundingClientRect();
  const view = wrapper.getBoundingClientRect();
  const left = Math.max(box.left, view.left);
  const right = Math.min(box.right, view.right);
  let columnX: number | null = null;
  let rowY: number | null = null;
  const rowElement = cell ? table.rows[cell.row] : undefined;
  const cellElement = cell ? rowElement?.cells[cell.column] : undefined;
  if (rowElement && cellElement) {
    const cellBox = cellElement.getBoundingClientRect();
    const centre = cellBox.left + cellBox.width / 2;
    if (centre >= view.left && centre <= view.right) columnX = Math.round(centre - origin.left);
    const rowBox = rowElement.getBoundingClientRect();
    rowY = Math.round(rowBox.top + rowBox.height / 2 - origin.top);
  }
  return {
    table: {
      top: Math.round(box.top - origin.top),
      left: Math.round(left - origin.left),
      width: Math.round(right - left),
      height: Math.round(box.height),
    },
    columnX,
    rowY,
  };
}

interface HandleMenuProps {
  editor: Editor;
  label: string;
  icon: ReactNode;
  className: string;
  style: CSSProperties;
  onOpenChange: (open: boolean) => void;
  children: ReactNode;
}

function HandleMenu({ editor, label, icon, className, style, onOpenChange, children }: HandleMenuProps) {
  return (
    <DropdownMenu onOpenChange={onOpenChange}>
      <DropdownMenuTrigger
        render={
          <button
            type="button"
            aria-label={label}
            title={label}
            className={cn(
              "pointer-events-auto absolute z-10 flex items-center justify-center rounded-sm bg-brand text-brand-foreground shadow-sm transition-colors hover:bg-brand/90 focus-visible:outline-none focus-visible:ring-2 focus-visible:ring-ring",
              className,
            )}
            style={style}
          >
            {icon}
          </button>
        }
      />
      {/* Closing hands focus back to the editor, where the caret still is. */}
      <DropdownMenuContent className="w-auto min-w-44" finalFocus={() => (editor.isDestroyed ? false : (editor.view.dom as HTMLElement))}>
        {children}
      </DropdownMenuContent>
    </DropdownMenu>
  );
}

export interface TableEditControlsProps {
  editor: Editor;
  node: ProseMirrorNode;
  getPos: () => number | undefined;
}

export function TableEditControls({ editor, node, getPos }: TableEditControlsProps) {
  const { t } = useT("editor");
  const overlayRef = useRef<HTMLDivElement>(null);

  const state = useEditorState({
    editor,
    selector: ({ editor: current }) => {
      if (!current.isEditable) return { editable: false, focused: false, cell: null };
      const pos = getPos();
      const cell = typeof pos === "number" ? activeCellIn(current, pos) : null;
      return { editable: true, focused: current.isFocused, cell };
    },
    equalityFn: (a, b) =>
      a?.editable === b?.editable && a?.focused === b?.focused && sameCell(a?.cell ?? null, b?.cell ?? null),
  });

  // Pressing a handle moves focus off the editor, so the handles stay mounted
  // while the selection is in the table and only fade with focus. An open
  // menu keeps its handle on the cell it was opened for.
  const [pinned, setPinned] = useState<TableCell | null>(null);
  const cell = pinned ?? state?.cell ?? null;
  const editable = state?.editable ?? false;
  const handlesShown = Boolean(state?.focused || pinned);

  const [geometry, setGeometry] = useState<Geometry | null>(null);
  const sync = useCallback(() => {
    const overlay = overlayRef.current;
    if (!overlay) return;
    const next = measure(overlay, cell);
    setGeometry((prev) => (sameGeometry(prev, next) ? prev : next));
  }, [cell]);

  useLayoutEffect(sync, [sync, node]);
  useEffect(() => {
    const overlay = overlayRef.current;
    const table = overlay?.parentElement?.querySelector<HTMLTableElement>(".tableWrapper > table");
    const wrapper = table?.parentElement;
    if (!editable || !table || !wrapper) return;
    wrapper.addEventListener("scroll", sync, { passive: true });
    const observer = typeof ResizeObserver === "undefined" ? null : new ResizeObserver(sync);
    observer?.observe(table);
    observer?.observe(wrapper);
    return () => {
      wrapper.removeEventListener("scroll", sync);
      observer?.disconnect();
    };
  }, [editable, sync]);

  if (!editable) return null;

  const rowCount = node.childCount;
  const columnCount = node.firstChild?.childCount ?? 0;

  // Runs a table command from `from`, then leaves the caret in `to`.
  const run = (
    from: TableCell,
    apply: (chain: ChainedCommands) => ChainedCommands,
    to?: TableCell,
  ) => {
    const tablePos = getPos();
    if (typeof tablePos !== "number") return;
    let chain = editor.chain().command(({ tr }) => placeCaretInCell(tr, tablePos, from));
    chain = apply(chain);
    if (to) chain = chain.command(({ tr }) => placeCaretInCell(tr, tablePos, to) || true);
    chain.scrollIntoView().run();
  };

  const appendColumn = () => {
    const from = { row: cell?.row ?? 0, column: columnCount - 1 };
    run(from, (chain) => chain.addColumnAfter(), { row: from.row, column: columnCount });
    editor.view.focus();
  };
  const appendRow = () => {
    run({ row: rowCount - 1, column: 0 }, (chain) => chain.addRowAfter(), { row: rowCount, column: 0 });
    editor.view.focus();
  };

  const pin = (open: boolean) => setPinned(open ? cell : null);
  const chrome = { [RICH_TABLE_CHROME_ATTRIBUTE]: "", contentEditable: false } as const;
  const rail =
    "pointer-events-auto absolute flex items-center justify-center rounded-sm bg-muted text-muted-foreground opacity-0 transition-opacity hover:bg-accent hover:text-foreground focus-visible:opacity-100 focus-visible:outline-none focus-visible:ring-2 focus-visible:ring-ring group-hover/table-node:opacity-100 [@media(hover:none)]:opacity-100";
  const box = geometry?.table;
  const header = cell?.row === 0;
  const handleVisibility = handlesShown
    ? "opacity-100"
    : "opacity-0 group-hover/table-node:opacity-100 focus-visible:opacity-100";

  return (
    <div ref={overlayRef} {...chrome} className="pointer-events-none absolute inset-0">
      {cell && geometry?.columnX != null && box && (
        <HandleMenu
          editor={editor}
          label={t(($) => $.table_edit.column_options)}
          icon={<GripHorizontal className="size-3" />}
          className={cn("h-3.5 w-6", handleVisibility)}
          style={{ left: geometry.columnX - 12, top: box.top - 7 }}
          onOpenChange={pin}
        >
          <DropdownMenuItem onClick={() => run(cell, (c) => c.addColumnBefore(), cell)}>
            <ArrowLeft />
            {t(($) => $.table_edit.insert_column_left)}
          </DropdownMenuItem>
          <DropdownMenuItem
            onClick={() => run(cell, (c) => c.addColumnAfter(), { row: cell.row, column: cell.column + 1 })}
          >
            <ArrowRight />
            {t(($) => $.table_edit.insert_column_right)}
          </DropdownMenuItem>
          <DropdownMenuItem
            variant="destructive"
            disabled={columnCount <= 1}
            onClick={() => run(cell, (c) => c.deleteColumn())}
          >
            <Trash2 />
            {t(($) => $.table_edit.delete_column)}
          </DropdownMenuItem>
          <DropdownMenuSeparator />
          <DropdownMenuItem variant="destructive" onClick={() => run(cell, (c) => c.deleteTable())}>
            <Trash2 />
            {t(($) => $.table_edit.delete_table)}
          </DropdownMenuItem>
        </HandleMenu>
      )}
      {cell && geometry?.rowY != null && box && (
        <HandleMenu
          editor={editor}
          label={t(($) => $.table_edit.row_options)}
          icon={<GripVertical className="size-3" />}
          className={cn("h-6 w-3.5", handleVisibility)}
          style={{ left: box.left - 7, top: geometry.rowY - 12 }}
          onOpenChange={pin}
        >
          <DropdownMenuItem disabled={header} onClick={() => run(cell, (c) => c.addRowBefore(), cell)}>
            <ArrowUp />
            {t(($) => $.table_edit.insert_row_above)}
          </DropdownMenuItem>
          <DropdownMenuItem
            onClick={() => run(cell, (c) => c.addRowAfter(), { row: cell.row + 1, column: cell.column })}
          >
            <ArrowDown />
            {t(($) => $.table_edit.insert_row_below)}
          </DropdownMenuItem>
          <DropdownMenuItem
            variant="destructive"
            disabled={header}
            onClick={() => run(cell, (c) => c.deleteRow())}
          >
            <Trash2 />
            {t(($) => $.table_edit.delete_row)}
          </DropdownMenuItem>
          <DropdownMenuSeparator />
          <DropdownMenuItem variant="destructive" onClick={() => run(cell, (c) => c.deleteTable())}>
            <Trash2 />
            {t(($) => $.table_edit.delete_table)}
          </DropdownMenuItem>
        </HandleMenu>
      )}
      {box && (
        <>
          <button
            type="button"
            aria-label={t(($) => $.table_edit.add_column)}
            title={t(($) => $.table_edit.add_column)}
            className={cn(rail, "w-3.5")}
            style={{ left: box.left + box.width + 4, top: box.top, height: box.height }}
            onMouseDown={(event) => event.preventDefault()}
            onClick={appendColumn}
          >
            <Plus className="size-3" />
          </button>
          <button
            type="button"
            aria-label={t(($) => $.table_edit.add_row)}
            title={t(($) => $.table_edit.add_row)}
            className={cn(rail, "h-3.5")}
            style={{ left: box.left, top: box.top + box.height + 4, width: box.width }}
            onMouseDown={(event) => event.preventDefault()}
            onClick={appendRow}
          >
            <Plus className="size-3" />
          </button>
        </>
      )}
    </div>
  );
}
