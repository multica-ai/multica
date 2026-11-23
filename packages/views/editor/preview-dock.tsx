"use client";

/**
 * PreviewDock — the side pane a surface's files open into, beside the content
 * instead of over it (MUL-10014).
 *
 * Two pieces, because the viewer's owner sits ABOVE the layout that has room
 * for a pane: the issue page's `PreviewSequenceProvider` wraps both its
 * content column and its right sidebar.
 *
 *   - `PreviewDockHost` goes above the provider and holds whichever dock the
 *     layout below registered.
 *   - `PreviewDock` is that layout: the content, then the pane. The provider
 *     asks it to open, and portals the pane's body into its slot.
 *
 * Room comes first from the surface's own content, which never goes below
 * `PREVIEW_MAIN_MIN_WIDTH`. When that is not enough, the dock asks its
 * `yields` — the left nav, then the right sidebar — to step aside, in order,
 * and gives them back in reverse when the pane closes. A yield the reader
 * reopened in the meantime stays as they left it. Nothing here is persisted
 * except the pane's own width; a parked sidebar or nav keeps its saved state.
 *
 * The pane's width is derived from the room the dock has, every render: a
 * window that narrows, or a right sidebar the reader opens with the pane up,
 * first narrows the pane to `PREVIEW_PANE_MIN_WIDTH` and then folds it away
 * (still open: it comes back when the room does). The pane never closes
 * itself — only the reader closes it.
 */

import {
  createContext,
  use,
  useCallback,
  useEffect,
  useImperativeHandle,
  useLayoutEffect,
  useMemo,
  useRef,
  useState,
  type ReactNode,
  type Ref,
} from "react";
import { useReducedMotion } from "@multica/core/appearance";
import { cn } from "@multica/ui/lib/utils";
import { useT } from "../i18n";

export const PREVIEW_MAIN_MIN_WIDTH = 600;
export const PREVIEW_PANE_MIN_WIDTH = 420;
export const PREVIEW_PANE_DEFAULT_WIDTH = 560;
const PANE_WIDTH_STORAGE_KEY = "multica_preview_pane_width";
const PANE_KEYBOARD_STEP = 16;
// Same motion as the right sidebar, so a pane opening while the sidebar
// parks reads as one movement.
const PANE_TRANSITION = "width 220ms cubic-bezier(0.22, 1, 0.36, 1)";
/** Long enough for the pane and every yield to finish moving. */
export const PREVIEW_DOCK_SETTLE_MS = 300;

/**
 * Something that can step aside to give the pane room: the left nav, the
 * right sidebar. `reclaimable` is the width it would give back right now — 0
 * when it is already out of the way.
 */
export interface PreviewDockYield {
  reclaimable: () => number;
  park: () => void;
  /** Undo `park`, unless the reader has changed it since. */
  restore: () => void;
}

export interface PreviewDockController {
  /**
   * Show the pane, making room for it. False when the surface cannot fit
   * one even with every yield out of the way: show the full viewer instead.
   */
  open: () => boolean;
  close: () => void;
  /** Where the pane's body renders. Null until the slot has mounted. */
  target: HTMLElement | null;
}

interface DockHostValue {
  controller: PreviewDockController | null;
  register: (controller: PreviewDockController | null) => void;
}

const DockHostContext = createContext<DockHostValue | null>(null);

export function PreviewDockHost({ children }: { children: ReactNode }) {
  const [controller, setController] = useState<PreviewDockController | null>(null);
  const value = useMemo(() => ({ controller, register: setController }), [controller]);
  return <DockHostContext.Provider value={value}>{children}</DockHostContext.Provider>;
}

/** The dock this surface's files open into, or null when there is none. */
export function usePreviewDockController(): PreviewDockController | null {
  return use(DockHostContext)?.controller ?? null;
}

function readStoredWidth(): number {
  try {
    const stored = Number(localStorage.getItem(PANE_WIDTH_STORAGE_KEY));
    return Number.isFinite(stored) && stored >= PREVIEW_PANE_MIN_WIDTH
      ? stored
      : PREVIEW_PANE_DEFAULT_WIDTH;
  } catch {
    return PREVIEW_PANE_DEFAULT_WIDTH;
  }
}

function storeWidth(width: number) {
  try {
    localStorage.setItem(PANE_WIDTH_STORAGE_KEY, String(Math.round(width)));
  } catch {
    // Private mode or a full quota: the width just isn't remembered.
  }
}

