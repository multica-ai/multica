"use client";

import {
  useCallback,
  useEffect,
  useId,
  useMemo,
  useRef,
  useState,
  type CSSProperties,
  type KeyboardEvent as ReactKeyboardEvent,
  type MouseEvent as ReactMouseEvent,
  type ReactNode,
} from "react";
import { useVirtualizer } from "@tanstack/react-virtual";
import { ArrowDown, ArrowUp, Loader2 } from "@multica/ui/icons";
import { Button } from "@multica/ui/components/ui/button";
import { Checkbox } from "@multica/ui/components/ui/checkbox";
import { Skeleton } from "@multica/ui/components/ui/skeleton";
import { cn } from "@multica/ui/lib/utils";
import {
  isEditableShortcutTarget,
  isPortalLayerShortcutTarget,
} from "@multica/core/shortcuts";
import { useModalStore } from "@multica/core/modals";
import { useIntentNavigate, useRowLink } from "../../navigation";
import { useT } from "../../i18n";
import { CollectionRowContext, type RowMenuState } from "./row-context";

export type CollectionSortDirection = "asc" | "desc";

export interface CollectionColumn<T> {
  key: string;
  /** Header label; also the column's name in the Display menu. */
  header?: string;
  /** Track width in px. The first column is the primary column: it takes the
   *  remaining space, and `width` is its minimum on wide layouts. */
  width?: number;
  align?: "start" | "end";
  /** Sort field this column's header toggles. */
  sortKey?: string;
  /** Stays visible below the wide breakpoint (the primary column always does). */
  core?: boolean;
  /** Listed in the Display menu's column switches. */
  hideable?: boolean;
  /** Holds its own controls: clicks inside never activate the row. */
  interactive?: boolean;
  className?: string;
  cell: (row: T) => ReactNode;
}

export interface CollectionSort<S extends string = string> {
  field: S;
  direction: CollectionSortDirection;
  /** Header click: flips direction on the active field, else switches field. */
  onToggle: (field: S) => void;
  onFieldChange?: (field: S) => void;
  onDirectionChange?: (direction: CollectionSortDirection) => void;
}

export interface CollectionSelection<T> {
  selectedIds: ReadonlySet<string>;
  onChange: (next: ReadonlySet<string>) => void;
  isSelectable?: (row: T) => boolean;
  /** Accessible name of a row's checkbox; defaults to "Select row". */
  getLabel?: (row: T) => string;
  /** Accessible name of the header checkbox; defaults to "Select all". */
  allLabel?: string;
}

export interface CollectionGroup<T> {
  key: string;
  label: ReactNode;
  count?: number;
  actions?: ReactNode;
  rows: readonly T[];
}

export interface CollectionListProps<T> {
  /** Accessible name of the grid. */
  label: string;
  columns: readonly CollectionColumn<T>[];
  hiddenColumns?: readonly string[];
  rows?: readonly T[];
  /** Grouped rows render as bands; takes precedence over `rows`. */
  groups?: readonly CollectionGroup<T>[];
  getRowId: (row: T) => string;
  getRowHref?: (row: T) => string | null | undefined;
  /** Tab title for new-tab opens. */
  getRowTitle?: (row: T) => string | undefined;
  /** Runs right before an in-place navigation to the row's href. */
  onRowNavigate?: (row: T) => void;
  /** Activation for rows without an href. */
  onRowOpen?: (row: T) => void;
  /** One menu for both the ⋯ button and right-click (see CollectionRowMenu). */
  rowMenu?: (row: T) => ReactNode;
  getRowClassName?: (row: T) => string | undefined;
  sort?: CollectionSort;
  selection?: CollectionSelection<T>;
  loading?: boolean;
  /** Shown in place of rows when there are none. */
  empty?: ReactNode;
  hasMore?: boolean;
  loadingMore?: boolean;
  onLoadMore?: () => void;
  /** Renders inside the page's own scroller instead of owning one. */
  embedded?: boolean;
  /** J/K and arrows reach the list while nothing else has focus. */
  pageShortcuts?: boolean;
  className?: string;
}

const EDGE = 4;
const SLOT = 16;
const MENU = 24;
const GAP = 12;
const PAD = 8;
const ROW_HEIGHT = 36;
const HEADER_HEIGHT = 36;
const GROUP_HEIGHT = 50;
const PRIMARY_MIN = 200;
const DEFAULT_WIDTH = 120;
const VIRTUALIZE_AFTER = 80;

