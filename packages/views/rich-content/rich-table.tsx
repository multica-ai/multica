"use client";

/**
 * Markdown tables: column resizing, an expanded view and copy. One frame
 * serves both renderers — RichContent's readonly tables and the editor's
 * table node view, whose rows ProseMirror owns — so everything it adds sits
 * outside the rows: the toolbar, a layer of resize handles over the column
 * edges, and the column widths on the `<table>` itself.
 *
 * The table keeps the browser's automatic layout while its columns fit (see
 * `overflowWidths` for when they do not). A viewer's first drag pins every
 * column at the width it renders at and switches to fixed layout, so only the
 * dragged column moves. Those widths are the viewer's own (see
 * `writeColumnWidths`) and never touch the content.
 */

import {
  createContext,
  memo,
  useCallback,
  useContext,
  useEffect,
  useLayoutEffect,
  useMemo,
  useRef,
  useState,
  type ComponentPropsWithoutRef,
  type KeyboardEvent as ReactKeyboardEvent,
  type PointerEvent as ReactPointerEvent,
  type ReactNode,
} from "react";
import { createPortal } from "react-dom";
import type { ExtraProps } from "react-markdown";
import { Check, Copy, Maximize2, RotateCcw } from "lucide-react";
import { copyText } from "@multica/ui/lib/clipboard";
import { cn } from "@multica/ui/lib/utils";
import { useT } from "../i18n";
import { RichTableDialog } from "./rich-table-dialog";
import {
  columnWidthKey,
  readColumnWidths,
  readTableModel,
  tableMarkdownFromSource,
  writeColumnWidths,
  type RichTableModel,
} from "./rich-table-model";

export interface RichTableSource {
  /** The content as written. */
  source: string;
  /** The preprocessed copy RichContent rendered, which node offsets index into. */
  rendered: string;
}

export const RichTableSourceContext = createContext<RichTableSource>({ source: "", rendered: "" });

/** Marks the frame's own controls, which the editor must leave to the browser. */
export const RICH_TABLE_CHROME_ATTRIBUTE = "data-rich-table-chrome";

const RESIZE_DRAG_THRESHOLD = 4;
const MIN_COLUMN_WIDTH = 64;
const MAX_COLUMN_WIDTH = 1200;
// A double-click fits the content, but a paragraph-long cell would otherwise
// claim its full single-line width.
const MAX_AUTO_FIT_WIDTH = 480;
const KEYBOARD_STEP = 8;
const KEYBOARD_STEP_LARGE = 32;
const HANDLE_WIDTH = 8;
const SCROLL_SHADOW = "color-mix(in oklab, var(--foreground) 8%, transparent)";

function clampWidth(width: number, max = MAX_COLUMN_WIDTH): number {
  return Math.min(max, Math.max(MIN_COLUMN_WIDTH, Math.round(width)));
}

function sum(widths: number[]): number {
  return widths.reduce((total, width) => total + width, 0);
}

function sameWidths(a: number[] | null, b: number[] | null): boolean {
  return a === b || (!!a && !!b && a.length === b.length && a.every((w, i) => w === b[i]));
}

// The editor keeps its header row in <tbody>, so the first row stands for the
// columns in both renderers.
function headerCells(table: HTMLTableElement): HTMLTableCellElement[] {
  return Array.from(table.rows[0]?.cells ?? []);
}

// Each column's min-content and max-content width, read with the table's own
// sizing lifted and restored within the same task, so the browser paints only
// the restored layout. Only the <table> and its <col>s are touched: in the
// editor, a style written to a cell would reach ProseMirror as an edit.
function measureColumns(table: HTMLTableElement): { min: number[]; max: number[] } {
  const { style } = table;
  const saved = { layout: style.tableLayout, width: style.width, minWidth: style.minWidth };
  const fixed = table.hasAttribute("data-column-widths");
  const cols = Array.from(table.querySelectorAll<HTMLElement>("colgroup > col"));
  const colWidths = cols.map((col) => col.style.width);
  const read = () => headerCells(table).map((cell) => Math.ceil(cell.getBoundingClientRect().width));

  if (fixed) table.removeAttribute("data-column-widths");
  for (const col of cols) col.style.width = "";
  style.tableLayout = "auto";
  style.minWidth = "0";
  style.width = "max-content";
  const max = read();
  style.width = "min-content";
  const min = read();

  style.tableLayout = saved.layout;
  style.width = saved.width;
  style.minWidth = saved.minWidth;
  cols.forEach((col, i) => {
    col.style.width = colWidths[i] ?? "";
  });
  if (fixed) table.setAttribute("data-column-widths", "");
  return { min, max };
}