/** The pane's width in `room`, or 0 when it does not fit at all. */
export function fitPaneWidth(wanted: number, room: number): number {
  const max = room - PREVIEW_MAIN_MIN_WIDTH;
  if (max < PREVIEW_PANE_MIN_WIDTH) return 0;
  return Math.min(Math.max(wanted, PREVIEW_PANE_MIN_WIDTH), max);
}

export function PreviewDock({
  yields,
  className,
  children,
  controllerRef,
}: {
  /** Asked in order to make room; given back in reverse. */
  yields: ReadonlyArray<PreviewDockYield | null>;
  className?: string;
  children: ReactNode;
  /** For hosts and tests that drive the dock directly. */
  controllerRef?: Ref<PreviewDockController | null>;
}) {
  const { t } = useT("editor");
  const host = use(DockHostContext);
  const register = host?.register;
  const reduceMotion = useReducedMotion();
  const rootRef = useRef<HTMLDivElement>(null);
  const [slot, setSlot] = useState<HTMLElement | null>(null);
  const [open, setOpen] = useState(false);
  const openRef = useRef(false);
  const [width, setWidth] = useState(readStoredWidth);
  const widthRef = useRef(width);
  widthRef.current = width;
  const [dragWidth, setDragWidth] = useState<number | null>(null);
  const [rootWidth, setRootWidth] = useState(0);
  // The room the dock will have once the yields it just parked have moved
  // out. Until they have, it stands in for the measured width, so the pane
  // opens at its full width while they slide away rather than after.
  const [expected, setExpected] = useState<number | null>(null);
  const expectedRef = useRef<number | null>(null);
  expectedRef.current = expected;
  const expectedTimer = useRef<ReturnType<typeof setTimeout> | null>(null);
  const yieldsRef = useRef(yields);
  yieldsRef.current = yields;
  const parkedRef = useRef<PreviewDockYield[]>([]);

  useLayoutEffect(() => {
    const root = rootRef.current;
    if (!root) return;
    setRootWidth(root.getBoundingClientRect().width);
    if (typeof ResizeObserver === "undefined") return;
    const observer = new ResizeObserver(([entry]) => {
      if (entry) setRootWidth(entry.contentRect.width);
    });
    observer.observe(root);
    return () => observer.disconnect();
  }, []);

  const restoreParked = useCallback(() => {
    const parked = parkedRef.current;
    parkedRef.current = [];
    for (const item of [...parked].reverse()) item.restore();
  }, []);

  const openPane = useCallback(() => {
    const root = rootRef.current;
    if (!root) return false;
    const room = Math.max(root.getBoundingClientRect().width, expectedRef.current ?? 0);
    const candidates = yieldsRef.current.filter(
      (item): item is PreviewDockYield => item !== null && !parkedRef.current.includes(item),
    );
    const reclaim = candidates.map((item) => Math.max(0, item.reclaimable()));
    const most = reclaim.reduce((sum, value) => sum + value, room);
    if (fitPaneWidth(PREVIEW_PANE_MIN_WIDTH, most) === 0) return false;

    // Already showing at a usable width: the reader only switched files.
    if (openRef.current && fitPaneWidth(widthRef.current, room) > 0) return true;

    let next = room;
    candidates.forEach((item, index) => {
      if (next - PREVIEW_MAIN_MIN_WIDTH >= widthRef.current) return;
      const gain = reclaim[index] ?? 0;
      if (gain <= 0) return;
      item.park();
      parkedRef.current.push(item);
      next += gain;
    });
    if (next > room) {
      expectedRef.current = next;
      setExpected(next);
      if (expectedTimer.current) clearTimeout(expectedTimer.current);
      expectedTimer.current = setTimeout(() => {
        expectedTimer.current = null;
        setExpected(null);
      }, PREVIEW_DOCK_SETTLE_MS);
    }
    openRef.current = true;
    setOpen(true);
    return true;
  }, []);

  const closePane = useCallback(() => {
    if (!openRef.current) return;
    openRef.current = false;
    setOpen(false);
    setExpected(null);
    restoreParked();
  }, [restoreParked]);

  const controller = useMemo<PreviewDockController>(
    () => ({ open: openPane, close: closePane, target: slot }),
    [openPane, closePane, slot],
  );
  useImperativeHandle(controllerRef, () => controller, [controller]);

  useEffect(() => {
    if (!register) return;
    register(controller);
    return () => register(null);
  }, [register, controller]);

  // Leaving the page with the pane up gives the nav and sidebar back.
  useEffect(
    () => () => {
      if (expectedTimer.current) clearTimeout(expectedTimer.current);
      if (openRef.current) restoreParked();
    },
    [restoreParked],
  );

  const room = Math.max(rootWidth, expected ?? 0);
  const paneWidth = open ? fitPaneWidth(dragWidth ?? width, room) : 0;
  // The body keeps the width it was last shown at while the pane slides
  // shut, so its text doesn't reflow on the way out.
  const shownWidthRef = useRef(paneWidth);
  if (paneWidth > 0) shownWidthRef.current = paneWidth;
  const bodyWidth = paneWidth || shownWidthRef.current;

  // Drag and arrow keys on the separator. The pane grows leftwards, so the
  // pointer moving left widens it.
  const dragRef = useRef<{ startX: number; startWidth: number; latest: number } | null>(null);
  const commitWidth = (next: number) => {
    setWidth(next);
    storeWidth(next);
  };
  const onPointerDown = (event: React.PointerEvent<HTMLDivElement>) => {
    if (event.button !== 0 || paneWidth === 0) return;
    event.preventDefault();
    event.currentTarget.setPointerCapture(event.pointerId);
    dragRef.current = { startX: event.clientX, startWidth: paneWidth, latest: paneWidth };
    document.documentElement.dataset.resizing = "col";
  };
  const onPointerMove = (event: React.PointerEvent<HTMLDivElement>) => {
    const drag = dragRef.current;
    if (!drag) return;
    const next = fitPaneWidth(drag.startWidth + drag.startX - event.clientX, rootWidth);
    if (next === 0) return;
    drag.latest = next;
    setDragWidth(next);
  };
  const endDrag = () => {
    const drag = dragRef.current;
    if (!drag) return;
    dragRef.current = null;
    delete document.documentElement.dataset.resizing;
    setDragWidth(null);
    commitWidth(drag.latest);
  };
  const onSeparatorKeyDown = (event: React.KeyboardEvent<HTMLDivElement>) => {
    const delta =
      event.key === "ArrowLeft" ? PANE_KEYBOARD_STEP : event.key === "ArrowRight" ? -PANE_KEYBOARD_STEP : 0;
    if (delta === 0 || paneWidth === 0) return;
    event.preventDefault();
    const next = fitPaneWidth(paneWidth + delta, rootWidth);
    if (next > 0) commitWidth(next);
  };

  const animate = !reduceMotion && dragWidth === null;

  return (
    <div ref={rootRef} data-slot="preview-dock" className={cn("flex min-h-0 min-w-0 flex-1", className)}>
      <div className="flex min-h-0 min-w-0 flex-1 flex-col">{children}</div>
      {paneWidth > 0 && (
        <div
          role="separator"
          aria-orientation="vertical"
          aria-label={t(($) => $.attachment.resize_preview)}
          aria-valuenow={Math.round(paneWidth)}
          aria-valuemin={PREVIEW_PANE_MIN_WIDTH}
          aria-valuemax={Math.max(PREVIEW_PANE_MIN_WIDTH, Math.round(rootWidth - PREVIEW_MAIN_MIN_WIDTH))}
          tabIndex={0}
          data-separator={dragWidth !== null ? "active" : undefined}
          className="relative z-10 w-0 shrink-0 cursor-col-resize touch-none before:absolute before:inset-y-0 before:left-1/2 before:w-px before:-translate-x-1/2 before:bg-transparent before:transition-colors hover:before:bg-foreground/15 data-[separator=active]:before:bg-foreground/15 after:absolute after:inset-y-0 after:left-1/2 after:w-2 after:-translate-x-1/2 focus-visible:outline-hidden focus-visible:before:bg-ring"
          onPointerDown={onPointerDown}
          onPointerMove={onPointerMove}
          onPointerUp={endDrag}
          onPointerCancel={endDrag}
          onKeyDown={onSeparatorKeyDown}
        />
      )}
      <div
        data-preview-pane={open ? (paneWidth > 0 ? "open" : "folded") : undefined}
        className={cn("min-h-0 shrink-0 overflow-hidden", paneWidth > 0 && "border-l border-border")}
        style={{ width: paneWidth, transition: animate ? PANE_TRANSITION : undefined }}
        aria-hidden={paneWidth === 0 || undefined}
        inert={paneWidth === 0 || undefined}
      >
        <div ref={setSlot} className="h-full" style={{ width: bodyWidth }} />
      </div>
    </div>
  );
}