const noop = () => {};

type Item<T> =
  | { kind: "group"; group: CollectionGroup<T> }
  | { kind: "row"; row: T; index: number };

function gridTemplate<T>(
  columns: readonly CollectionColumn<T>[],
  { wide, selectable, menu }: { wide: boolean; selectable: boolean; menu: boolean },
) {
  const tracks = [`${EDGE}px`];
  if (selectable && wide) tracks.push(`${SLOT}px`);
  columns.forEach((col, i) => {
    if (i === 0) tracks.push(wide ? `minmax(${col.width ?? PRIMARY_MIN}px,1fr)` : "minmax(0,1fr)");
    else if (wide || col.core) tracks.push(`${col.width ?? DEFAULT_WIDTH}px`);
  });
  if (menu) tracks.push(`${MENU}px`);
  tracks.push(`${EDGE}px`);
  return tracks;
}

function minWidth(tracks: string[]) {
  let px = PAD * 2 + GAP * (tracks.length - 1);
  for (const track of tracks) {
    const match = /^(?:minmax\()?(\d+)px/.exec(track);
    px += match ? Number(match[1]) : 0;
  }
  return px;
}

function isImeKey(e: KeyboardEvent | ReactKeyboardEvent) {
  const native = "nativeEvent" in e ? e.nativeEvent : e;
  return native.isComposing || native.keyCode === 229;
}

export function CollectionList<T>({
  label,
  columns: allColumns,
  hiddenColumns,
  rows: plainRows,
  groups,
  getRowId,
  getRowHref,
  getRowTitle,
  onRowNavigate,
  onRowOpen,
  rowMenu,
  getRowClassName,
  sort,
  selection,
  loading = false,
  empty,
  hasMore = false,
  loadingMore = false,
  onLoadMore,
  embedded = false,
  pageShortcuts = !embedded,
  className,
}: CollectionListProps<T>) {
  const { t } = useT("layout");
  const listId = useId();
  const rowLink = useRowLink();
  const intentNavigate = useIntentNavigate();
  const scrollRef = useRef<HTMLDivElement | null>(null);
  const gridRef = useRef<HTMLDivElement | null>(null);

  const columns = useMemo(
    () =>
      allColumns.filter(
        (col, i) => i === 0 || !col.hideable || !hiddenColumns?.includes(col.key),
      ),
    [allColumns, hiddenColumns],
  );

  const items = useMemo<Item<T>[]>(() => {
    const out: Item<T>[] = [];
    let index = 0;
    if (groups) {
      for (const group of groups) {
        out.push({ kind: "group", group });
        for (const row of group.rows) out.push({ kind: "row", row, index: index++ });
      }
    } else {
      for (const row of plainRows ?? []) out.push({ kind: "row", row, index: index++ });
    }
    return out;
  }, [groups, plainRows]);

  const flatRows = useMemo(
    () =>
      items.flatMap((item) => (item.kind === "row" ? [item.row] : [])),
    [items],
  );
  const rowIds = useMemo(() => flatRows.map(getRowId), [flatRows, getRowId]);
  const itemIndexById = useMemo(() => {
    const map = new Map<string, number>();
    items.forEach((item, i) => {
      if (item.kind === "row") map.set(getRowId(item.row), i);
    });
    return map;
  }, [items, getRowId]);

  const selectable = !!selection;
  const hasMenu = !!rowMenu;
  const wideTracks = gridTemplate(columns, { wide: true, selectable, menu: hasMenu });
  const narrowTracks = gridTemplate(columns, { wide: false, selectable, menu: hasMenu });
  const gridStyle = {
    "--cl-wide": wideTracks.join(" "),
    "--cl-narrow": narrowTracks.join(" "),
    "--cl-minw": `${minWidth(wideTracks)}px`,
  } as CSSProperties;

  const virtualize = !embedded && !loading && items.length > VIRTUALIZE_AFTER;
  const virtualizer = useVirtualizer({
    count: virtualize ? items.length : 0,
    getScrollElement: () => scrollRef.current,
    estimateSize: (i) => (items[i]?.kind === "group" ? GROUP_HEIGHT : ROW_HEIGHT),
    paddingStart: HEADER_HEIGHT,
    scrollPaddingStart: HEADER_HEIGHT,
    overscan: 12,
  });

  // Selection
  const anchorRef = useRef<string | null>(null);
  const selectedIds = selection?.selectedIds;
  const isSelectable = useCallback(
    (row: T) => selection?.isSelectable?.(row) ?? true,
    [selection],
  );
  const selectableIds = useMemo(
    () => (selection ? flatRows.filter(isSelectable).map(getRowId) : []),
    [selection, flatRows, isSelectable, getRowId],
  );
  const selectedVisible = selectedIds
    ? selectableIds.filter((id) => selectedIds.has(id)).length
    : 0;
  const allSelected = selectableIds.length > 0 && selectedVisible === selectableIds.length;
  const anySelected = (selectedIds?.size ?? 0) > 0;

  const toggleRow = useCallback(
    (id: string, range: boolean) => {
      if (!selection) return;
      const next = new Set(selection.selectedIds);
      const anchor = anchorRef.current;
      if (range && anchor && anchor !== id) {
        const a = rowIds.indexOf(anchor);
        const b = rowIds.indexOf(id);
        if (a >= 0 && b >= 0) {
          const [from, to] = a < b ? [a, b] : [b, a];
          for (let i = from; i <= to; i++) {
            const rowId = rowIds[i];
            const row = flatRows[i];
            if (rowId && row && isSelectable(row)) next.add(rowId);
          }
          selection.onChange(next);
          return;
        }
      }
      if (next.has(id)) next.delete(id);
      else next.add(id);
      anchorRef.current = id;
      selection.onChange(next);
    },
    [selection, rowIds, flatRows, isSelectable],
  );

  const toggleAll = useCallback(() => {
    if (!selection) return;
    selection.onChange(allSelected ? new Set() : new Set(selectableIds));
  }, [selection, allSelected, selectableIds]);

  // Keyboard
  const [activeId, setActiveId] = useState<string | null>(null);
  const [showActive, setShowActive] = useState(false);
  const activeIndex = activeId ? rowIds.indexOf(activeId) : -1;

  const scrollToRow = useCallback(
    (id: string) => {
      if (virtualize) {
        const itemIndex = itemIndexById.get(id);
        if (itemIndex !== undefined) virtualizer.scrollToIndex(itemIndex, { align: "auto" });
        return;
      }
      const el = document.getElementById(`${listId}-row-${id}`);
      el?.scrollIntoView?.({ block: "nearest" });
    },
    [virtualize, itemIndexById, virtualizer, listId],
  );

  const openRow = useCallback(
    (row: T, newTab: boolean) => {
      const href = getRowHref?.(row);
      if (href) {
        if (!newTab) onRowNavigate?.(row);
        intentNavigate(href, newTab ? "foreground-tab" : "push", getRowTitle?.(row));
        return;
      }
      onRowOpen?.(row);
    },
    [getRowHref, getRowTitle, intentNavigate, onRowNavigate, onRowOpen],
  );

  const handleKey = useCallback(
    (e: KeyboardEvent | ReactKeyboardEvent, onGrid: boolean): boolean => {
      const mod = e.metaKey || e.ctrlKey;
      const n = rowIds.length;
      const move = (delta: number) => {
        if (n === 0) return;
        const from = activeIndex < 0 ? (delta > 0 ? -1 : n) : activeIndex;
        const next = Math.min(n - 1, Math.max(0, from + delta));
        const id = rowIds[next]!;
        if (e.shiftKey && selection) {
          const fromId = activeId ?? id;
          if (!anchorRef.current) anchorRef.current = fromId;
          const a = rowIds.indexOf(anchorRef.current);
          const [lo, hi] = a < next ? [a, next] : [next, a];
          const nextSel = new Set(selection.selectedIds);
          for (let i = lo; i <= hi; i++) {
            const row = flatRows[i];
            if (row && isSelectable(row)) nextSel.add(rowIds[i]!);
          }
          selection.onChange(nextSel);
        }
        setActiveId(id);
        setShowActive(true);
        scrollToRow(id);
      };
      if (e.altKey) return false;
      switch (e.key) {
        case "ArrowDown":
        case "j":
        case "J":
          if (mod) return false;
          move(1);
          return true;
        case "ArrowUp":
        case "k":
        case "K":
          if (mod) return false;
          move(-1);
          return true;
        case "Home":
          if (n === 0) return false;
          move(-n);
          return true;
        case "End":
          if (n === 0) return false;
          move(n);
          return true;
        case "Enter": {
          if (!onGrid || activeIndex < 0) return false;
          const row = flatRows[activeIndex];
          if (!row) return false;
          openRow(row, mod);
          return true;
        }
        case "x":
        case "X": {
          if (mod || e.shiftKey || !selection || activeIndex < 0) return false;
          const row = flatRows[activeIndex];
          if (!row || !isSelectable(row)) return false;
          toggleRow(rowIds[activeIndex]!, false);
          setShowActive(true);
          return true;
        }
        case "a":
        case "A":
          if (!mod || !selection || selectableIds.length === 0) return false;
          selection.onChange(new Set(selectableIds));
          return true;
        case "Escape":
          if (selection && selection.selectedIds.size > 0) {
            selection.onChange(new Set());
            anchorRef.current = null;
            return true;
          }
          if (activeId) {
            setActiveId(null);
            return true;
          }
          return false;
        default:
          return false;
      }
    },
    [
      rowIds,
      activeIndex,
      activeId,
      selection,
      flatRows,
      isSelectable,
      scrollToRow,
      openRow,
      toggleRow,
      selectableIds,
    ],
  );

  const onGridKeyDown = (e: ReactKeyboardEvent<HTMLDivElement>) => {
    if (e.defaultPrevented || isImeKey(e)) return;
    const target = e.target as Node;
    if (!e.currentTarget.contains(target)) return;
    if (isEditableShortcutTarget(e.target)) return;
    const onGrid = e.target === e.currentTarget;
    if (handleKey(e, onGrid)) e.preventDefault();
  };

  const focusGrid = () => gridRef.current?.focus({ preventScroll: true });

  const handleKeyRef = useRef(handleKey);
  handleKeyRef.current = handleKey;
  useEffect(() => {
    if (!pageShortcuts) return;
    const onKeyDown = (e: KeyboardEvent) => {
      if (e.defaultPrevented || e.repeat || isImeKey(e)) return;
      if (e.metaKey || e.ctrlKey || e.altKey) return;
      if (!["ArrowDown", "ArrowUp", "j", "k", "J", "K"].includes(e.key)) return;
      const active = document.activeElement;
      if (active && active !== document.body) return;
      if (isPortalLayerShortcutTarget(e.target)) return;
      if (useModalStore.getState().modal) return;
      const grid = gridRef.current;
      if (!grid || grid.getClientRects().length === 0) return;
      grid.focus({ preventScroll: true });
      if (handleKeyRef.current(e, true)) e.preventDefault();
    };
    document.addEventListener("keydown", onKeyDown);
    return () => document.removeEventListener("keydown", onKeyDown);
  }, [pageShortcuts]);

  const renderRow = (row: T) => {
    const id = getRowId(row);
    return (
      <CollectionRow
        key={id}
        domId={`${listId}-row-${id}`}
        row={row}
        id={id}
        columns={columns}
        href={getRowHref?.(row) ?? null}
        title={getRowTitle?.(row)}
        onNavigate={onRowNavigate}
        onOpen={onRowOpen}
        menu={rowMenu ? rowMenu(row) : undefined}
        hasMenuSlot={hasMenu}
        className={getRowClassName?.(row)}
        rowLink={rowLink}
        selection={
          selection
            ? {
                selected: selectedIds?.has(id) ?? false,
                selectable: isSelectable(row),
                revealed: anySelected,
                onToggle: (range) => {
                  toggleRow(id, range);
                  focusGrid();
                },
                label: selection.getLabel?.(row) ?? t(($) => $.collection.select_row),
              }
            : null
        }
        rangeSelect={anySelected ? (range) => toggleRow(id, range) : null}
        active={showActive && activeId === id}
        onPointerActivate={() => {
          setActiveId(id);
          setShowActive(false);
        }}
      />
    );
  };

  const renderGroup = (group: CollectionGroup<T>) => (
    <div
      key={`group:${group.key}`}
      role="row"
      className="col-span-full pt-2 pb-0.5"
    >
      <div
        role="rowheader"
        className="flex h-10 items-center gap-2 rounded-lg bg-muted px-3 text-caption font-semibold"
      >
        <span className="min-w-0 truncate">{group.label}</span>
        {group.count !== undefined ? (
          <span className="font-normal tabular-nums text-muted-foreground">
            {group.count}
          </span>
        ) : null}
        {group.actions ? (
          <span className="ml-auto flex items-center gap-1">{group.actions}</span>
        ) : null}
      </div>
    </div>
  );

  const renderItem = (item: Item<T>) =>
    item.kind === "group" ? renderGroup(item.group) : renderRow(item.row);

  let body: ReactNode;
  if (loading) {
    body = Array.from({ length: 6 }).map((_, i) => (
      <SkeletonRow key={i} columns={columns} selectable={selectable} menu={hasMenu} />
    ));
  } else if (flatRows.length === 0 && !groups?.length) {
    body = empty ? (
      <div className="col-span-full py-16 text-center text-body text-muted-foreground">
        {empty}
      </div>
    ) : null;
  } else if (virtualize) {
    const virtualItems = virtualizer.getVirtualItems();
    const first = virtualItems[0];
    const last = virtualItems[virtualItems.length - 1];
    const top = first ? first.start - HEADER_HEIGHT : 0;
    const bottom = last ? virtualizer.getTotalSize() - last.end : 0;
    body = (
      <>
        {top > 0 ? <div aria-hidden className="col-span-full" style={{ height: top }} /> : null}
        {virtualItems.map((vi) => {
          const item = items[vi.index];
          return item ? renderItem(item) : null;
        })}
        {bottom > 0 ? (
          <div aria-hidden className="col-span-full" style={{ height: bottom }} />
        ) : null}
      </>
    );
  } else {
    body = items.map(renderItem);
  }

  const grid = (
    <div
      ref={gridRef}
      role="grid"
      aria-label={label}
      aria-multiselectable={selectable || undefined}
      aria-busy={loading || undefined}
      aria-activedescendant={
        showActive && activeId ? `${listId}-row-${activeId}` : undefined
      }
      tabIndex={0}
      onKeyDown={onGridKeyDown}
      onBlur={(e) => {
        if (!e.currentTarget.contains(e.relatedTarget as Node | null)) setShowActive(false);
      }}
      style={gridStyle}
      className={cn(
        "grid min-w-0 content-start gap-x-3 px-2 outline-none",
        "grid-cols-(--cl-narrow) @2xl:min-w-(--cl-minw) @2xl:grid-cols-(--cl-wide)",
        !embedded && "pb-16",
        className,
      )}
    >
      <HeaderRow
        columns={columns}
        sort={sort}
        selection={
          selection
            ? {
                all: allSelected,
                some: selectedVisible > 0 && !allSelected,
                onToggle: () => {
                  toggleAll();
                  focusGrid();
                },
                disabled: selectableIds.length === 0,
                label: selection.allLabel ?? t(($) => $.collection.select_all),
              }
            : null
        }
        menu={hasMenu}
      />
      {body}
      {hasMore && onLoadMore && !loading ? (
        <div className="col-span-full flex justify-center py-3">
          <Button
            variant="ghost"
            size="sm"
            disabled={loadingMore}
            onClick={onLoadMore}
            className="text-muted-foreground"
          >
            {loadingMore ? <Loader2 className="size-3.5 animate-spin" /> : null}
            {loadingMore
              ? t(($) => $.collection.loading_more)
              : t(($) => $.collection.load_more)}
          </Button>
        </div>
      ) : null}
    </div>
  );

  if (embedded) return <div className="@container min-w-0 overflow-x-auto">{grid}</div>;
  return (
    <div ref={scrollRef} className="min-h-0 flex-1 overflow-auto @container">
      {grid}
    </div>
  );
}

function HeaderRow<T>({
  columns,
  sort,
  selection,
  menu,
}: {
  columns: readonly CollectionColumn<T>[];
  sort?: CollectionSort;
  selection: {
    all: boolean;
    some: boolean;
    onToggle: () => void;
    label: string;
    disabled: boolean;
  } | null;
  menu: boolean;
}) {
  return (
    <div
      role="row"
      className="group/header sticky top-0 z-10 col-span-full grid h-9 grid-cols-subgrid items-center bg-background text-caption text-muted-foreground shadow-[-8px_0_0_0_var(--background),8px_0_0_0_var(--background)]"
    >
      <span aria-hidden />
      {selection ? (
        <div role="columnheader" className="hidden items-center justify-center @2xl:flex">
          <button
            type="button"
            role="checkbox"
            tabIndex={-1}
            aria-label={selection.label}
            aria-checked={selection.all ? true : selection.some ? "mixed" : false}
            disabled={selection.disabled}
            onMouseDown={(e) => e.preventDefault()}
            onClick={selection.onToggle}
            className={cn(
              "-m-1.5 flex items-center p-1.5",
              !selection.all &&
                !selection.some &&
                "opacity-0 transition-opacity group-hover/header:opacity-100 focus-visible:opacity-100",
            )}
          >
            <Checkbox
              aria-hidden
              checked={selection.all}
              indeterminate={selection.some}
              tabIndex={-1}
              className="pointer-events-none"
            />
          </button>
        </div>
      ) : null}
      {columns.map((col, i) => {
        const sorted = sort && col.sortKey && sort.field === col.sortKey ? sort.direction : null;
        return (
          <div
            key={col.key}
            role="columnheader"
            aria-sort={
              sorted === "asc" ? "ascending" : sorted === "desc" ? "descending" : undefined
            }
            className={cn(
              "flex min-w-0 items-center",
              col.align === "end" && "justify-end",
              i > 0 && !col.core && "hidden @2xl:flex",
            )}
          >
            {col.header && col.sortKey && sort ? (
              <button
                type="button"
                tabIndex={-1}
                onClick={() => sort.onToggle(col.sortKey!)}
                className={cn(
                  "-mx-1 inline-flex min-w-0 items-center gap-1 rounded-sm px-1 py-0.5 transition-colors hover:text-foreground",
                  sorted && "font-medium text-foreground",
                )}
              >
                <span className="truncate">{col.header}</span>
                {sorted === "asc" ? (
                  <ArrowUp className="size-3 shrink-0" />
                ) : sorted === "desc" ? (
                  <ArrowDown className="size-3 shrink-0" />
                ) : null}
              </button>
            ) : (
              <span className="truncate">{col.header}</span>
            )}
          </div>
        );
      })}
      {menu ? <span aria-hidden /> : null}
      <span aria-hidden />
    </div>
  );
}

interface RowSelectionProps {
  selected: boolean;
  selectable: boolean;
  revealed: boolean;
  onToggle: (range: boolean) => void;
  label: string;
}

function CollectionRow<T>({
  domId,
  row,
  id,
  columns,
  href,
  title,
  onNavigate,
  onOpen,
  menu,
  hasMenuSlot,
  className,
  rowLink,
  selection,
  rangeSelect,
  active,
  onPointerActivate,
}: {
  domId: string;
  row: T;
  id: string;
  columns: readonly CollectionColumn<T>[];
  href: string | null;
  title?: string;
  onNavigate?: (row: T) => void;
  onOpen?: (row: T) => void;
  menu: ReactNode;
  hasMenuSlot: boolean;
  className?: string;
  rowLink: ReturnType<typeof useRowLink>;
  selection: RowSelectionProps | null;
  rangeSelect: ((range: boolean) => void) | null;
  active: boolean;
  onPointerActivate: () => void;
}) {
  const [menuState, setMenu] = useState<RowMenuState>({ open: false, point: null });
  const context = useMemo(() => ({ menu: menuState, setMenu }), [menuState]);
  const link = href ? rowLink(href, title, onNavigate ? () => onNavigate(row) : undefined) : null;
  const clickable = !!link || !!onOpen;
  const hasMenu = menu !== undefined && menu !== null && menu !== false;
  const interactive = clickable || hasMenu || !!selection?.selectable;

  const inside = (e: ReactMouseEvent<HTMLDivElement>) =>
    e.currentTarget.contains(e.target as Node);

  return (
    <CollectionRowContext.Provider value={context}>
      <div
        id={domId}
        role="row"
        aria-selected={selection ? selection.selected : undefined}
        data-selected={selection?.selected || undefined}
        data-active={active || undefined}
        data-menu-open={menuState.open || undefined}
        data-row-id={id}
        className={cn(
          "group/row col-span-full grid h-9 scroll-mt-9 grid-cols-subgrid items-center rounded-md text-body",
          interactive && "hover:bg-surface-hover data-menu-open:bg-surface-hover",
          clickable && "cursor-pointer",
          "data-selected:bg-surface-selected data-selected:hover:bg-surface-selected",
          "data-active:ring-1 data-active:ring-ring data-active:ring-inset",
          className,
        )}
        onPointerDown={(e) => {
          if (e.button === 0 && inside(e)) onPointerActivate();
        }}
        onClick={(e) => {
          if (!inside(e) || e.defaultPrevented) return;
          if (e.shiftKey && rangeSelect && selection?.selectable) {
            e.preventDefault();
            rangeSelect(true);
            return;
          }
          if (link) link.onClick(e);
          else if (onOpen && e.button === 0) onOpen(row);
        }}
        onAuxClick={(e) => {
          if (inside(e)) link?.onAuxClick(e);
        }}
        onMouseEnter={link?.onMouseEnter ?? noop}
        onContextMenu={(e) => {
          if (!hasMenu || !inside(e)) return;
          e.preventDefault();
          setMenu({ open: true, point: { x: e.clientX, y: e.clientY } });
        }}
      >
        <span aria-hidden />
        {selection ? (
          <div role="gridcell" className="hidden items-center justify-center @2xl:flex">
            {selection.selectable ? (
              <button
                type="button"
                role="checkbox"
                tabIndex={-1}
                aria-label={selection.label}
                aria-checked={selection.selected}
                onMouseDown={(e) => e.preventDefault()}
                onClick={(e) => {
                  e.stopPropagation();
                  selection.onToggle(e.shiftKey);
                }}
                onAuxClick={(e) => e.stopPropagation()}
                className={cn(
                  "-m-1.5 flex items-center p-1.5",
                  !selection.selected &&
                    !selection.revealed &&
                    "opacity-0 transition-opacity group-hover/row:opacity-100 focus-visible:opacity-100",
                )}
              >
                <Checkbox
                  aria-hidden
                  checked={selection.selected}
                  tabIndex={-1}
                  className="pointer-events-none"
                />
              </button>
            ) : null}
          </div>
        ) : null}
        {columns.map((col, i) => (
          <div
            key={col.key}
            role="gridcell"
            className={cn(
              "flex min-w-0 items-center",
              col.align === "end" && "justify-end",
              i > 0 && !col.core && "hidden @2xl:flex",
              i > 0 && "gap-1.5 whitespace-nowrap text-caption text-muted-foreground",
              col.className,
            )}
            onClick={col.interactive ? (e) => e.stopPropagation() : undefined}
            onAuxClick={col.interactive ? (e) => e.stopPropagation() : undefined}
          >
            {col.cell(row)}
          </div>
        ))}
        {hasMenuSlot ? (
          <div
            role="gridcell"
            className="flex items-center justify-end"
            onClick={(e) => e.stopPropagation()}
            onAuxClick={(e) => e.stopPropagation()}
          >
            {menu}
          </div>
        ) : null}
        <span aria-hidden />
      </div>
    </CollectionRowContext.Provider>
  );
}

function SkeletonRow<T>({
  columns,
  selectable,
  menu,
}: {
  columns: readonly CollectionColumn<T>[];
  selectable: boolean;
  menu: boolean;
}) {
  return (
    <div aria-hidden className="col-span-full grid h-9 grid-cols-subgrid items-center">
      <span />
      {selectable ? <span className="hidden @2xl:block" /> : null}
      {columns.map((col, i) => (
        <div
          key={col.key}
          className={cn(
            "flex min-w-0 items-center",
            col.align === "end" && "justify-end",
            i > 0 && !col.core && "hidden @2xl:flex",
          )}
        >
          {i === 0 ? (
            <span className="flex min-w-0 flex-1 items-center gap-2">
              <Skeleton className="size-5 shrink-0 rounded-full" />
              <Skeleton className="h-3.5 w-40 max-w-full" />
            </span>
          ) : (
            <Skeleton className="h-3 w-14 max-w-full" />
          )}
        </div>
      ))}
      {menu ? <span /> : null}
      <span />
    </div>
  );
}