// The browser's automatic layout reads well while the columns' minimums fit.
// Once they do not, it shrinks every column to its minimum, which leaves a
// prose column a few characters wide beside a column of unbreakable paths.
// Past that point each column gets its content width, capped so one long cell
// cannot claim the table, and the table scrolls.
const PREFERRED_MAX_COLUMN_WIDTH = 320;

function overflowWidths(table: HTMLTableElement, available: number): number[] | null {
  const { min, max } = measureColumns(table);
  if (min.length === 0 || sum(min) <= available) return null;
  return min.map((least, i) => Math.max(least, Math.min(max[i] ?? least, PREFERRED_MAX_COLUMN_WIDTH)));
}

interface ColumnEdges {
  /** Left offset of each column's handle within the wrapper's scroll area. */
  lefts: number[];
  top: number;
  headerHeight: number;
}

const NO_EDGES: ColumnEdges = { lefts: [], top: 0, headerHeight: 0 };

function sameEdges(a: ColumnEdges, b: ColumnEdges): boolean {
  return (
    a.top === b.top &&
    a.headerHeight === b.headerHeight &&
    a.lefts.length === b.lefts.length &&
    a.lefts.every((left, i) => left === b.lefts[i])
  );
}

function measureEdges(wrapper: HTMLElement, table: HTMLTableElement): ColumnEdges {
  const cells = headerCells(table);
  if (cells.length === 0) return NO_EDGES;
  const box = wrapper.getBoundingClientRect();
  const originX = box.left + wrapper.clientLeft - wrapper.scrollLeft;
  const originY = box.top + wrapper.clientTop - wrapper.scrollTop;
  const row = cells[0]!.parentElement!.getBoundingClientRect();
  return {
    // Centred on the edge, except the last, which stays inside the scroll box:
    // overhanging it would add a few pixels of horizontal scroll to every table.
    lefts: cells.map((cell, i) => {
      const right = Math.round(cell.getBoundingClientRect().right - originX);
      return i === cells.length - 1 ? right - HANDLE_WIDTH : right - HANDLE_WIDTH / 2;
    }),
    top: Math.round(row.top - originY),
    headerHeight: Math.round(row.height),
  };
}

export interface RichTableFrameProps {
  model: RichTableModel;
  /** The table's Markdown, read when it is copied. */
  getMarkdown: () => string;
  /** Receives the `<table>`, for rows rendered outside React. */
  tableRef?: (table: HTMLTableElement | null) => void;
  children?: ReactNode;
}

export function RichTableFrame({ model, getMarkdown, tableRef: onTable, children }: RichTableFrameProps) {
  const { t } = useT("editor");
  const columnCount = model.headerText.length;
  const storageKey = useMemo(() => columnWidthKey(model), [model]);

  const [widths, setWidths] = useState<number[] | null>(null);
  const [dragging, setDragging] = useState(false);
  const [expanded, setExpanded] = useState(false);
  const [copied, setCopied] = useState(false);
  const tableRef = useRef<HTMLTableElement | null>(null);
  const wrapperRef = useRef<HTMLDivElement>(null);
  const setTable = useCallback(
    (table: HTMLTableElement | null) => {
      tableRef.current = table;
      onTable?.(table);
    },
    [onTable],
  );
  const widthsRef = useRef(widths);
  useLayoutEffect(() => {
    widthsRef.current = widths;
  }, [widths]);

  // Layout effect: stored widths apply before the first paint, so a resized
  // table does not flash at its automatic layout on every mount. A table being
  // edited changes its key with every keystroke; its widths move to the new
  // key as long as the columns stay the same.
  const keyRef = useRef(storageKey);
  useLayoutEffect(() => {
    const previousKey = keyRef.current;
    keyRef.current = storageKey;
    const stored = readColumnWidths(storageKey, columnCount);
    const carried =
      !stored && previousKey !== storageKey && widthsRef.current?.length === columnCount
        ? widthsRef.current
        : null;
    if (carried) {
      writeColumnWidths(previousKey, null);
      writeColumnWidths(storageKey, carried);
    }
    setWidths(stored ?? carried);
  }, [storageKey, columnCount]);

  const [autoWidths, setAutoWidths] = useState<number[] | null>(null);
  useLayoutEffect(() => {
    const wrapper = wrapperRef.current;
    const table = tableRef.current;
    if (!wrapper || !table || widths) return;
    let measuredAt = -1;
    let active = true;
    const fit = () => {
      const available = wrapper.clientWidth;
      if (!active || available === measuredAt) return;
      measuredAt = available;
      const next = overflowWidths(table, available);
      setAutoWidths((prev) => (sameWidths(prev, next) ? prev : next));
    };
    fit();
    const observer = typeof ResizeObserver === "undefined" ? null : new ResizeObserver(fit);
    observer?.observe(wrapper);
    // Content widths change when a web font replaces its fallback.
    void document.fonts?.ready.then(() => {
      measuredAt = -1;
      fit();
    });
    return () => {
      active = false;
      observer?.disconnect();
    };
  }, [widths, model]);

  const userWidths = widths?.length === columnCount ? widths : null;
  const resized = userWidths !== null;
  const columnWidths = userWidths ?? (autoWidths?.length === columnCount ? autoWidths : null);

  const [edges, setEdges] = useState(NO_EDGES);
  const syncEdges = useCallback(() => {
    const wrapper = wrapperRef.current;
    const table = tableRef.current;
    if (!wrapper || !table) return;
    const next = measureEdges(wrapper, table);
    setEdges((prev) => (sameEdges(prev, next) ? prev : next));
  }, []);
  useLayoutEffect(syncEdges, [syncEdges, columnWidths, model]);
  useEffect(() => {
    const table = tableRef.current;
    if (!table || typeof ResizeObserver === "undefined") return;
    const observer = new ResizeObserver(syncEdges);
    observer.observe(table);
    return () => observer.disconnect();
  }, [syncEdges]);

  const [overflow, setOverflow] = useState({ start: false, end: false });
  useEffect(() => {
    const wrapper = wrapperRef.current;
    if (!wrapper) return;
    const sync = () => {
      const start = wrapper.scrollLeft > 0;
      const end = wrapper.scrollLeft + wrapper.clientWidth < wrapper.scrollWidth - 1;
      setOverflow((prev) => (prev.start === start && prev.end === end ? prev : { start, end }));
    };
    sync();
    wrapper.addEventListener("scroll", sync, { passive: true });
    const observer = typeof ResizeObserver === "undefined" ? null : new ResizeObserver(sync);
    observer?.observe(wrapper);
    if (tableRef.current) observer?.observe(tableRef.current);
    return () => {
      wrapper.removeEventListener("scroll", sync);
      observer?.disconnect();
    };
  }, []);

  const resize = useMemo(() => {
    const renderedWidths = (): number[] => {
      const table = tableRef.current;
      return table ? headerCells(table).map((cell) => clampWidth(cell.getBoundingClientRect().width)) : [];
    };
    const baseWidths = () => widthsRef.current ?? renderedWidths();
    const commit = (next: number[]) => {
      setWidths(next);
      writeColumnWidths(storageKey, next);
    };
    const columnOf = (handle: HTMLElement) => Number(handle.dataset.column ?? -1);

    return {
      begin(event: ReactPointerEvent<HTMLElement>) {
        const handle = event.currentTarget;
        const index = columnOf(handle);
        if (index < 0) return;
        event.preventDefault();
        event.stopPropagation();

        const startX = event.clientX;
        const { pointerId } = event;
        const table = tableRef.current;
        const badge = handle.querySelector<HTMLElement>("[data-width-badge]");
        const restingHeight = handle.style.height;
        let base: number[] | null = null;
        let latest: number[] | null = null;

        const move = (pointerEvent: PointerEvent) => {
          const delta = pointerEvent.clientX - startX;
          if (!base && Math.abs(delta) < RESIZE_DRAG_THRESHOLD) return;
          if (!base) {
            base = baseWidths();
            handle.dataset.resizing = "";
            if (table) handle.style.height = `${table.offsetHeight}px`;
            setDragging(true);
          }
          const width = clampWidth((base[index] ?? 0) + delta);
          latest = base.map((w, i) => (i === index ? width : w));
          if (badge) badge.textContent = `${width}px`;
          setWidths(latest);
        };
        const stop = () => {
          window.removeEventListener("pointermove", move);
          window.removeEventListener("pointerup", stop);
          window.removeEventListener("pointercancel", stop);
          window.removeEventListener("blur", stop);
          handle.removeEventListener("lostpointercapture", stop);
          if (handle.hasPointerCapture?.(pointerId)) handle.releasePointerCapture?.(pointerId);
          delete handle.dataset.resizing;
          handle.style.height = restingHeight;
          setDragging(false);
          if (latest) commit(latest);
        };

        window.addEventListener("pointermove", move);
        window.addEventListener("pointerup", stop);
        window.addEventListener("pointercancel", stop);
        // Releasing outside the window never delivers pointerup.
        window.addEventListener("blur", stop);
        handle.addEventListener("lostpointercapture", stop);
        handle.setPointerCapture?.(pointerId);
      },

      autoFit(handle: HTMLElement) {
        const table = tableRef.current;
        const index = columnOf(handle);
        if (!table || index < 0) return;
        const base = baseWidths();
        const widest = measureColumns(table).max[index] ?? 0;
        if (widest > 0) {
          commit(base.map((w, i) => (i === index ? clampWidth(widest, MAX_AUTO_FIT_WIDTH) : w)));
        }
      },

      nudge(event: ReactKeyboardEvent<HTMLElement>) {
        if (event.key !== "ArrowLeft" && event.key !== "ArrowRight") return;
        const index = columnOf(event.currentTarget);
        if (index < 0) return;
        event.preventDefault();
        const step = event.shiftKey ? KEYBOARD_STEP_LARGE : KEYBOARD_STEP;
        const delta = event.key === "ArrowRight" ? step : -step;
        commit(baseWidths().map((w, i) => (i === index ? clampWidth(w + delta) : w)));
      },
    };
  }, [storageKey]);

  const resetWidths = () => {
    setWidths(null);
    writeColumnWidths(storageKey, null);
  };

  const copyMarkdown = async () => {
    if (await copyText(getMarkdown())) {
      setCopied(true);
      setTimeout(() => setCopied(false), 2000);
    }
  };

  const chrome = { [RICH_TABLE_CHROME_ATTRIBUTE]: "", contentEditable: false } as const;
  const toolbarButton =
    "flex h-6 items-center justify-center gap-1 rounded-sm text-muted-foreground transition-colors hover:bg-muted hover:text-foreground focus-visible:outline-none focus-visible:ring-2 focus-visible:ring-ring";

  return (
    <div className="rich-table group/table relative" data-resized={resized ? "" : undefined}>
      {/* Above the table, inside its top margin: over the table it would cover
          the last columns' headers and resize handles, and in the flow it
          would make the space above the table taller than the space below. */}
      <div
        {...chrome}
        className="rich-table-toolbar absolute right-0 bottom-full flex h-6 items-center gap-0.5 opacity-0 transition-opacity group-hover/table:opacity-100 focus-within:opacity-100 [@media(hover:none)]:opacity-100"
      >
        {resized && (
          <button type="button" onClick={resetWidths} className={cn(toolbarButton, "px-1.5 text-caption")}>
            <RotateCcw className="size-3.5" />
            {t(($) => $.table.reset_widths)}
          </button>
        )}
        <button
          type="button"
          onClick={() => setExpanded(true)}
          className={cn(toolbarButton, "w-6")}
          title={t(($) => $.table.expand)}
          aria-label={t(($) => $.table.expand)}
        >
          <Maximize2 className="size-3.5" />
        </button>
        <button
          type="button"
          onClick={copyMarkdown}
          className={cn(toolbarButton, "w-6")}
          title={copied ? t(($) => $.table.copied) : t(($) => $.table.copy_markdown)}
          aria-label={t(($) => $.table.copy_markdown)}
        >
          {copied ? <Check className="size-3.5" /> : <Copy className="size-3.5" />}
        </button>
      </div>
      <div className="relative">
        <div ref={wrapperRef} className="tableWrapper">
          <table
            ref={setTable}
            data-column-widths={columnWidths ? "" : undefined}
            style={columnWidths ? { width: sum(columnWidths) } : undefined}
          >
            {/* Always rendered, so it stays ahead of rows appended outside React. */}
            <colgroup>
              {columnWidths?.map((width, index) => (
                <col key={index} style={{ width }} />
              ))}
            </colgroup>
            {children}
          </table>
          {model.headerText.map((name, index) => (
            <div
              key={index}
              {...chrome}
              role="separator"
              aria-orientation="vertical"
              aria-label={t(($) => $.table.resize_column, { name })}
              tabIndex={0}
              data-column={index}
              data-edge={index === columnCount - 1 ? "end" : undefined}
              className="rich-table-resize-handle"
              style={{ left: edges.lefts[index] ?? 0, top: edges.top, height: edges.headerHeight || undefined }}
              onPointerDown={resize.begin}
              onDoubleClick={(event) => {
                event.preventDefault();
                resize.autoFit(event.currentTarget);
              }}
              onKeyDown={resize.nudge}
            >
              <span data-width-badge="" aria-hidden />
            </div>
          ))}
        </div>
        {/* Columns continue past the edge: a shadow, not a fade to a background
            colour, because the same table renders on cards, pages and chat. */}
        {overflow.start && (
          <div
            aria-hidden
            className="pointer-events-none absolute inset-y-px left-px w-3 rounded-l-[var(--radius)]"
            style={{ background: `linear-gradient(to right, ${SCROLL_SHADOW}, transparent)` }}
          />
        )}
        {overflow.end && (
          <div
            aria-hidden
            className="pointer-events-none absolute inset-y-px right-px w-3 rounded-r-[var(--radius)]"
            style={{ background: `linear-gradient(to left, ${SCROLL_SHADOW}, transparent)` }}
          />
        )}
      </div>
      {expanded && (
        <RichTableDialog model={model} open={expanded} onOpenChange={setExpanded} getMarkdown={getMarkdown} />
      )}
      {/* Owns the cursor for the whole drag, so cells that declare their own
          (links, text) cannot override it, and keeps text unselectable. */}
      {dragging &&
        typeof document !== "undefined" &&
        createPortal(
          <div aria-hidden className="fixed inset-0 z-50 cursor-col-resize touch-none select-none" />,
          document.body,
        )}
    </div>
  );
}

// Rendered children of the table. Memoized on their identity so a drag, which
// re-renders the frame every frame for its widths, never re-renders the cells.
const TableContent = memo(function TableContent({ children }: { children: ReactNode }) {
  return <>{children}</>;
});

type RichTableProps = ComponentPropsWithoutRef<"table"> & ExtraProps;

/** react-markdown's `table` override. */
export function RichTable({ children, node }: RichTableProps) {
  const { source, rendered } = useContext(RichTableSourceContext);
  const model = useMemo(() => readTableModel(children), [children]);
  const getMarkdown = () =>
    tableMarkdownFromSource(source, rendered, node?.position?.start.offset, node?.position?.end.offset);
  return (
    <RichTableFrame model={model} getMarkdown={getMarkdown}>
      <TableContent>{children}</TableContent>
    </RichTableFrame>
  );
}
